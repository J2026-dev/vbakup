package main

import (
	"path/filepath"
	"testing"

	"github.com/J2026-dev/vbakup/internal/model"
)

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
