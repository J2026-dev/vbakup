//go:build !linux

package main

func collectResources() resourceStats { return resourceStats{} }
