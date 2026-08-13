package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRestoreServicesReadsMetadataFromStage(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("agent restore semantics are Linux-only")
	}
	stage := t.TempDir()
	destination := t.TempDir()
	metadata := filepath.Join(stage, ".vbakup")
	if err := os.MkdirAll(metadata, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(metadata, "redis.rdb"), []byte("redis-data"), 0600); err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{DatabaseDumps: []string{"redis.rdb"}}
	_ = RestoreServices(stage, destination, manifest)
	got, err := os.ReadFile(filepath.Join(destination, "var", "lib", "redis", "dump.rdb"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "redis-data" {
		t.Fatalf("restored %q", got)
	}
}

func TestDestinationPathStaysWithinRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("agent restore semantics are Linux-only")
	}
	root := t.TempDir()
	got, err := destinationPath(root, "/opt/app")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "opt", "app")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if _, err = destinationPath(root, "relative"); err == nil {
		t.Fatal("expected relative path rejection")
	}
}

func TestRestoreComposeMetadataToOriginalDirectory(t *testing.T) {
	metadata := t.TempDir()
	destination := t.TempDir()
	if err := os.WriteFile(filepath.Join(metadata, "compose.yaml"), []byte("services: {}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(metadata, ".env"), []byte("SECRET=value"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := restoreComposeMetadata(metadata, destination); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"compose.yaml", ".env"} {
		if _, err := os.Stat(filepath.Join(destination, name)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestRestorableServicesPreserveManifestV2State(t *testing.T) {
	manifest := Manifest{Version: 2, Discovery: Discovery{Services: []Service{{Name: "sing-box", Manager: "systemd", Unit: "sing-box", WasActive: true, WasEnabled: true}}}}
	services := restorableServices(manifest)
	if len(services) != 1 || services[0].Unit != "sing-box" || !services[0].WasActive || !services[0].WasEnabled {
		t.Fatalf("services=%+v", services)
	}
}

func TestRestorableServicesSupportsLegacySingBoxManifest(t *testing.T) {
	manifest := Manifest{Version: 1, Discovery: Discovery{Services: []Service{{Name: "sing-box"}}}}
	services := restorableServices(manifest)
	if len(services) != 1 || services[0].Manager != "systemd" || services[0].Unit != "sing-box" {
		t.Fatalf("services=%+v", services)
	}
}
