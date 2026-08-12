package webdav

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
)

type Client struct {
	base, user, password string
	client               *http.Client
}

func New(rawURL, username, password string) (*Client, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("invalid WebDAV URL")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
	return &Client{base: strings.TrimRight(u.String(), "/"), user: username, password: password, client: &http.Client{Transport: transport, Timeout: 0}}, nil
}

func (c *Client) request(method, name string, body io.Reader) (*http.Response, error) {
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
