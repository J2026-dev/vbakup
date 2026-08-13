//go:build !linux

package main

import "os"

func applyRestoreOwner(string, os.FileInfo, bool) error { return nil }
