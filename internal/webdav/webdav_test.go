package webdav

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

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
	if err != nil { t.Fatal(err) }
	used, files, err := client.Usage()
	if err != nil { t.Fatal(err) }
	if used != 42 || files != 2 { t.Fatalf("used=%d files=%d", used, files) }
}

