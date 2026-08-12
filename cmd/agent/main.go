package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	agentlib "github.com/J2026-dev/vbakup/internal/agent"
	"github.com/J2026-dev/vbakup/internal/model"
	"github.com/J2026-dev/vbakup/internal/webdav"
)

const version = "0.1.0"

type config struct {
	Controller string `json:"controller"`
	NodeID     string `json:"node_id"`
	Token      string `json:"token"`
}
type app struct {
	config      config
	client      *http.Client
	resultsPath string
}

func main() {
	configPath := env("VBAKUP_CONFIG", "/etc/vbakup/agent.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		log.Fatal("read config: ", err)
	}
	var cfg config
	if err = json.Unmarshal(data, &cfg); err != nil || cfg.Controller == "" || cfg.NodeID == "" || cfg.Token == "" {
		log.Fatal("invalid agent config")
	}
	runner := &app{config: cfg, resultsPath: env("VBAKUP_RESULTS", "/var/lib/vbakup/results.json"), client: &http.Client{Transport: &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 90 * time.Second,
	}}}
	log.Printf("vBakup agent %s started", version)
	runner.loop()
}
func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func (a *app) loop() {
	for {
		if err := a.cycle(); err != nil {
			log.Printf("controller cycle: %v", err)
		}
		time.Sleep(30 * time.Second)
	}
}
func (a *app) cycle() error {
	discovery := agentlib.Discover()
	if err := a.post("/api/agent/"+a.config.NodeID+"/heartbeat", map[string]any{"os": runtime.GOOS, "architecture": runtime.GOARCH, "agent_version": version, "services": agentlib.ServiceNames(discovery)}, nil); err != nil {
		return err
	}
	var response struct {
		Commands []model.Command `json:"commands"`
	}
	if err := a.get("/api/agent/"+a.config.NodeID+"/commands", &response); err != nil {
		return err
	}
	for _, command := range response.Commands {
		results, err := loadResults(a.resultsPath)
		if err != nil {
			log.Printf("load command results: %v", err)
			continue
		}
		result, exists := results[command.ID]
		if !exists {
			result = a.execute(command)
			results[command.ID] = result
			if err = saveResults(a.resultsPath, results); err != nil {
				log.Printf("save command %s result: %v", command.ID, err)
				continue
			}
		}
		if err := a.post("/api/agent/"+a.config.NodeID+"/commands/"+command.ID+"/result", result, nil); err != nil {
			log.Printf("report command %s: %v", command.ID, err)
			continue
		}
		delete(results, command.ID)
		if err = saveResults(a.resultsPath, results); err != nil {
			log.Printf("remove reported command %s result: %v", command.ID, err)
		}
	}
	return nil
}

func loadResults(path string) (map[string]model.CommandResult, error) {
	results := map[string]model.CommandResult{}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return results, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(b, &results); err != nil {
		return nil, err
	}
	return results, nil
}

func saveResults(path string, results map[string]model.CommandResult) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	b, err := json.Marshal(results)
	if err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err = os.WriteFile(temporary, b, 0600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func (a *app) execute(command model.Command) model.CommandResult {
	switch command.Type {
	case "backup":
		return a.backup(command)
	case "restore":
		return a.restore(command)
	default:
		return failure("unknown command: " + command.Type)
	}
}
func (a *app) backup(command model.Command) model.CommandResult {
	if command.Task == nil {
		return failure("missing task")
	}
	repo, err := credentials(command.Payload)
	if err != nil {
		return failure(err.Error())
	}
	tmp, err := os.CreateTemp("", "vbakup-*.tar.gz")
	if err != nil {
		return failure(err.Error())
	}
	archive := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(archive)
	manifest, err := agentlib.CreateArchive(archive, command.Task.Paths, command.Task.IncludeDocker, command.Task.IncludeDatabases)
	if err != nil {
		return failure(err.Error())
	}
	hash, size, err := agentlib.FileSHA256(archive)
	if err != nil {
		return failure(err.Error())
	}
	remote := path.Join(repo.BasePath, a.config.NodeID, command.Task.ID, time.Now().UTC().Format("20060102T150405Z")+".tar.gz")
	file, err := os.Open(archive)
	if err != nil {
		return failure(err.Error())
	}
	defer file.Close()
	dav, err := webdav.New(repo.URL, repo.Username, repo.Password)
	if err != nil {
		return failure(err.Error())
	}
	if err = dav.Put(remote, file); err != nil {
		return failure(err.Error())
	}
	message := "backup uploaded"
	if len(manifest.Warnings) > 0 {
		message = fmt.Sprintf("backup uploaded with %d warning(s)", len(manifest.Warnings))
	}
	return model.CommandResult{Status: "success", Message: message, Backup: &model.Backup{RepositoryID: command.Task.RepositoryID, RemotePath: remote, Size: size, SHA256: hash, Services: agentlib.ServiceNames(manifest.Discovery)}}
}

func (a *app) restore(command model.Command) model.CommandResult {
	repo, err := credentials(command.Payload)
	if err != nil {
		return failure(err.Error())
	}
	raw, ok := command.Payload["backup"]
	if !ok {
		return failure("missing backup")
	}
	data, _ := json.Marshal(raw)
	var backup model.Backup
	if err = json.Unmarshal(data, &backup); err != nil {
		return failure("invalid backup")
	}
	confirmed, _ := command.Payload["confirm"].(bool)
	if !confirmed {
		return failure("restore not confirmed")
	}
	dav, err := webdav.New(repo.URL, repo.Username, repo.Password)
	if err != nil {
		return failure(err.Error())
	}
	body, err := dav.Get(backup.RemotePath)
	if err != nil {
		return failure(err.Error())
	}
	defer body.Close()
	tmp, err := os.CreateTemp("", "vbakup-restore-*.tar.gz")
	if err != nil {
		return failure(err.Error())
	}
	archive := tmp.Name()
	defer os.Remove(archive)
	if _, err = io.Copy(tmp, body); err != nil {
		_ = tmp.Close()
		return failure(err.Error())
	}
	_ = tmp.Close()
	hash, _, err := agentlib.FileSHA256(archive)
	if err != nil || !strings.EqualFold(hash, backup.SHA256) {
		return failure("backup checksum mismatch")
	}
	if err = os.MkdirAll("/var/lib/vbakup", 0700); err != nil {
		return failure(err.Error())
	}
	stage, err := os.MkdirTemp("/var/lib/vbakup", "restore-")
	if err != nil {
		return failure(err.Error())
	}
	defer os.RemoveAll(stage)
	if err = agentlib.ExtractArchive(archive, stage); err != nil {
		return failure(err.Error())
	}
	manifest, err := agentlib.ReadManifest(stage)
	if err != nil {
		return failure("manifest validation failed: " + err.Error())
	}
	warnings := agentlib.StopServices(manifest)
	if err = copyRestoreTree(stage, "/"); err != nil {
		return failure("file restore failed: " + err.Error())
	}
	warnings = append(warnings, agentlib.RestoreServices(stage, "/", manifest)...)
	return model.CommandResult{Status: "success", Message: fmt.Sprintf("restore completed with %d warning(s)", len(warnings)), Details: map[string]any{"warnings": warnings}}
}

func credentials(payload map[string]any) (model.RepositoryCredentials, error) {
	raw, ok := payload["repository"]
	if !ok {
		return model.RepositoryCredentials{}, errors.New("missing repository credentials")
	}
	data, _ := json.Marshal(raw)
	var repo model.RepositoryCredentials
	if err := json.Unmarshal(data, &repo); err != nil || repo.URL == "" {
		return repo, errors.New("invalid repository credentials")
	}
	return repo, nil
}
func failure(message string) model.CommandResult {
	return model.CommandResult{Status: "failed", Message: message}
}
func (a *app) get(endpoint string, out any) error {
	return a.request(http.MethodGet, endpoint, nil, out)
}
func (a *app) post(endpoint string, in, out any) error {
	return a.request(http.MethodPost, endpoint, in, out)
}
func (a *app) request(method, endpoint string, in, out any) error {
	var body io.Reader
	if in != nil {
		var buffer bytes.Buffer
		if err := json.NewEncoder(&buffer).Encode(in); err != nil {
			return err
		}
		body = &buffer
	}
	request, err := http.NewRequest(method, strings.TrimRight(a.config.Controller, "/")+endpoint, body)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+a.config.Token)
	request.Header.Set("Content-Type", "application/json")
	response, err := a.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("controller %s: %s", response.Status, strings.TrimSpace(string(message)))
	}
	if out != nil {
		return json.NewDecoder(response.Body).Decode(out)
	}
	return nil
}
func copyRestoreTree(source, destination string) error {
	return filepath.Walk(source, func(current string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, current)
		if err != nil || relative == "." || strings.HasPrefix(relative, ".vbakup") {
			return err
		}
		target := filepath.Join(destination, relative)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(current)
			if err != nil {
				return err
			}
			_ = os.Remove(target)
			return os.Symlink(link, target)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		in, err := os.Open(current)
		if err != nil {
			return err
		}
		if err = os.MkdirAll(filepath.Dir(target), 0750); err != nil {
			_ = in.Close()
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode())
		if err != nil {
			_ = in.Close()
			return err
		}
		_, copyErr := io.Copy(out, in)
		inputCloseErr := in.Close()
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if inputCloseErr != nil {
			return inputCloseErr
		}
		return closeErr
	})
}
