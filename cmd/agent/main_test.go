package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agentlib "github.com/J2026-dev/vbakup/internal/agent"
	"github.com/J2026-dev/vbakup/internal/model"
	"github.com/J2026-dev/vbakup/internal/webdav"
)

func TestSafeRemoteKeyword(t *testing.T) {
	got := safeRemoteKeyword(" 香港 主站 / DB #1 ")
	if got != "香港-主站-DB-1" {
		t.Fatalf("got %q", got)
	}
	if strings.ContainsAny(got, "/\\") {
		t.Fatalf("unsafe keyword %q", got)
	}
}

func TestDownloadVerifiedRetriesEmptyResponse(t *testing.T) {
	want := []byte("verified backup")
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts > 1 {
			_, _ = w.Write(want)
		}
	}))
	defer server.Close()
	dav, err := webdav.New(server.URL, "", "")
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "source")
	if err = os.WriteFile(source, want, 0600); err != nil {
		t.Fatal(err)
	}
	hash, size, err := agentlib.FileSHA256(source)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "download")
	actualHash, actualSize, err := downloadVerified(dav, "archive", destination, hash, size, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if attempts < 2 || actualHash != hash || actualSize != size {
		t.Fatalf("attempts=%d hash=%s size=%d", attempts, actualHash, actualSize)
	}
}

func TestConfigurePersistsAutoUpdate(t *testing.T) {
	originalTimer := setAutoUpdateTimer
	setAutoUpdateTimer = func(bool) error { return nil }
	t.Cleanup(func() { setAutoUpdateTimer = originalTimer })
	configPath := filepath.Join(t.TempDir(), "agent.json")
	runner := &app{config: config{Controller: "https://backup.example.com", NodeID: "node-1", Token: "token"}, configPath: configPath}
	result := runner.configure(model.Command{Payload: map[string]any{"auto_update": true}})
	if result.Status != "success" {
		t.Fatalf("result=%+v", result)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var saved config
	if err = json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if !saved.AutoUpdate || saved.NodeID != "node-1" || saved.Token != "token" {
		t.Fatalf("saved=%+v", saved)
	}
}

func TestCommandResultsPersistAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "results.json")
	want := model.CommandResult{Status: "success", Message: "restore complete"}
	if err := saveResults(path, map[string]model.CommandResult{"cmd-1": want}); err != nil {
		t.Fatal(err)
	}
	got, err := loadResults(path)
	if err != nil {
		t.Fatal(err)
	}
	if got["cmd-1"].Status != want.Status || got["cmd-1"].Message != want.Message {
		t.Fatalf("got %#v", got)
	}
	delete(got, "cmd-1")
	if err = saveResults(path, got); err != nil {
		t.Fatal(err)
	}
	got, err = loadResults(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatal("reported result was not removed")
	}
}
