package agent

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCopyExactWithPaddingKeepsDeclaredLength(t *testing.T) {
	var output bytes.Buffer
	written, err := copyExactWithPadding(&output, strings.NewReader("abc"), 8)
	if err == nil || written != 3 {
		t.Fatalf("written=%d err=%v", written, err)
	}
	if output.Len() != 8 || string(output.Bytes()[:3]) != "abc" {
		t.Fatalf("output=%v", output.Bytes())
	}
	if !bytes.Equal(output.Bytes()[3:], make([]byte, 5)) {
		t.Fatalf("padding=%v", output.Bytes()[3:])
	}
}

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
	if _, err := createArchive(archive, []string{source}, false, false, Discovery{}); err != nil {
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

func TestArchiveManifestIncludesDiscoveredAndConfiguredData(t *testing.T) {
	// t.TempDir is under /tmp on Linux, which the production backup policy
	// intentionally excludes. Use the package directory so this test exercises
	// configured and discovered paths rather than the /tmp exclusion rule.
	root, err := os.MkdirTemp(".", ".vbakup-configured-test-")
	if err != nil {
		t.Fatal(err)
	}
	discovered, err := os.MkdirTemp(".", ".vbakup-discovered-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	defer os.RemoveAll(discovered)
	root, err = filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	discovered, err = filepath.Abs(discovered)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "app.conf"), []byte("service=true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(discovered, "app.log"), []byte("started\n"), 0600); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "backup.tar.gz")
	manifest, err := createArchive(archive, []string{root}, false, false, Discovery{Paths: []string{discovered}})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Files < 2 || manifest.Bytes < int64(len("service=true\nstarted\n")) {
		t.Fatalf("manifest=%+v", manifest)
	}
	if len(manifest.Paths) != 2 {
		t.Fatalf("paths=%v", manifest.Paths)
	}
	// Production agents run on Linux. Windows drive-letter paths are not valid
	// tar paths and extraction behavior is covered by the portable tests above.
	if runtime.GOOS == "windows" {
		return
	}
	extracted := t.TempDir()
	if err := ExtractArchive(archive, extracted); err != nil {
		t.Fatal(err)
	}
	archivedManifest, err := ReadManifest(extracted)
	if err != nil {
		t.Fatalf("manifest missing from archive: %v", err)
	}
	if archivedManifest.Files < 2 || archivedManifest.Bytes < int64(len("service=true\nstarted\n")) {
		t.Fatalf("archived manifest=%+v", archivedManifest)
	}
	for _, item := range []struct{ root, name string }{{root, "app.conf"}, {discovered, "app.log"}} {
		if _, err := os.Stat(filepath.Join(extracted, filepath.FromSlash(strings.TrimPrefix(filepath.ToSlash(item.root), "/")), item.name)); err != nil {
			t.Fatalf("%s missing: %v", item.name, err)
		}
	}
}
