//go:build linux

package main

import "testing"

func TestCanRestoreOwnershipOnlyAsRoot(t *testing.T) {
	if !canRestoreOwnership(0) {
		t.Fatal("root agent must restore archived UID/GID")
	}
	for _, euid := range []int{1, 1000, 65534} {
		if canRestoreOwnership(euid) {
			t.Fatalf("non-root euid %d must not attempt chown", euid)
		}
	}
}
