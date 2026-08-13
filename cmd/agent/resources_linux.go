//go:build linux

package main

import (
	"os"
	"strconv"
	"strings"
	"syscall"
)

func collectResources() resourceStats {
	var stats resourceStats
	if data, err := os.ReadFile("/proc/uptime"); err == nil {
		value, _ := strconv.ParseFloat(strings.Fields(string(data))[0], 64)
		stats.UptimeSeconds = int64(value)
	}
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		stats.Load1, _ = strconv.ParseFloat(strings.Fields(string(data))[0], 64)
	}
	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		values := map[string]uint64{}
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				value, _ := strconv.ParseUint(fields[1], 10, 64)
				values[strings.TrimSuffix(fields[0], ":")] = value * 1024
			}
		}
		stats.MemoryTotal = values["MemTotal"]
		if available := values["MemAvailable"]; stats.MemoryTotal >= available {
			stats.MemoryUsed = stats.MemoryTotal - available
		}
	}
	var fs syscall.Statfs_t
	if err := syscall.Statfs("/", &fs); err == nil {
		stats.DiskTotal = fs.Blocks * uint64(fs.Bsize)
		stats.DiskUsed = (fs.Blocks - fs.Bavail) * uint64(fs.Bsize)
	}
	return stats
}
