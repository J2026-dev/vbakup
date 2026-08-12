package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/J2026-dev/vbakup/internal/model"
	"github.com/J2026-dev/vbakup/internal/store"
	"github.com/J2026-dev/vbakup/internal/vault"
)

func newTestServer(t *testing.T) (*server, *store.Store) {
	t.Helper()
	dataDir := t.TempDir()
	state, err := store.Open(filepath.Join(dataDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := vault.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	return &server{store: state, vault: secrets, publicURL: "https://backup.example.com", bootstrapSecret: "test-secret"}, state
}

func TestStateUsesEmptyArraysAndIncludesInstallCommand(t *testing.T) {
	app, _ := newTestServer(t)
	response := httptest.NewRecorder()
	app.handleState(response, httptest.NewRequest(http.MethodGet, "/api/state", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"nodes", "repositories", "tasks", "backups", "operations"} {
		value, ok := body[key].([]any)
		if !ok || len(value) != 0 {
			t.Fatalf("%s must be an empty array, got %#v", key, body[key])
		}
	}
	if !strings.Contains(body["install_command"].(string), "https://backup.example.com/install.sh") {
		t.Fatalf("unexpected install command: %v", body["install_command"])
	}
}

func TestUpdateNodeNoteAndQueueReadableName(t *testing.T) {
	app, state := newTestServer(t)
	if err := state.Update(func(st *model.State) error {
		st.Nodes = append(st.Nodes, model.Node{ID: "node-12345678", Name: "host-1"})
		st.Repositories = append(st.Repositories, model.Repository{ID: "repo-1"})
		st.Tasks = append(st.Tasks, model.Task{ID: "task-1", NodeID: "node-12345678", RepositoryID: "repo-1", Schedule: "@daily", Enabled: true})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPatch, "/api/nodes/node-12345678", strings.NewReader(`{"note":"香港主站"}`))
	request.SetPathValue("id", "node-12345678")
	response := httptest.NewRecorder()
	app.handleUpdateNode(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	command, err := app.queueBackup("task-1", true)
	if err != nil {
		t.Fatal(err)
	}
	if command.Payload["node_name"] != "香港主站" {
		t.Fatalf("node_name=%v", command.Payload["node_name"])
	}
}

func TestCreateRepositoryChecksWebDAVBeforeSaving(t *testing.T) {
	const username, password = "dav-user", "dav-password"
	dav := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if r.Method != "PROPFIND" || r.Header.Get("Depth") != "0" || !ok || user != username || pass != password {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusMultiStatus)
	}))
	defer dav.Close()
	app, state := newTestServer(t)
	body := fmt.Sprintf(`{"name":"test","url":%q,"username":%q,"password":%q,"base_path":"vbakup"}`, dav.URL, username, password)
	response := httptest.NewRecorder()
	app.handleCreateRepository(response, httptest.NewRequest(http.MethodPost, "/api/repositories", strings.NewReader(body)))
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if len(state.Snapshot().Repositories) != 1 {
		t.Fatal("validated repository was not saved")
	}
}

func TestQueuedCommandDoesNotPersistRepositoryPassword(t *testing.T) {
	dataDir := t.TempDir()
	statePath := filepath.Join(dataDir, "state.json")
	state, err := store.Open(statePath)
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := vault.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := secrets.Encrypt("unique-plaintext-password")
	if err != nil {
		t.Fatal(err)
	}
	app := &server{store: state, vault: secrets}
	err = state.Update(func(st *model.State) error {
		st.Repositories = append(st.Repositories, model.Repository{ID: "repo-1", URL: "https://dav.example", PasswordEncrypted: encrypted})
		st.Tasks = append(st.Tasks, model.Task{ID: "task-1", NodeID: "node-1", RepositoryID: "repo-1", Schedule: "@daily", Enabled: true, CreatedAt: time.Now()})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	command, err := app.queueBackup("task-1", true)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "unique-plaintext-password") {
		t.Fatal("plaintext password persisted in queue")
	}
	if err = app.injectRepositoryCredentials(&command); err != nil {
		t.Fatal(err)
	}
	repo, ok := command.Payload["repository"].(model.RepositoryCredentials)
	if !ok || repo.Password != "unique-plaintext-password" {
		t.Fatal("credentials were not injected for delivery")
	}
	if err = state.Update(func(st *model.State) error { return nil }); err != nil {
		t.Fatal(err)
	}
	b, err = os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "unique-plaintext-password") {
		t.Fatal("delivery mutated persisted command payload")
	}
}

func TestCloneCommandDoesNotSharePayload(t *testing.T) {
	original := model.Command{Payload: map[string]any{"repository_id": "repo-1"}}
	copy, err := cloneCommand(original)
	if err != nil {
		t.Fatal(err)
	}
	copy.Payload["repository"] = "secret"
	if _, ok := original.Payload["repository"]; ok {
		t.Fatal("cloned payload shares backing map")
	}
}

func TestRestoreResultCannotCreateBackupOrPanic(t *testing.T) {
	dataDir := t.TempDir()
	state, err := store.Open(filepath.Join(dataDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := vault.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	token := "agent-token"
	node := model.Node{ID: "node-1", TokenHash: hashToken(token)}
	command := model.Command{ID: "cmd-1", NodeID: node.ID, Type: "restore", Payload: map[string]any{"confirm": true}}
	if err = state.Update(func(st *model.State) error {
		st.Nodes = append(st.Nodes, node)
		st.Commands = append(st.Commands, command)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	app := &server{store: state, vault: secrets}
	body, _ := json.Marshal(model.CommandResult{Status: "success", Backup: &model.Backup{RemotePath: "unexpected"}})
	request := httptest.NewRequest(http.MethodPost, "/api/agent/node-1/commands/cmd-1/result", bytes.NewReader(body))
	request.SetPathValue("node", "node-1")
	request.SetPathValue("command", "cmd-1")
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	app.handleCommandResult(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if len(state.Snapshot().Backups) != 0 {
		t.Fatal("restore result created a backup record")
	}
}
