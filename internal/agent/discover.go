package agent

import (
	"os"
	"os/exec"
	"sort"
	"strings"
)

type Service struct {
	Name  string   `json:"name"`
	Kind  string   `json:"kind"`
	Paths []string `json:"paths,omitempty"`
}

type Discovery struct {
	Services         []Service `json:"services"`
	Paths            []string  `json:"paths"`
	DockerContainers []string  `json:"docker_containers,omitempty"`
}

func Discover() Discovery {
	definitions := []Service{
		{Name: "1Panel", Kind: "panel", Paths: []string{"/opt/1panel", "/etc/1panel"}},
		{Name: "Docker", Kind: "container", Paths: []string{"/etc/docker", "/var/lib/docker/volumes"}},
		{Name: "Xray", Kind: "proxy", Paths: []string{"/usr/local/etc/xray", "/etc/xray"}},
		{Name: "Komari Agent", Kind: "monitoring", Paths: []string{"/opt/komari", "/etc/komari"}},
		{Name: "Cloudreve", Kind: "storage", Paths: []string{"/opt/cloudreve", "/etc/cloudreve"}},
		{Name: "MySQL", Kind: "database", Paths: []string{"/etc/mysql", "/var/lib/mysql"}},
		{Name: "PostgreSQL", Kind: "database", Paths: []string{"/etc/postgresql", "/var/lib/postgresql"}},
		{Name: "Redis", Kind: "database", Paths: []string{"/etc/redis", "/var/lib/redis"}},
		{Name: "Nginx", Kind: "web", Paths: []string{"/etc/nginx"}},
	}
	pathSet := map[string]bool{}
	var services []Service
	for _, service := range definitions {
		var found []string
		for _, p := range service.Paths {
			if _, err := os.Stat(p); err == nil {
				found = append(found, p)
				pathSet[p] = true
			}
		}
		if len(found) > 0 {
			service.Paths = found
			services = append(services, service)
		}
	}
	for _, p := range []string{"/etc", "/home", "/root", "/var/www", "/opt"} {
		if _, err := os.Stat(p); err == nil {
			pathSet[p] = true
		}
	}
	containers := commandLines("docker", "ps", "-a", "--format", "{{.Names}}")
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
	return Discovery{Services: services, Paths: paths, DockerContainers: containers}
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
func ServiceNames(discovery Discovery) []string {
	out := make([]string, 0, len(discovery.Services))
	for _, s := range discovery.Services {
		out = append(out, s.Name)
	}
	return out
}
