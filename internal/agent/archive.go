package agent

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Manifest struct {
	Version       int       `json:"version"`
	CreatedAt     time.Time `json:"created_at"`
	Hostname      string    `json:"hostname"`
	Discovery     Discovery `json:"discovery"`
	Paths         []string  `json:"paths"`
	DatabaseDumps []string  `json:"database_dumps,omitempty"`
	Warnings      []string  `json:"warnings,omitempty"`
}

var excludedPrefixes = []string{"/proc", "/sys", "/dev", "/run", "/tmp", "/var/tmp", "/var/cache", "/var/lib/vbakup", "/var/lib/docker/overlay2"}

func CreateArchive(destination string, configured []string, includeDocker, includeDatabases bool) (Manifest, error) {
	discovery := Discover()
	paths := cleanAbsolutePaths(configured)
	if len(paths) == 0 {
		paths = cleanAbsolutePaths(discovery.Paths)
	}
	host, _ := os.Hostname()
	manifest := Manifest{Version: 1, CreatedAt: time.Now().UTC(), Hostname: host, Discovery: discovery, Paths: paths}
	stage, err := os.MkdirTemp("", "vbakup-stage-")
	if err != nil {
		return manifest, err
	}
	defer os.RemoveAll(stage)
	if includeDocker && len(discovery.DockerContainers) > 0 {
		args := append([]string{"inspect"}, discovery.DockerContainers...)
		if out, e := exec.Command("docker", args...).CombinedOutput(); e == nil {
			_ = os.WriteFile(filepath.Join(stage, "docker-inspect.json"), out, 0600)
		} else {
			manifest.Warnings = append(manifest.Warnings, "docker inspect: "+e.Error())
		}
		captureDockerCompose(stage, &manifest)
	}
	if includeDatabases {
		manifest.DatabaseDumps, manifest.Warnings = dumpDatabases(stage, manifest.Warnings)
	}
	manifestBytes, _ := json.MarshalIndent(manifest, "", "  ")
	if err = os.WriteFile(filepath.Join(stage, "manifest.json"), manifestBytes, 0600); err != nil {
		return manifest, err
	}
	f, err := os.Create(destination)
	if err != nil {
		return manifest, err
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, root := range append([]string{stage}, paths...) {
		if err = addTree(tw, root, stage, &manifest); err != nil {
			_ = tw.Close()
			_ = gz.Close()
			_ = f.Close()
			return manifest, err
		}
	}
	if err = tw.Close(); err == nil {
		err = gz.Close()
	} else {
		_ = gz.Close()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return manifest, err
}

func addTree(tw *tar.Writer, root, stage string, manifest *Manifest) error {
	root = filepath.Clean(root)
	return filepath.Walk(root, func(current string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			manifest.Warnings = append(manifest.Warnings, current+": "+walkErr.Error())
			return nil
		}
		if current != stage && isExcluded(current) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.Mode()&os.ModeSocket != 0 {
			return nil
		}
		linkTarget := ""
		if info.Mode()&os.ModeSymlink != 0 {
			var linkErr error
			linkTarget, linkErr = os.Readlink(current)
			if linkErr != nil {
				manifest.Warnings = append(manifest.Warnings, current+": "+linkErr.Error())
				return nil
			}
		}
		header, err := tar.FileInfoHeader(info, linkTarget)
		if err != nil {
			return nil
		}
		if strings.HasPrefix(current, stage) {
			rel, _ := filepath.Rel(stage, current)
			header.Name = filepath.ToSlash(filepath.Join(".vbakup", rel))
		} else {
			header.Name = strings.TrimPrefix(filepath.ToSlash(current), "/")
		}
		if err = tw.WriteHeader(header); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		in, err := os.Open(current)
		if err != nil {
			manifest.Warnings = append(manifest.Warnings, current+": "+err.Error())
			return nil
		}
		_, err = io.Copy(tw, in)
		_ = in.Close()
		return err
	})
}

func ExtractArchive(source, destination string) error {
	f, err := os.Open(source)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	base, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	type pendingSymlink struct{ path, target string }
	var symlinks []pendingSymlink
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		target := filepath.Join(base, filepath.FromSlash(header.Name))
		absolute, _ := filepath.Abs(target)
		relative, relErr := filepath.Rel(base, absolute)
		if relErr != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
			return fmt.Errorf("unsafe archive path %q", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err = os.MkdirAll(absolute, header.FileInfo().Mode()); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err = os.MkdirAll(filepath.Dir(absolute), 0750); err != nil {
				return err
			}
			out, err := os.OpenFile(absolute, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, header.FileInfo().Mode())
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(out, tr)
			closeErr := out.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		case tar.TypeSymlink:
			symlinks = append(symlinks, pendingSymlink{path: absolute, target: header.Linkname})
		}
	}
	for _, link := range symlinks {
		if err = os.MkdirAll(filepath.Dir(link.path), 0750); err != nil {
			return err
		}
		if _, err = os.Lstat(link.path); err == nil {
			return fmt.Errorf("symlink path already exists %q", link.path)
		}
		if !os.IsNotExist(err) {
			return err
		}
		if err = os.Symlink(link.target, link.path); err != nil {
			return err
		}
	}
	return nil
}

func ReadManifest(extractedRoot string) (Manifest, error) {
	var m Manifest
	b, err := os.ReadFile(filepath.Join(extractedRoot, ".vbakup", "manifest.json"))
	if err != nil {
		return m, err
	}
	err = json.Unmarshal(b, &m)
	return m, err
}
func FileSHA256(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	size, err := io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), size, err
}

func dumpDatabases(stage string, warnings []string) ([]string, []string) {
	var dumps []string
	definitions := []struct {
		name, file string
		args       []string
	}{{"mysqldump", "mysql.sql", []string{"--all-databases", "--single-transaction", "--routines", "--events"}}, {"pg_dumpall", "postgresql.sql", nil}}
	for _, d := range definitions {
		if _, err := exec.LookPath(d.name); err != nil {
			continue
		}
		file := filepath.Join(stage, d.file)
		out, err := os.Create(file)
		if err != nil {
			continue
		}
		cmd := exec.Command(d.name, d.args...)
		cmd.Stdout = out
		err = cmd.Run()
		_ = out.Close()
		if err != nil {
			warnings = append(warnings, d.name+": "+err.Error())
			_ = os.Remove(file)
		} else {
			dumps = append(dumps, d.file)
		}
	}
	if _, err := exec.LookPath("redis-cli"); err == nil {
		file := filepath.Join(stage, "redis.rdb")
		if err = exec.Command("redis-cli", "--rdb", file).Run(); err != nil {
			warnings = append(warnings, "redis-cli: "+err.Error())
		} else {
			dumps = append(dumps, "redis.rdb")
		}
	}
	return dumps, warnings
}
func captureDockerCompose(stage string, manifest *Manifest) {
	for _, dir := range manifest.Discovery.ComposeProjects {
		for _, name := range []string{"compose.yaml", "compose.yml", "docker-compose.yml", "docker-compose.yaml", ".env"} {
			source := filepath.Join(dir, name)
			if b, err := os.ReadFile(source); err == nil {
				safe := composeMetadataName(dir)
				_ = os.MkdirAll(filepath.Join(stage, "compose", safe), 0700)
				_ = os.WriteFile(filepath.Join(stage, "compose", safe, name), b, 0600)
			}
		}
	}
}
func composeMetadataName(directory string) string {
	return strings.Trim(strings.ReplaceAll(filepath.ToSlash(filepath.Clean(directory)), "/", "_"), "_")
}
func cleanAbsolutePaths(paths []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range paths {
		p = filepath.Clean(strings.TrimSpace(p))
		if filepath.IsAbs(p) && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	var compact []string
	for _, candidate := range out {
		covered := false
		for _, parent := range out {
			if candidate != parent && strings.HasPrefix(candidate, strings.TrimRight(parent, string(os.PathSeparator))+string(os.PathSeparator)) {
				covered = true
				break
			}
		}
		if !covered {
			compact = append(compact, candidate)
		}
	}
	return compact
}
func isExcluded(value string) bool {
	clean := filepath.ToSlash(filepath.Clean(value))
	for _, prefix := range excludedPrefixes {
		if clean == prefix || strings.HasPrefix(clean, prefix+"/") {
			return true
		}
	}
	return false
}
