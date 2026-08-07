#!/usr/bin/env bash
set -euo pipefail
check_environment() { local miss=0 dep; for dep in bash restic rclone jq curl tar zstd; do command -v "$dep" >/dev/null 2>&1 || { log_warn "Missing dependency: $dep"; miss=1; }; done; return "$miss"; }
check_supported_os() { [[ -f /etc/os-release ]] || return 1; . /etc/os-release; case "${ID:-}:${VERSION_ID:-}" in ubuntu:20.04|ubuntu:22.04|ubuntu:24.04|debian:11|debian:12) return 0;; *) log_warn "Untested OS: ${PRETTY_NAME:-unknown}"; return 0;; esac; }
