package agent

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	Files         int64     `json:"files"`
	Bytes         int64     `json:"bytes"`
}

var excludedPrefixes = []string{
	"/proc", "/sys", "/dev", "/run", "/tmp", "/var/tmp", "/var/cache",
	"/etc/vbakup", "/var/lib/vbakup", "/usr/local/bin/vbakup-agent", "/usr/local/bin/vbakup-agentctl",
	"/etc/systemd/system/vbakup-agent.service", "/etc/systemd/system/vbakup-agent-update.service",
	"/etc/systemd/system/vbakup-agent-update.timer", "/etc/init.d/vbakup-agent",
	"/var/lib/docker/overlay2",
}

func CreateArchive(destination string, configured []string, includeDocker, includeDatabases bool) (Manifest, error) {
	return createArchive(destination, configured, includeDocker, includeDatabases, Discover())
}

func createArchive(destination string, configured []string, includeDocker, includeDatabases bool, discovery Discovery) (Manifest, error) {
	paths := cleanAbsolutePaths(append(append([]string{}, configured...), discovery.Paths...))
	host, _ := os.Hostname()
	manifest := Manifest{Version: 1, CreatedAt: time.Now().UTC(), Hostname: host, Discovery: discovery, Paths: paths}
	stage, err := os.MkdirTemp("", "vbakup-stage-")
	if err != nil {
		return manifest, err
	}
	defer os.RemoveAll(stage)
	var restartContainers []string
	restarted := false
	if includeDocker && len(discovery.DockerContainers) > 0 {
		// Logical dumps must run while containers are still available. They are
		// written into the staging area and then included with the stopped-volume
		// archive below.
		if includeDatabases {
			manifest.DatabaseDumps, manifest.Warnings = dumpDockerDatabases(stage, discovery.DockerContainers, manifest.DatabaseDumps, manifest.Warnings)
		}
		// Stop running containers before database exports and filesystem traversal
		// so mounted volumes and application state are captured consistently.
		restartContainers, manifest.Warnings = stopRunningContainers(discovery.DockerContainers, manifest.Warnings)
		defer func() {
			if len(restartContainers) > 0 && !restarted {
				_, manifest.Warnings = startContainers(restartContainers, manifest.Warnings)
			}
		}()
		args := append([]string{"inspect"}, discovery.DockerContainers...)
		if out, e := exec.Command("docker", args...).CombinedOutput(); e == nil {
			_ = os.WriteFile(filepath.Join(stage, "docker-inspect.json"), out, 0600)
		} else {
			manifest.Warnings = append(manifest.Warnings, "docker inspect: "+e.Error())
		}
		captureDockerCompose(stage, &manifest)
	}
	if includeDatabases {
		var hostDumps []string
		hostDumps, manifest.Warnings = dumpDatabases(stage, manifest.Warnings)
		manifest.DatabaseDumps = append(manifest.DatabaseDumps, hostDumps...)
	}
	f, err := os.Create(destination)
	if err != nil {
		return manifest, err
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	// Add user/application roots first so traversal warnings and byte counts
	// are final before the manifest is written into the archive.
	for _, root := range paths {
		if err = addTree(tw, root, stage, &manifest); err != nil {
			_ = tw.Close()
			_ = gz.Close()
			_ = f.Close()
			return manifest, err
		}
	}
	// Restart containers before writing the manifest so restart warnings are
	// persisted in the archive as well as returned to the controller.
	if len(restartContainers) > 0 {
		_, manifest.Warnings = startContainers(restartContainers, manifest.Warnings)
		restarted = true
	}
	manifestBytes, _ := json.MarshalIndent(manifest, "", "  ")
	if err = os.WriteFile(filepath.Join(stage, "manifest.json"), manifestBytes, 0600); err != nil {
		_ = tw.Close()
		_ = gz.Close()
		_ = f.Close()
		return manifest, err
	}
	if err = addTree(tw, stage, stage, &manifest); err != nil {
		_ = tw.Close()
		_ = gz.Close()
		_ = f.Close()
		return manifest, err
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

func stopRunningContainers(containers, warnings []string) ([]string, []string) {
	var running []string
	for _, name := range containers {
		if out, err := exec.Command("docker", "inspect", "--format", "{{.State.Running}}", name).Output(); err == nil && strings.TrimSpace(string(out)) == "true" {
			if err := exec.Command("docker", "stop", "-t", "30", name).Run(); err != nil {
				warnings = append(warnings, "docker stop "+name+": "+err.Error())
				continue
			}
			running = append(running, name)
		}
	}
	return running, warnings
}

func startContainers(containers, warnings []string) ([]string, []string) {
	for _, name := range containers {
		if err := exec.Command("docker", "start", name).Run(); err != nil {
			warnings = append(warnings, "docker start "+name+": "+err.Error())
		}
	}
	return containers, warnings
}

func addTree(tw *tar.Writer, root, stage string, manifest *Manifest) error {
	root = filepath.Clean(root)
	return filepath.Walk(root, func(current string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			manifest.Warnings = append(manifest.Warnings, current+": "+walkErr.Error())
			return nil
		}
		inStage := current == stage || strings.HasPrefix(current, stage+string(os.PathSeparator))
		if !inStage && isExcluded(current) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.Mode()&os.ModeSocket != 0 {
			return nil
		}
		if info.Mode().IsRegular() {
			return addRegularFile(tw, current, stage, manifest)
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
		header.Name = archiveHeaderName(current, stage)
		if err = tw.WriteHeader(header); err != nil {
			return err
		}
		return nil
	})
}

func addRegularFile(tw *tar.Writer, current, stage string, manifest *Manifest) error {
	in, err := os.Open(current)
	if err != nil {
		manifest.Warnings = append(manifest.Warnings, current+": "+err.Error())
		return nil
	}
	defer in.Close()

	// Stat the opened descriptor so the tar header and bytes refer to the same
	// file even when a log is renamed or replaced during traversal.
	info, err := in.Stat()
	if err != nil {
		manifest.Warnings = append(manifest.Warnings, current+": "+err.Error())
		return nil
	}
	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		manifest.Warnings = append(manifest.Warnings, current+": "+err.Error())
		return nil
	}
	header.Name = archiveHeaderName(current, stage)
	if err = tw.WriteHeader(header); err != nil {
		return err
	}
	manifest.Files++
	manifest.Bytes += info.Size()

	written, copyErr := copyExactWithPadding(tw, in, info.Size())
	if copyErr == nil {
		return nil
	}
	if !errors.Is(copyErr, io.ErrUnexpectedEOF) {
		return copyErr
	}
	missing := info.Size() - written
	manifest.Warnings = append(manifest.Warnings, fmt.Sprintf("%s changed while archiving: padded %d missing byte(s): %v", current, missing, copyErr))
	return nil
}

func archiveHeaderName(current, stage string) string {
	if current == stage || strings.HasPrefix(current, stage+string(os.PathSeparator)) {
		rel, _ := filepath.Rel(stage, current)
		return filepath.ToSlash(filepath.Join(".vbakup", rel))
	}
	return strings.TrimPrefix(filepath.ToSlash(current), "/")
}

func writeZeroPadding(writer io.Writer, count int64) error {
	zeros := make([]byte, 32*1024)
	for count > 0 {
		chunk := int64(len(zeros))
		if count < chunk {
			chunk = count
		}
		if _, err := writer.Write(zeros[:int(chunk)]); err != nil {
			return err
		}
		count -= chunk
	}
	return nil
}

func copyExactWithPadding(writer io.Writer, reader io.Reader, expected int64) (int64, error) {
	written, err := io.Copy(writer, io.LimitReader(reader, expected))
	if err != nil || written >= expected {
		return written, err
	}
	if paddingErr := writeZeroPadding(writer, expected-written); paddingErr != nil {
		return written, paddingErr
	}
	return written, io.ErrUnexpectedEOF
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

func dumpDockerDatabases(stage string, containers, dumps, warnings []string) ([]string, []string) {
	for _, container := range containers {
		image, err := exec.Command("docker", "inspect", "--format", "{{.Config.Image}}", container).Output()
		if err != nil {
			continue
		}
		kind := strings.ToLower(strings.TrimSpace(string(image)))
		switch {
		case strings.Contains(kind, "mysql"), strings.Contains(kind, "mariadb"):
			name := "mysql-" + safeMetadataName(container) + ".sql"
			file := filepath.Join(stage, name)
			if err := dockerExecToFile(container, file, "sh", "-c", "mysqldump --all-databases --single-transaction --routines --events -uroot 2>/dev/null"); err != nil {
				warnings = append(warnings, "docker mysql dump "+container+": "+err.Error())
			} else {
				dumps = append(dumps, name)
			}
		case strings.Contains(kind, "postgres"):
			name := "postgresql-" + safeMetadataName(container) + ".sql"
			file := filepath.Join(stage, name)
			if err := dockerExecToFile(container, file, "sh", "-c", "pg_dumpall -U postgres 2>/dev/null"); err != nil {
				warnings = append(warnings, "docker postgres dump "+container+": "+err.Error())
			} else {
				dumps = append(dumps, name)
			}
		case strings.Contains(kind, "redis"):
			name := "redis-" + safeMetadataName(container) + ".rdb"
			file := filepath.Join(stage, name)
			if err := dockerExecToFile(container, file, "sh", "-c", "redis-cli --rdb /tmp/vbakup.rdb >/dev/null 2>&1 && cat /tmp/vbakup.rdb"); err != nil {
				warnings = append(warnings, "docker redis dump "+container+": "+err.Error())
			} else {
				dumps = append(dumps, name)
			}
		}
	}
	return dumps, warnings
}

func dockerExecToFile(container, destination string, command ...string) error {
	out, err := os.Create(destination)
	if err != nil {
		return err
	}
	cmd := exec.Command("docker", append([]string{"exec", container}, command...)...)
	cmd.Stdout = out
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	closeErr := out.Close()
	if runErr != nil {
		_ = os.Remove(destination)
		if message := strings.TrimSpace(stderr.String()); message != "" {
			return fmt.Errorf("%w: %s", runErr, message)
		}
		return runErr
	}
	if closeErr != nil {
		return closeErr
	}
	return nil
}

func safeMetadataName(value string) string {
	value = strings.TrimSpace(value)
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
		if b.Len() >= 48 {
			break
		}
	}
	if b.Len() == 0 {
		return "container"
	}
	return b.String()
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
