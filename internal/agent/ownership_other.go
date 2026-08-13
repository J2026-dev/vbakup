//go:build !linux

package agent

func applyArchiveOwner(string, int, int, bool) error { return nil }
