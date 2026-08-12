package webdav

import (
	"bytes"
	"crypto/tls"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

type Client struct {
	base, user, password string
	client               *http.Client
}

type davMultiStatus struct {
	Responses []davResponse `xml:"response"`
}
type davResponse struct {
	Href  string       `xml:"href"`
	Props []davPropSet `xml:"propstat"`
}
type davPropSet struct {
	Prop davProps `xml:"prop"`
}
type davProps struct {
	Length    string       `xml:"getcontentlength"`
	Collection *struct{} `xml:"resourcetype>collection"`
}

func New(rawURL, username, password string) (*Client, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("invalid WebDAV URL")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	return &Client{base: strings.TrimRight(u.String(), "/"), user: username, password: password, client: &http.Client{Transport: transport, Timeout: 30 * time.Second}}, nil
}

func (c *Client) request(method, name string, body io.Reader) (*http.Response, error) {
	return c.requestDepth(method, name, body, "0")
}

func (c *Client) requestDepth(method, name string, body io.Reader, depth string) (*http.Response, error) {
	clean := strings.TrimLeft(path.Clean("/"+name), "/")
	req, err := http.NewRequest(method, c.base+"/"+clean, body)
	if err != nil {
		return nil, err
	}
	if c.user != "" {
		req.SetBasicAuth(c.user, c.password)
	}
	if method == http.MethodPut {
		req.Header.Set("Content-Type", "application/octet-stream")
	}
	if method == "PROPFIND" {
		req.Header.Set("Content-Type", "application/xml; charset=utf-8")
		req.Header.Set("Depth", depth)
	}
	return c.client.Do(req)
}

func (c *Client) MkdirAll(directory string) error {
	current := ""
	for _, part := range strings.Split(strings.Trim(path.Clean(directory), "/"), "/") {
		if part == "" || part == "." {
			continue
		}
		current = path.Join(current, part)
		response, err := c.request("MKCOL", current, bytes.NewReader(nil))
		if err != nil {
			return err
		}
		_ = response.Body.Close()
		if response.StatusCode >= 300 && response.StatusCode != http.StatusMethodNotAllowed && response.StatusCode != http.StatusConflict {
			return fmt.Errorf("WebDAV MKCOL %s: %s", current, response.Status)
		}
	}
	return nil
}
func (c *Client) Put(name string, body io.Reader) error {
	if err := c.MkdirAll(path.Dir(name)); err != nil {
		return err
	}
	response, err := c.request(http.MethodPut, name, body)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		return fmt.Errorf("WebDAV PUT: %s", response.Status)
	}
	return nil
}
func (c *Client) Get(name string) (io.ReadCloser, error) {
	response, err := c.request(http.MethodGet, name, nil)
	if err != nil {
		return nil, err
	}
	if response.StatusCode >= 300 {
		_ = response.Body.Close()
		return nil, fmt.Errorf("WebDAV GET: %s", response.Status)
	}
	return response.Body, nil
}

func (c *Client) Delete(name string) error {
	response, err := c.request(http.MethodDelete, name, nil)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 && response.StatusCode != http.StatusNotFound {
		return fmt.Errorf("WebDAV DELETE: %s", response.Status)
	}
	return nil
}

func (c *Client) Usage() (usedBytes int64, files int, err error) {
	body := bytes.NewReader([]byte(`<?xml version="1.0"?><propfind xmlns="DAV:"><prop><getcontentlength/><resourcetype/></prop></propfind>`))
	response, requestErr := c.requestDepth("PROPFIND", "", body, "infinity")
	if requestErr == nil {
		usedBytes, files, err = parseUsageResponse(response)
		if err == nil {
			return usedBytes, files, nil
		}
	}
	// Some WebDAV gateways reject Depth: infinity. Walk one directory level at
	// a time as a compatible fallback.
	return c.usageWalk()
}

func parseUsageResponse(response *http.Response) (int64, int, error) {
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		return 0, 0, fmt.Errorf("WebDAV PROPFIND: %s", response.Status)
	}
	var result davMultiStatus
	if err := xml.NewDecoder(response.Body).Decode(&result); err != nil {
		return 0, 0, err
	}
	var used int64
	files := 0
	for _, item := range result.Responses {
		for _, propstat := range item.Props {
			if propstat.Prop.Collection != nil {
				continue
			}
			var size int64
			if _, scanErr := fmt.Sscan(propstat.Prop.Length, &size); scanErr == nil && size >= 0 {
				used += size
				files++
			}
		}
	}
	return used, files, nil
}

func (c *Client) usageWalk() (int64, int, error) {
	queue := []string{""}
	var used int64
	files := 0
	seen := map[string]bool{"": true}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		response, err := c.requestDepth("PROPFIND", name, bytes.NewReader([]byte(`<?xml version="1.0"?><propfind xmlns="DAV:"><prop><getcontentlength/><resourcetype/></prop></propfind>`)), "1")
		if err != nil {
			return 0, 0, err
		}
		if response.StatusCode >= 300 {
			status := response.Status
			_ = response.Body.Close()
			return 0, 0, fmt.Errorf("WebDAV PROPFIND: %s", status)
		}
		var result davMultiStatus
		if err = xml.NewDecoder(response.Body).Decode(&result); err != nil {
			_ = response.Body.Close()
			return 0, 0, err
		}
		_ = response.Body.Close()
		for _, item := range result.Responses {
			itemName := strings.Trim(strings.TrimSpace(item.Href), "/")
			for _, propstat := range item.Props {
				if propstat.Prop.Collection != nil {
					if itemName != "" && !seen[itemName] {
						seen[itemName] = true
						queue = append(queue, itemName)
					}
					continue
				}
				var size int64
				if _, scanErr := fmt.Sscan(propstat.Prop.Length, &size); scanErr == nil && size >= 0 {
					used += size
					files++
				}
			}
		}
	}
	return used, files, nil
}

func (c *Client) Test() error {
	response, err := c.request("PROPFIND", "", bytes.NewReader([]byte(`<?xml version="1.0"?><propfind xmlns="DAV:"><prop><displayname/></prop></propfind>`)))
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		return fmt.Errorf("WebDAV PROPFIND: %s", response.Status)
	}
	return nil
}
