package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/J2026-dev/vbakup/internal/id"
	"github.com/J2026-dev/vbakup/internal/model"
	"github.com/J2026-dev/vbakup/internal/store"
	"github.com/J2026-dev/vbakup/internal/vault"
)

//go:embed assets/*
var assets embed.FS

type server struct {
	store                                                             *store.Store
	vault                                                             *vault.Vault
	publicURL, releaseBase, bootstrapSecret, adminUser, adminPassword string
}

type publicState struct {
	Nodes          []model.Node      `json:"nodes"`
	Repositories   []repositoryView  `json:"repositories"`
	Tasks          []model.Task      `json:"tasks"`
	Backups        []model.Backup    `json:"backups"`
	Operations     []model.Operation `json:"operations"`
	InstallCommand string            `json:"install_command"`
}
type repositoryView struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Username  string    `json:"username"`
	BasePath  string    `json:"base_path"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	web, _ := fs.Sub(assets, "assets")
	mux.Handle("GET /", s.admin(http.FileServer(http.FS(web))))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.Handle("GET /api/state", s.admin(http.HandlerFunc(s.handleState)))
	mux.Handle("POST /api/repositories", s.admin(http.HandlerFunc(s.handleCreateRepository)))
	mux.Handle("POST /api/tasks", s.admin(http.HandlerFunc(s.handleCreateTask)))
	mux.Handle("POST /api/tasks/{id}/run", s.admin(http.HandlerFunc(s.handleRunTask)))
	mux.Handle("POST /api/backups/{id}/restore", s.admin(http.HandlerFunc(s.handleRestore)))
	mux.Handle("GET /install.sh", http.HandlerFunc(s.handleInstaller))
	mux.Handle("POST /api/agent/register", http.HandlerFunc(s.handleRegister))
	mux.Handle("POST /api/agent/{node}/heartbeat", http.HandlerFunc(s.handleHeartbeat))
	mux.Handle("GET /api/agent/{node}/commands", http.HandlerFunc(s.handleCommands))
	mux.Handle("POST /api/agent/{node}/commands/{command}/result", http.HandlerFunc(s.handleCommandResult))
	return securityHeaders(requestLog(mux))
}

func (s *server) admin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(u), []byte(s.adminUser)) != 1 || subtle.ConstantTimeCompare([]byte(p), []byte(s.adminPassword)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="vBakup"`)
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if origin := r.Header.Get("Origin"); origin != "" {
				expected, _ := url.Parse(s.publicURL)
				actual, _ := url.Parse(origin)
				if expected.Host != actual.Host {
					http.Error(w, "invalid origin", http.StatusForbidden)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) handleState(w http.ResponseWriter, _ *http.Request) {
	state := s.store.Snapshot()
	for i := range state.Nodes {
		state.Nodes[i].TokenHash = ""
	}
	repos := make([]repositoryView, 0, len(state.Repositories))
	for _, r := range state.Repositories {
		repos = append(repos, repositoryView{ID: r.ID, Name: r.Name, URL: r.URL, Username: r.Username, BasePath: r.BasePath, CreatedAt: r.CreatedAt})
	}
	command := fmt.Sprintf("curl -fsSL %s/install.sh | sudo sh -s -- --controller %s --secret %s", s.publicURL, s.publicURL, s.bootstrapSecret)
	writeJSON(w, http.StatusOK, publicState{Nodes: state.Nodes, Repositories: repos, Tasks: state.Tasks, Backups: state.Backups, Operations: state.Operations, InstallCommand: command})
}

func (s *server) handleCreateRepository(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name     string `json:"name"`
		URL      string `json:"url"`
		Username string `json:"username"`
		Password string `json:"password"`
		BasePath string `json:"base_path"`
	}
	if err := decodeJSON(r, &in); err != nil || strings.TrimSpace(in.Name) == "" {
		writeError(w, http.StatusBadRequest, "名称和有效的 WebDAV 地址为必填项")
		return
	}
	u, err := url.ParseRequestURI(in.URL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		writeError(w, http.StatusBadRequest, "WebDAV 地址无效")
		return
	}
	encrypted, err := s.vault.Encrypt(in.Password)
	if err != nil {
		writeError(w, 500, "无法保存凭据")
		return
	}
	repo := model.Repository{ID: id.New("repo"), Name: strings.TrimSpace(in.Name), URL: strings.TrimRight(in.URL, "/"), Username: in.Username, PasswordEncrypted: encrypted, BasePath: strings.Trim(in.BasePath, "/"), CreatedAt: time.Now().UTC()}
	if err := s.store.Update(func(state *model.State) error { state.Repositories = append(state.Repositories, repo); return nil }); err != nil {
		writeError(w, 500, "保存失败")
		return
	}
	writeJSON(w, http.StatusCreated, repositoryView{ID: repo.ID, Name: repo.Name, URL: repo.URL, Username: repo.Username, BasePath: repo.BasePath, CreatedAt: repo.CreatedAt})
}

func (s *server) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	var task model.Task
	if decodeJSON(r, &task) != nil || strings.TrimSpace(task.Name) == "" || task.NodeID == "" || task.RepositoryID == "" {
		writeError(w, 400, "任务名称、节点和备份库为必填项")
		return
	}
	state := s.store.Snapshot()
	if !hasNode(state, task.NodeID) || !hasRepo(state, task.RepositoryID) {
		writeError(w, 400, "节点或备份库不存在")
		return
	}
	if _, ok := scheduleInterval(task.Schedule); !ok {
		writeError(w, 400, "不支持的备份频率")
		return
	}
	task.ID = id.New("task")
	task.Enabled = true
	task.CreatedAt = time.Now().UTC()
	task.Paths = cleanPaths(task.Paths)
	if err := s.store.Update(func(state *model.State) error { state.Tasks = append(state.Tasks, task); return nil }); err != nil {
		writeError(w, 500, "保存失败")
		return
	}
	writeJSON(w, 201, task)
}

func (s *server) handleRunTask(w http.ResponseWriter, r *http.Request) {
	command, err := s.queueBackup(r.PathValue("id"), true)
	if errors.Is(err, errNotFound) {
		writeError(w, 404, "任务不存在")
		return
	}
	if err != nil {
		writeError(w, 409, err.Error())
		return
	}
	writeJSON(w, 202, command)
}

func (s *server) handleRestore(w http.ResponseWriter, r *http.Request) {
	var in struct {
		NodeID  string `json:"node_id"`
		Confirm bool   `json:"confirm"`
	}
	if decodeJSON(r, &in) != nil || in.NodeID == "" || !in.Confirm {
		writeError(w, 400, "必须选择目标节点并明确确认恢复")
		return
	}
	state := s.store.Snapshot()
	var backup *model.Backup
	for i := range state.Backups {
		if state.Backups[i].ID == r.PathValue("id") {
			backup = &state.Backups[i]
			break
		}
	}
	if backup == nil {
		writeError(w, 404, "备份不存在")
		return
	}
	if !hasNode(state, in.NodeID) {
		writeError(w, 400, "目标节点不存在")
		return
	}
	operation := model.Operation{ID: id.New("op"), Type: "restore", NodeID: in.NodeID, BackupID: backup.ID, Status: "queued", CreatedAt: time.Now().UTC()}
	command := model.Command{ID: id.New("cmd"), NodeID: in.NodeID, Type: "restore", Payload: map[string]any{"repository_id": backup.RepositoryID, "backup": backup, "confirm": true, "operation_id": operation.ID}, CreatedAt: time.Now().UTC()}
	if err := s.store.Update(func(st *model.State) error {
		st.Commands = append(st.Commands, command)
		st.Operations = append([]model.Operation{operation}, st.Operations...)
		return nil
	}); err != nil {
		writeError(w, 500, "无法下发恢复任务")
		return
	}
	writeJSON(w, 202, command)
}

func (s *server) handleInstaller(w http.ResponseWriter, _ *http.Request) {
	b, _ := assets.ReadFile("assets/install-agent.sh")
	script := strings.ReplaceAll(string(b), "__RELEASE_BASE__", s.releaseBase)
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(script))
}

func (s *server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name         string `json:"name"`
		Secret       string `json:"secret"`
		OS           string `json:"os"`
		Architecture string `json:"architecture"`
		AgentVersion string `json:"agent_version"`
	}
	if decodeJSON(r, &in) != nil || subtle.ConstantTimeCompare([]byte(in.Secret), []byte(s.bootstrapSecret)) != 1 {
		writeError(w, 401, "注册密钥无效")
		return
	}
	if strings.TrimSpace(in.Name) == "" {
		in.Name = "unnamed-node"
	}
	token := randomToken(32)
	node := model.Node{ID: id.New("node"), Name: in.Name, TokenHash: hashToken(token), Status: "online", LastSeen: time.Now().UTC(), OS: in.OS, Architecture: in.Architecture, AgentVersion: in.AgentVersion, CreatedAt: time.Now().UTC()}
	if s.store.Update(func(st *model.State) error { st.Nodes = append(st.Nodes, node); return nil }) != nil {
		writeError(w, 500, "注册失败")
		return
	}
	writeJSON(w, 201, map[string]string{"node_id": node.ID, "token": token, "controller": s.publicURL})
}

func (s *server) authenticateNode(w http.ResponseWriter, r *http.Request) (model.Node, bool) {
	state := s.store.Snapshot()
	for _, n := range state.Nodes {
		if n.ID == r.PathValue("node") && subtle.ConstantTimeCompare([]byte(n.TokenHash), []byte(hashToken(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")))) == 1 {
			return n, true
		}
	}
	writeError(w, 401, "节点认证失败")
	return model.Node{}, false
}

func (s *server) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	node, ok := s.authenticateNode(w, r)
	if !ok {
		return
	}
	var in struct {
		OS           string   `json:"os"`
		Architecture string   `json:"architecture"`
		AgentVersion string   `json:"agent_version"`
		Services     []string `json:"services"`
	}
	if decodeJSON(r, &in) != nil {
		writeError(w, 400, "无效心跳")
		return
	}
	_ = s.store.Update(func(st *model.State) error {
		for i := range st.Nodes {
			if st.Nodes[i].ID == node.ID {
				st.Nodes[i].Status = "online"
				st.Nodes[i].LastSeen = time.Now().UTC()
				st.Nodes[i].OS = in.OS
				st.Nodes[i].Architecture = in.Architecture
				st.Nodes[i].AgentVersion = in.AgentVersion
				st.Nodes[i].Services = in.Services
			}
		}
		return nil
	})
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *server) handleCommands(w http.ResponseWriter, r *http.Request) {
	node, ok := s.authenticateNode(w, r)
	if !ok {
		return
	}
	var commands []model.Command
	now := time.Now().UTC()
	_ = s.store.Update(func(st *model.State) error {
		for i := range st.Commands {
			c := &st.Commands[i]
			if c.NodeID == node.ID && (c.ClaimedAt.IsZero() || now.Sub(c.ClaimedAt) > 30*time.Minute) {
				c.ClaimedAt = now
				copy, err := cloneCommand(*c)
				if err != nil {
					return err
				}
				commands = append(commands, copy)
				break
			}
		}
		return nil
	})
	for i := range commands {
		if err := s.injectRepositoryCredentials(&commands[i]); err != nil {
			writeError(w, http.StatusInternalServerError, "无法读取备份库凭据")
			return
		}
	}
	writeJSON(w, 200, map[string]any{"commands": commands})
}

func cloneCommand(command model.Command) (model.Command, error) {
	b, err := json.Marshal(command)
	if err != nil {
		return model.Command{}, err
	}
	var copy model.Command
	err = json.Unmarshal(b, &copy)
	return copy, err
}

func (s *server) injectRepositoryCredentials(command *model.Command) error {
	if command.Payload == nil {
		return errors.New("命令缺少备份库")
	}
	repositoryID, _ := command.Payload["repository_id"].(string)
	if repositoryID == "" && command.Task != nil {
		repositoryID = command.Task.RepositoryID
	}
	if repositoryID == "" {
		return errors.New("命令缺少备份库")
	}
	credentials, err := s.repositoryCredentials(repositoryID)
	if err != nil {
		return err
	}
	command.Payload["repository"] = credentials
	delete(command.Payload, "repository_id")
	return nil
}

func (s *server) handleCommandResult(w http.ResponseWriter, r *http.Request) {
	node, ok := s.authenticateNode(w, r)
	if !ok {
		return
	}
	var result model.CommandResult
	if decodeJSON(r, &result) != nil {
		writeError(w, 400, "无效结果")
		return
	}
	commandID := r.PathValue("command")
	err := s.store.Update(func(st *model.State) error {
		index := -1
		var command model.Command
		for i, c := range st.Commands {
			if c.ID == commandID && c.NodeID == node.ID {
				index = i
				command = c
				break
			}
		}
		if index < 0 {
			return errNotFound
		}
		st.Commands = append(st.Commands[:index], st.Commands[index+1:]...)
		if command.Task != nil {
			for i := range st.Tasks {
				if st.Tasks[i].ID == command.Task.ID {
					st.Tasks[i].LastStatus = result.Status
					st.Tasks[i].LastMessage = result.Message
				}
			}
		}
		if operationID, _ := command.Payload["operation_id"].(string); operationID != "" {
			for i := range st.Operations {
				if st.Operations[i].ID == operationID {
					st.Operations[i].Status = result.Status
					st.Operations[i].Message = result.Message
					st.Operations[i].Details = result.Details
					st.Operations[i].CompletedAt = time.Now().UTC()
				}
			}
		}
		if result.Backup != nil && result.Status == "success" && command.Type == "backup" && command.Task != nil {
			result.Backup.ID = id.New("backup")
			result.Backup.TaskID = command.Task.ID
			result.Backup.NodeID = node.ID
			result.Backup.CreatedAt = time.Now().UTC()
			st.Backups = append([]model.Backup{*result.Backup}, st.Backups...)
		}
		return nil
	})
	if errors.Is(err, errNotFound) {
		writeError(w, 404, "命令不存在")
		return
	}
	if err != nil {
		writeError(w, 500, "结果保存失败")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *server) repositoryCredentials(repoID string) (model.RepositoryCredentials, error) {
	for _, repo := range s.store.Snapshot().Repositories {
		if repo.ID == repoID {
			password, err := s.vault.Decrypt(repo.PasswordEncrypted)
			return model.RepositoryCredentials{URL: repo.URL, Username: repo.Username, Password: password, BasePath: repo.BasePath}, err
		}
	}
	return model.RepositoryCredentials{}, errNotFound
}

var errNotFound = errors.New("not found")

func randomToken(bytesCount int) string {
	b := make([]byte, bytesCount)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
func hashToken(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func hasNode(st model.State, id string) bool {
	for _, n := range st.Nodes {
		if n.ID == id {
			return true
		}
	}
	return false
}
func hasRepo(st model.State, id string) bool {
	for _, repo := range st.Repositories {
		if repo.ID == id {
			return true
		}
	}
	return false
}
func cleanPaths(paths []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, "/") && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}
func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'")
		next.ServeHTTP(w, r)
	})
}
func requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { next.ServeHTTP(w, r) })
}
