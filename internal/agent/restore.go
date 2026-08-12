package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func RestoreServices(root string, manifest Manifest) []string {
	var warnings []string
	base := filepath.Join(root, ".vbakup")
	for _, dump := range manifest.DatabaseDumps {
		file := filepath.Join(base, dump)
		switch dump {
		case "mysql.sql":
			warnings = appendCommandWarning(warnings, "mysql restore", file, "mysql")
		case "postgresql.sql":
			warnings = appendCommandWarning(warnings, "postgres restore", file, "psql", "-U", "postgres")
		case "redis.rdb":
			target := filepath.Join(root, "var/lib/redis/dump.rdb")
			if err := copyFile(file, target); err != nil {
				warnings = append(warnings, "redis restore: "+err.Error())
			}
		}
	}
	for _, service := range manifest.Discovery.Services {
		var unit string
		switch service.Name {
		case "Docker":
			unit = "docker"
		case "Xray":
			unit = "xray"
		case "Komari Agent":
			unit = "komari-agent"
		case "Cloudreve":
			unit = "cloudreve"
		case "MySQL":
			unit = "mysql"
		case "PostgreSQL":
			unit = "postgresql"
		case "Redis":
			unit = "redis-server"
		case "Nginx":
			unit = "nginx"
		}
		if unit != "" {
			if err := exec.Command("systemctl", "try-restart", unit).Run(); err != nil {
				warnings = append(warnings, fmt.Sprintf("restart %s: %v", unit, err))
			}
		}
	}
	return warnings
}
func appendCommandWarning(warnings []string, label, input string, name string, args ...string) []string {
	file, err := os.Open(input)
	if err != nil {
		return append(warnings, label+": "+err.Error())
	}
	defer file.Close()
	cmd := exec.Command(name, args...)
	cmd.Stdin = file
	if err = cmd.Run(); err != nil {
		return append(warnings, label+": "+err.Error())
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
