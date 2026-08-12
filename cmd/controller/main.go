package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/J2026-dev/vbakup/internal/model"
	"github.com/J2026-dev/vbakup/internal/store"
	"github.com/J2026-dev/vbakup/internal/vault"
)

func main() {
	dataDir := env("VBAKUP_DATA", "./data")
	state, err := store.Open(filepath.Join(dataDir, "state.json"))
	if err != nil {
		log.Fatal(err)
	}
	secrets, err := vault.Open(dataDir)
	if err != nil {
		log.Fatal(err)
	}

	bootstrap := os.Getenv("VBAKUP_BOOTSTRAP_SECRET")
	if bootstrap == "" {
		bootstrap = randomToken(24)
		log.Printf("generated bootstrap secret: %s", bootstrap)
	}
	adminPassword := os.Getenv("VBAKUP_ADMIN_PASSWORD")
	if adminPassword == "" {
		adminPassword = randomToken(24)
		log.Printf("generated admin password: %s", adminPassword)
	}
	if encrypted := state.Snapshot().Settings.AdminPasswordEncrypted; encrypted != "" {
		if storedPassword, decryptErr := secrets.Decrypt(encrypted); decryptErr == nil && storedPassword != "" {
			adminPassword = storedPassword
		}
	}
	if state.Snapshot().Settings.AdminPasswordEncrypted == "" {
		if encrypted, encryptErr := secrets.Encrypt(adminPassword); encryptErr == nil {
			_ = state.Update(func(st *model.State) error { st.Settings.AdminPasswordEncrypted = encrypted; return nil })
		}
	}

	app := &server{
		store: state, vault: secrets,
		publicURL:       strings.TrimRight(env("VBAKUP_PUBLIC_URL", "http://localhost:8080"), "/"),
		releaseBase:     strings.TrimRight(env("VBAKUP_RELEASE_BASE", "https://github.com/J2026-dev/vbakup/releases/latest/download"), "/"),
		bootstrapSecret: bootstrap, adminUser: env("VBAKUP_ADMIN_USER", "admin"), adminPassword: adminPassword, sessions: map[string]uint64{}, sessionEpoch: state.Snapshot().Settings.AdminSessionEpoch,
	}
	go app.runScheduler(time.Minute)
	addr := env("VBAKUP_LISTEN", ":8080")
	log.Printf("vBakup controller listening on %s", addr)
	httpServer := &http.Server{Addr: addr, Handler: app.routes(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second}
	log.Fatal(httpServer.ListenAndServe())
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
