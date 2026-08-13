package main

type resourceStats struct {
	UptimeSeconds int64   `json:"uptime_seconds"`
	Load1         float64 `json:"load_1"`
	MemoryTotal   uint64  `json:"memory_total"`
	MemoryUsed    uint64  `json:"memory_used"`
	DiskTotal     uint64  `json:"disk_total"`
	DiskUsed      uint64  `json:"disk_used"`
}
