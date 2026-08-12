package store

import (
	"path/filepath"
	"testing"

	"github.com/J2026-dev/vbakup/internal/model"
)

func TestPersistsState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Update(func(st *model.State) error { st.Nodes = append(st.Nodes, model.Node{ID: "node-1"}); return nil }); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(reopened.Snapshot().Nodes); got != 1 {
		t.Fatalf("got %d nodes", got)
	}
}
