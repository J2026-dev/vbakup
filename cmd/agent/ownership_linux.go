//go:build linux

package main

import (
	"os"
	"syscall"
)

func applyRestoreOwner(path string, info os.FileInfo, symlink bool) error {
	if !canRestoreOwnership(os.Geteuid()) {
		return nil
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	if symlink {
		return os.Lchown(path, int(stat.Uid), int(stat.Gid))
	}
	return os.Chown(path, int(stat.Uid), int(stat.Gid))
}

func canRestoreOwnership(euid int) bool {
	return euid == 0
}
