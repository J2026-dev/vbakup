package agent

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type Service struct {
	Name       string   `json:"name"`
	Kind       string   `json:"kind"`
	Paths      []string `json:"paths,omitempty"`
	Manager    string   `json:"manager,omitempty"`
	Unit       string   `json:"unit,omitempty"`
	WasActive  bool     `json:"was_active,omitempty"`
	WasEnabled bool     `json:"was_enabled,omitempty"`
	Runtime    string   `json:"runtime,omitempty"`
}

type ShortcutCommand struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Kind       string `json:"kind"`
	Target     string `json:"target,omitempty"`
	Restorable bool   `json:"restorable"`
}

type Discovery struct {
	Services         []Service         `json:"services"`
	Paths            []string          `json:"paths"`
	DockerContainers []string          `json:"docker_containers,omitempty"`
	ComposeProjects  []string          `json:"compose_projects,omitempty"`
	ShortcutCommands []ShortcutCommand `json:"shortcut_commands,omitempty"`
}

type serviceDefinition struct {
	Service
	Units    []string
	Binaries []string
}

func Discover() Discovery {
	definitions := serviceDefinitions()
	pathSet := map[string]bool{}
	var services []Service
	for _, definition := range definitions {
		service := definition.Service
		var found []string
		for _, p := range append(append([]string{}, service.Paths...), definition.Binaries...) {
			if _, err := os.Stat(p); err == nil {
				found = append(found, p)
				pathSet[p] = true
			}
		}
		manager, unit, active, enabled, unitPath := discoverServiceState(definition.Units)
		if unitPath != "" {
			found = append(found, unitPath)
			pathSet[unitPath] = true
		}
		if len(found) > 0 || unit != "" {
			service.Paths = found
			service.Manager = manager
			service.Unit = unit
			service.WasActive = active
			service.WasEnabled = enabled
			services = append(services, service)
		}
	}
	// These roots cover application data and logs without archiving the OS
	// binaries or virtual filesystems under /bin, /usr and /proc-like mounts.
	for _, p := range []string{"/etc", "/home", "/root", "/opt", "/srv", "/var/www", "/var/log", "/var/lib", "/usr/local/etc", "/usr/local/bin", "/usr/local/sbin"} {
		if _, err := os.Stat(p); err == nil {
			pathSet[p] = true
		}
	}
	containers := commandLines("docker", "ps", "-a", "--format", "{{.Names}}")
	composeProjects := uniqueAbsoluteLines(commandLines("docker", "ps", "-a", "--format", "{{.Label \"com.docker.compose.project.working_dir\"}}"))
	if len(containers) > 0 && !containsService(services, "Docker") {
		services = append(services, Service{Name: "Docker", Kind: "container", Paths: []string{"/var/lib/docker/volumes"}})
		pathSet["/var/lib/docker/volumes"] = true
	}
	paths := make([]string, 0, len(pathSet))
	for p := range pathSet {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	sort.Slice(services, func(i, j int) bool { return services[i].Name < services[j].Name })
	return Discovery{Services: services, Paths: paths, DockerContainers: containers, ComposeProjects: composeProjects, ShortcutCommands: discoverShortcutCommands()}
}

func serviceDefinitions() []serviceDefinition {
	return []serviceDefinition{
		{Service: Service{Name: "1Panel", Kind: "panel", Paths: []string{"/opt/1panel", "/etc/1panel", "/var/lib/1panel"}}, Units: []string{"1panel"}, Binaries: []string{"/usr/local/bin/1pctl"}},
		{Service: Service{Name: "Docker", Kind: "container", Runtime: "docker", Paths: []string{"/etc/docker", "/var/lib/docker/volumes"}}, Units: []string{"docker"}},
		{Service: Service{Name: "vless-all-in-one", Kind: "proxy-manager", Paths: []string{"/etc/vless-reality"}}, Binaries: []string{"/usr/local/bin/vless-server.sh"}},
		{Service: Service{Name: "Xray", Kind: "proxy", Paths: []string{"/usr/local/etc/xray", "/etc/xray"}}, Units: []string{"xray"}, Binaries: []string{"/usr/local/bin/xray", "/usr/bin/xray"}},
		{Service: Service{Name: "sing-box", Kind: "proxy", Paths: []string{"/etc/sing-box", "/usr/local/etc/sing-box"}}, Units: []string{"sing-box"}, Binaries: []string{"/usr/local/bin/sing-box", "/usr/bin/sing-box"}},
		{Service: Service{Name: "Komari Agent", Kind: "monitoring", Paths: []string{"/opt/komari", "/etc/komari"}}, Units: []string{"komari-agent", "komari"}, Binaries: []string{"/usr/local/bin/komari-agent", "/usr/bin/komari-agent"}},
		{Service: Service{Name: "Cloudreve", Kind: "storage", Paths: []string{"/opt/cloudreve", "/etc/cloudreve"}}, Units: []string{"cloudreve"}, Binaries: []string{"/usr/local/bin/cloudreve", "/usr/bin/cloudreve"}},
		{Service: Service{Name: "MySQL", Kind: "database", Paths: []string{"/etc/mysql", "/var/lib/mysql"}}, Units: []string{"mysql", "mariadb"}},
		{Service: Service{Name: "PostgreSQL", Kind: "database", Paths: []string{"/etc/postgresql", "/var/lib/postgresql"}}, Units: []string{"postgresql"}},
		{Service: Service{Name: "Redis", Kind: "database", Paths: []string{"/etc/redis", "/var/lib/redis"}}, Units: []string{"redis-server", "redis"}},
		{Service: Service{Name: "Nginx", Kind: "web", Paths: []string{"/etc/nginx"}}, Units: []string{"nginx"}},
	}
}

func discoverServiceState(candidates []string) (manager, unit string, active, enabled bool, unitPath string) {
	if _, err := exec.LookPath("systemctl"); err == nil {
		for _, candidate := range candidates {
			output, showErr := exec.Command("systemctl", "show", candidate, "--property=LoadState,ActiveState,UnitFileState,FragmentPath").Output()
			if showErr != nil {
				continue
			}
			values := parseSystemdProperties(string(output))
			if values["LoadState"] == "not-found" || values["LoadState"] == "" {
				continue
			}
			return "systemd", candidate, values["ActiveState"] == "active", values["UnitFileState"] == "enabled" || values["UnitFileState"] == "enabled-runtime", existingUnitPath(values["FragmentPath"])
		}
	}
	if _, err := exec.LookPath("rc-service"); err == nil {
		for _, candidate := range candidates {
			if _, statErr := os.Stat(filepath.Join("/etc/init.d", candidate)); statErr != nil {
				continue
			}
			active = exec.Command("rc-service", candidate, "status").Run() == nil
			enabled = openRCEnabled(candidate)
			return "openrc", candidate, active, enabled, filepath.Join("/etc/init.d", candidate)
		}
	}
	return "", "", false, false, ""
}

func parseSystemdProperties(output string) map[string]string {
	values := map[string]string{}
	for _, line := range strings.Split(output, "\n") {
		if key, value, ok := strings.Cut(line, "="); ok {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return values
}

func existingUnitPath(path string) string {
	path = strings.TrimSpace(path)
	clean := filepath.ToSlash(filepath.Clean(path))
	if filepath.IsAbs(path) && (strings.HasPrefix(clean, "/etc/systemd/system/") || strings.HasPrefix(clean, "/etc/init.d/")) {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func openRCEnabled(unit string) bool {
	output, err := exec.Command("rc-update", "show").Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(output), "\n") {
		if fields := strings.Fields(line); len(fields) > 0 && fields[0] == unit {
			return true
		}
	}
	return false
}

func discoverShortcutCommands() []ShortcutCommand {
	seen := map[string]bool{}
	var result []ShortcutCommand
	add := func(command ShortcutCommand) {
		key := command.Kind + "\x00" + command.Name + "\x00" + command.Path
		if command.Name != "" && !seen[key] {
			seen[key] = true
			result = append(result, command)
		}
	}
	for _, directory := range shortcutDirectories() {
		entries, err := os.ReadDir(directory)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			path := filepath.Join(directory, entry.Name())
			info, infoErr := os.Lstat(path)
			if infoErr != nil || info.IsDir() || entry.Name() == "vbakup-agent" || entry.Name() == "vbakup-agentctl" {
				continue
			}
			command := ShortcutCommand{Name: entry.Name(), Path: path, Kind: "executable", Restorable: isRestorableShortcut(path)}
			if info.Mode()&os.ModeSymlink != 0 {
				command.Kind = "symlink"
				command.Target, _ = os.Readlink(path)
			} else if info.Mode()&0111 == 0 {
				continue
			}
			add(command)
		}
	}
	for _, profile := range shellProfileFiles() {
		file, err := os.Open(profile)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if !strings.HasPrefix(line, "alias ") {
				continue
			}
			name, _, ok := strings.Cut(strings.TrimSpace(strings.TrimPrefix(line, "alias ")), "=")
			name = strings.TrimSpace(name)
			if ok && name != "" && !strings.ContainsAny(name, " \t/\\") {
				add(ShortcutCommand{Name: name, Path: profile, Kind: "alias", Restorable: true})
			}
		}
		_ = file.Close()
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Name == result[j].Name {
			return result[i].Path < result[j].Path
		}
		return result[i].Name < result[j].Name
	})
	return result
}

func shortcutDirectories() []string {
	directories := []string{"/usr/local/bin", "/usr/local/sbin", "/root/.local/bin"}
	homes, _ := filepath.Glob("/home/*/.local/bin")
	return append(directories, homes...)
}

func shellProfileFiles() []string {
	profiles := []string{"/etc/profile", "/root/.bashrc", "/root/.profile", "/root/.zshrc"}
	for _, pattern := range []string{"/etc/profile.d/*.sh", "/home/*/.bashrc", "/home/*/.profile", "/home/*/.zshrc"} {
		matches, _ := filepath.Glob(pattern)
		profiles = append(profiles, matches...)
	}
	return profiles
}

func isRestorableShortcut(path string) bool {
	clean := filepath.ToSlash(filepath.Clean(path))
	for _, prefix := range []string{"/usr/local/bin", "/usr/local/sbin", "/root", "/home"} {
		if clean == prefix || strings.HasPrefix(clean, prefix+"/") {
			return true
		}
	}
	return false
}

func containsService(services []Service, name string) bool {
	for _, s := range services {
		if s.Name == name {
			return true
		}
	}
	return false
}
func commandLines(name string, args ...string) []string {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		return nil
	}
	var lines []string
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
func uniqueAbsoluteLines(lines []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, line := range lines {
		line = filepath.Clean(line)
		if filepath.IsAbs(line) && !seen[line] {
			seen[line] = true
			result = append(result, line)
		}
	}
	sort.Strings(result)
	return result
}
func ServiceNames(discovery Discovery) []string {
	out := make([]string, 0, len(discovery.Services))
	for _, s := range discovery.Services {
		out = append(out, s.Name)
	}
	return out
}

func ShortcutNames(discovery Discovery) []string {
	out := make([]string, 0, len(discovery.ShortcutCommands))
	for _, shortcut := range discovery.ShortcutCommands {
		label := shortcut.Name
		if shortcut.Path != "" {
			label += " (" + shortcut.Path + ")"
		}
		out = append(out, label)
	}
	return out
}
