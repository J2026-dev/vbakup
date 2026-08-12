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
	for _, unit := range serviceUnits(manifest) {
		if err := exec.Command("systemctl", "stop", unit).Run(); err != nil {
			warnings = append(warnings, fmt.Sprintf("stop %s: %v", unit, err))
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

	for _, unit := range serviceUnits(manifest) {
		if err := exec.Command("systemctl", "restart", unit).Run(); err != nil {
			warnings = append(warnings, fmt.Sprintf("restart %s: %v", unit, err))
		}
	}
	for _, dump := range manifest.DatabaseDumps {
		file := filepath.Join(metadata, dump)
		switch dump {
		case "mysql.sql":
			warnings = appendCommandWarning(warnings, "mysql restore", file, "mysql")
		case "postgresql.sql":
			warnings = appendCommandWarning(warnings, "postgres restore", file, "psql", "-U", "postgres")
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

func serviceUnits(manifest Manifest) []string {
	units := map[string]string{"Docker": "docker", "Xray": "xray", "Komari Agent": "komari-agent", "Cloudreve": "cloudreve", "MySQL": "mysql", "PostgreSQL": "postgresql", "Redis": "redis-server", "Nginx": "nginx"}
	var result []string
	for _, service := range manifest.Discovery.Services {
		if unit := units[service.Name]; unit != "" {
			result = append(result, unit)
		}
	}
	return result
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
