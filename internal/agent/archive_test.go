package agent

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestExtractRejectsTraversal(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "bad.tar.gz")
	f, _ := os.Create(archive)
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "../../escape", Mode: 0600, Size: 1})
	_, _ = tw.Write([]byte("x"))
	_ = tw.Close()
	_ = gz.Close()
	_ = f.Close()
	if err := ExtractArchive(archive, t.TempDir()); err == nil {
		t.Fatal("expected traversal rejection")
	}
}

func TestExtractRelativeSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows symlinks require elevated privileges")
	}
	archive := filepath.Join(t.TempDir(), "link.tar.gz")
	f, _ := os.Create(archive)
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "etc/service/config", Mode: 0600, Size: 2, Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte("ok"))
	_ = tw.WriteHeader(&tar.Header{Name: "etc/service/current", Linkname: "config", Typeflag: tar.TypeSymlink})
	_ = tw.Close()
	_ = gz.Close()
	_ = f.Close()

	destination := t.TempDir()
	if err := ExtractArchive(archive, destination); err != nil {
		t.Fatal(err)
	}
	link, err := os.Readlink(filepath.Join(destination, "etc", "service", "current"))
	if err != nil || link != "config" {
		t.Fatalf("link=%q err=%v", link, err)
	}
}

func TestExtractDoesNotWriteThroughEscapingSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("agent restore semantics are Linux-only")
	}
	archive := filepath.Join(t.TempDir(), "bad-link.tar.gz")
	f, _ := os.Create(archive)
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "etc/link", Linkname: "../../escape", Typeflag: tar.TypeSymlink})
	_ = tw.Close()
	_ = gz.Close()
	_ = f.Close()
	destination := t.TempDir()
	if err := ExtractArchive(archive, destination); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(destination), "escape")); !os.IsNotExist(err) {
		t.Fatal("archive wrote outside destination")
	}
}

func TestArchivePreservesSymlinkTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows symlinks require elevated privileges")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.MkdirTemp(home, ".vbakup-symlink-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(source)
	source, err = filepath.Abs(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "config"), []byte("ok"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("config", filepath.Join(source, "current")); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "backup.tar.gz")
	if _, err := CreateArchive(archive, []string{source}, false, false); err != nil {
		t.Fatal(err)
	}
	destination := t.TempDir()
	if err := ExtractArchive(archive, destination); err != nil {
		t.Fatal(err)
	}
	relative := strings.TrimPrefix(filepath.ToSlash(source), "/")
	link, err := os.Readlink(filepath.Join(destination, filepath.FromSlash(relative), "current"))
	if err != nil || link != "config" {
		t.Fatalf("link=%q err=%v", link, err)
	}
}
