package model

import "time"

type State struct {
	Nodes        []Node       `json:"nodes"`
	Repositories []Repository `json:"repositories"`
	Tasks        []Task       `json:"tasks"`
	Commands     []Command    `json:"commands"`
	Backups      []Backup     `json:"backups"`
	Operations   []Operation  `json:"operations"`
	Settings     Settings     `json:"settings"`
}

type Settings struct {
	PublicURL              string `json:"public_url"`
	AdminPasswordEncrypted string `json:"admin_password_encrypted,omitempty"`
	AdminSessionEpoch      uint64 `json:"admin_session_epoch,omitempty"`
}

type Node struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	Note             string    `json:"note,omitempty"`
	TokenHash        string    `json:"token_hash,omitempty"`
	Status           string    `json:"status"`
	LastSeen         time.Time `json:"last_seen"`
	OS               string    `json:"os,omitempty"`
	Architecture     string    `json:"architecture,omitempty"`
	Services         []string  `json:"services,omitempty"`
	DiscoveredPaths  []string  `json:"discovered_paths,omitempty"`
	AgentVersion     string    `json:"agent_version,omitempty"`
	AutoUpdate       bool      `json:"auto_update"`
	UptimeSeconds    int64     `json:"uptime_seconds,omitempty"`
	Load1            float64   `json:"load_1,omitempty"`
	MemoryTotal      uint64    `json:"memory_total,omitempty"`
	MemoryUsed       uint64    `json:"memory_used,omitempty"`
	DiskTotal        uint64    `json:"disk_total,omitempty"`
	DiskUsed         uint64    `json:"disk_used,omitempty"`
	DockerContainers int       `json:"docker_containers,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

type Repository struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	URL               string    `json:"url"`
	Username          string    `json:"username,omitempty"`
	PasswordEncrypted string    `json:"password_encrypted,omitempty"`
	BasePath          string    `json:"base_path"`
	CreatedAt         time.Time `json:"created_at"`
}

type RepositoryCredentials struct {
	URL      string `json:"url"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	BasePath string `json:"base_path"`
}

type Task struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	NodeID           string    `json:"node_id"`
	RepositoryID     string    `json:"repository_id"`
	Schedule         string    `json:"schedule"`
	Paths            []string  `json:"paths,omitempty"`
	IncludeDocker    bool      `json:"include_docker"`
	IncludeDatabases bool      `json:"include_databases"`
	Enabled          bool      `json:"enabled"`
	LastRun          time.Time `json:"last_run,omitempty"`
	LastStatus       string    `json:"last_status,omitempty"`
	LastMessage      string    `json:"last_message,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

type Command struct {
	ID        string         `json:"id"`
	NodeID    string         `json:"node_id"`
	Type      string         `json:"type"`
	Task      *Task          `json:"task,omitempty"`
	Payload   map[string]any `json:"payload,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	ClaimedAt time.Time      `json:"claimed_at,omitempty"`
}

type CommandResult struct {
	Status  string         `json:"status"`
	Message string         `json:"message,omitempty"`
	Backup  *Backup        `json:"backup,omitempty"`
	Details map[string]any `json:"details,omitempty"`
}

type Backup struct {
	ID           string    `json:"id"`
	TaskID       string    `json:"task_id"`
	NodeID       string    `json:"node_id"`
	NodeName     string    `json:"node_name,omitempty"`
	RepositoryID string    `json:"repository_id"`
	RemotePath   string    `json:"remote_path"`
	Size         int64     `json:"size"`
	SHA256       string    `json:"sha256"`
	Services     []string  `json:"services,omitempty"`
	Files        int64     `json:"files,omitempty"`
	ArchiveBytes int64     `json:"archive_bytes,omitempty"`
	Warnings     []string  `json:"warnings,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type Operation struct {
	ID          string         `json:"id"`
	Type        string         `json:"type"`
	NodeID      string         `json:"node_id"`
	BackupID    string         `json:"backup_id,omitempty"`
	Status      string         `json:"status"`
	Message     string         `json:"message,omitempty"`
	Details     map[string]any `json:"details,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	CompletedAt time.Time      `json:"completed_at,omitempty"`
}
