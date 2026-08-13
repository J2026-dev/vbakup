//go:build linux

package agent

import "os"

func applyArchiveOwner(path string, uid, gid int, symlink bool) error {
	if symlink {
		return os.Lchown(path, uid, gid)
	}
	return os.Chown(path, uid, gid)
}
