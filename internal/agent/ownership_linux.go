//go:build linux

package agent

import "os"

func applyArchiveOwner(path string, uid, gid int, symlink bool) error {
	if !canRestoreOwnership(os.Geteuid()) {
		return nil
	}
	if symlink {
		return os.Lchown(path, uid, gid)
	}
	return os.Chown(path, uid, gid)
}

func canRestoreOwnership(euid int) bool {
	return euid == 0
}
