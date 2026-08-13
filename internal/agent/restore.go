package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func StopServices(manifest Manifest) []string {
	var warnings []string
	for _, service := range restorableServices(manifest) {
		if manifest.Version >= 2 && !service.WasActive {
			continue
		}
		if err := serviceCommand(service.Manager, "stop", service.Unit); err != nil {
			warnings = append(warnings, fmt.Sprintf("stop %s: %v", service.Unit, err))
		}
	}
	return warnings
}

func RestoreServices(metadataRoot, destinationRoot string, manifest Manifest) []string {
	var warnings []string
	metadata := filepath.Join(metadataRoot, ".vbakup")

	// Redis reads the RDB at service startup, so place it before restarting.
	if contains(manifest.DatabaseDumps, "redis.rdb") {
		target, err := destinationPath(destinationRoot, "/var/lib/redis/dump.rdb")
		if err != nil {
			warnings = append(warnings, "redis restore: "+err.Error())
		} else if err = copyFile(filepath.Join(metadata, "redis.rdb"), target); err != nil {
			warnings = append(warnings, "redis restore: "+err.Error())
		}
	}

	services := restorableServices(manifest)
	if containsManager(services, "systemd") {
		if output, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
			warnings = append(warnings, fmt.Sprintf("systemd daemon-reload: %v: %s", err, strings.TrimSpace(string(output))))
		}
	}
	for _, service := range services {
		if manifest.Version >= 2 && service.WasEnabled {
			if err := serviceCommand(service.Manager, "enable", service.Unit); err != nil {
				warnings = append(warnings, fmt.Sprintf("enable %s: %v", service.Unit, err))
			}
		}
		if manifest.Version >= 2 && !service.WasActive {
			continue
		}
		if err := serviceCommand(service.Manager, "restart", service.Unit); err != nil {
			warnings = append(warnings, fmt.Sprintf("restart %s: %v", service.Unit, err))
		}
	}
	for _, project := range manifest.Discovery.ComposeProjects {
		directory, err := destinationPath(destinationRoot, project)
		if err != nil {
			warnings = append(warnings, "compose "+project+": "+err.Error())
			continue
		}
		composeMetadata := filepath.Join(metadata, "compose", composeMetadataName(project))
		if err = restoreComposeMetadata(composeMetadata, directory); err != nil {
			warnings = append(warnings, "compose "+project+": "+err.Error())
			continue
		}
		command := exec.Command("docker", "compose", "up", "-d")
		command.Dir = directory
		if output, runErr := command.CombinedOutput(); runErr != nil {
			warnings = append(warnings, fmt.Sprintf("compose %s: %v: %s", project, runErr, strings.TrimSpace(string(output))))
		}
	}
	for _, dump := range manifest.DatabaseDumps {
		file := filepath.Join(metadata, dump)
		switch {
		case dump == "mysql.sql":
			warnings = appendCommandWarning(warnings, "mysql restore", file, "mysql")
		case dump == "postgresql.sql":
			warnings = appendCommandWarning(warnings, "postgres restore", file, "psql", "-U", "postgres")
		case strings.HasPrefix(dump, "mysql-") && strings.HasSuffix(dump, ".sql"):
			container := strings.TrimSuffix(strings.TrimPrefix(dump, "mysql-"), ".sql")
			warnings = appendDockerCommandWarning(warnings, "docker mysql restore", file, container, "sh", "-c", "mysql -uroot")
		case strings.HasPrefix(dump, "postgresql-") && strings.HasSuffix(dump, ".sql"):
			container := strings.TrimSuffix(strings.TrimPrefix(dump, "postgresql-"), ".sql")
			warnings = appendDockerCommandWarning(warnings, "docker postgres restore", file, container, "sh", "-c", "psql -U postgres")
		case strings.HasPrefix(dump, "redis-") && strings.HasSuffix(dump, ".rdb"):
			container := strings.TrimSuffix(strings.TrimPrefix(dump, "redis-"), ".rdb")
			warnings = appendDockerRedisRestore(warnings, "docker redis restore", file, container)
		}
	}
	return warnings
}

func appendDockerRedisRestore(warnings []string, label, input, container string) []string {
	if err := exec.Command("docker", "stop", "-t", "30", container).Run(); err != nil {
		return append(warnings, fmt.Sprintf("%s stop: %v", label, err))
	}
	if output, err := exec.Command("docker", "cp", input, container+":/data/dump.rdb").CombinedOutput(); err != nil {
		_ = exec.Command("docker", "start", container).Run()
		return append(warnings, fmt.Sprintf("%s copy: %v: %s", label, err, strings.TrimSpace(string(output))))
	}
	if err := exec.Command("docker", "start", container).Run(); err != nil {
		return append(warnings, fmt.Sprintf("%s start: %v", label, err))
	}
	return warnings
}

func restoreComposeMetadata(source, destination string) error {
	entries, err := os.ReadDir(source)
	if err != nil {
		return fmt.Errorf("metadata missing: %w", err)
	}
	if err = os.MkdirAll(destination, 0750); err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if err = copyFile(filepath.Join(source, entry.Name()), filepath.Join(destination, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func restorableServices(manifest Manifest) []Service {
	legacyUnits := map[string]string{"1Panel": "1panel", "Docker": "docker", "Xray": "xray", "sing-box": "sing-box", "Komari Agent": "komari-agent", "Cloudreve": "cloudreve", "MySQL": "mysql", "PostgreSQL": "postgresql", "Redis": "redis-server", "Nginx": "nginx"}
	seen := map[string]bool{}
	var result []Service
	for _, service := range manifest.Discovery.Services {
		if service.Unit == "" {
			service.Unit = legacyUnits[service.Name]
		}
		if service.Manager == "" && service.Unit != "" {
			service.Manager = "systemd"
		}
		key := service.Manager + "\x00" + service.Unit
		if service.Unit != "" && !seen[key] {
			seen[key] = true
			result = append(result, service)
		}
	}
	return result
}

func containsManager(services []Service, manager string) bool {
	for _, service := range services {
		if service.Manager == manager {
			return true
		}
	}
	return false
}

func serviceCommand(manager, action, unit string) error {
	var command *exec.Cmd
	switch manager {
	case "openrc":
		if action == "enable" {
			command = exec.Command("rc-update", "add", unit, "default")
		} else {
			command = exec.Command("rc-service", unit, action)
		}
	default:
		command = exec.Command("systemctl", action, unit)
	}
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
func destinationPath(root, absolute string) (string, error) {
	if !filepath.IsAbs(absolute) {
		return "", fmt.Errorf("path is not absolute")
	}
	cleanRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	target, err := filepath.Abs(filepath.Join(cleanRoot, strings.TrimPrefix(filepath.Clean(absolute), string(os.PathSeparator))))
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(cleanRoot, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes destination")
	}
	return target, nil
}
func appendCommandWarning(warnings []string, label, input, name string, args ...string) []string {
	file, err := os.Open(input)
	if err != nil {
		return append(warnings, label+": "+err.Error())
	}
	defer file.Close()
	command := exec.Command(name, args...)
	command.Stdin = file
	if output, runErr := command.CombinedOutput(); runErr != nil {
		return append(warnings, fmt.Sprintf("%s: %v: %s", label, runErr, strings.TrimSpace(string(output))))
	}
	return warnings
}

func appendDockerCommandWarning(warnings []string, label, input, container string, args ...string) []string {
	file, err := os.Open(input)
	if err != nil {
		return append(warnings, label+": "+err.Error())
	}
	defer file.Close()
	command := exec.Command("docker", append([]string{"exec", "-i", container}, args...)...)
	command.Stdin = file
	if output, runErr := command.CombinedOutput(); runErr != nil {
		return append(warnings, fmt.Sprintf("%s: %v: %s", label, runErr, strings.TrimSpace(string(output))))
	}
	return warnings
}
func copyFile(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	if err = os.MkdirAll(filepath.Dir(target), 0750); err != nil {
		return err
	}
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	_, err = out.ReadFrom(in)
	closeErr := out.Close()
	if err != nil {
		return err
	}
	return closeErr
}
