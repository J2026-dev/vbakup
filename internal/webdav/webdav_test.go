package webdav

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPutSizedSetsContentLengthAndSizeReadsHead(t *testing.T) {
	const payload = "backup-content"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "MKCOL":
			w.WriteHeader(http.StatusCreated)
		case http.MethodPut:
			if r.ContentLength != int64(len(payload)) {
				t.Fatalf("content length=%d", r.ContentLength)
			}
			body, _ := io.ReadAll(r.Body)
			if string(body) != payload {
				t.Fatalf("body=%q", body)
			}
			w.WriteHeader(http.StatusCreated)
		case http.MethodHead:
			w.Header().Set("Content-Length", "14")
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, "unexpected", http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()
	client, err := New(server.URL+"/dav", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err = client.PutSized("node/snapshot.tar.gz", bytes.NewBufferString(payload), int64(len(payload))); err != nil {
		t.Fatal(err)
	}
	got, err := client.Size("node/snapshot.tar.gz")
	if err != nil || got != int64(len(payload)) {
		t.Fatalf("size=%d err=%v", got, err)
	}
}

func TestUsageParsesFilesAndDirectories(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PROPFIND" {
			t.Fatalf("method=%s", r.Method)
		}
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusMultiStatus)
		_, _ = w.Write([]byte(`<?xml version="1.0"?><multistatus xmlns="DAV:">
<response><href>/dav/</href><propstat><prop><resourcetype><collection/></resourcetype></prop></propstat></response>
<response><href>/dav/a.tar.gz</href><propstat><prop><getcontentlength>12</getcontentlength><resourcetype/></prop></propstat></response>
<response><href>/dav/b.tar.gz</href><propstat><prop><getcontentlength>30</getcontentlength><resourcetype/></prop></propstat></response>
</multistatus>`))
	}))
	defer server.Close()
	client, err := New(server.URL+"/dav", "user", "pass")
	if err != nil {
		t.Fatal(err)
	}
	used, files, err := client.Usage()
	if err != nil {
		t.Fatal(err)
	}
	if used != 42 || files != 2 {
		t.Fatalf("used=%d files=%d", used, files)
	}
}
