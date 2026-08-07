#!/usr/bin/env bash
set -euo pipefail
systemd_discover() { local out="$1"; systemctl list-unit-files --type=service --type=timer --no-pager 2>/dev/null | awk 'NR>1 {print $1" "$2}' | jq -Rn '[inputs|split(" ")|select(length>=2)|{unit:.[0],state:.[1]}]' >"$out" || echo '[]' >"$out"; }
systemd_backup() { local dir="$1/systemd"; mkdir -p "$dir"; systemd_discover "$dir/units.json"; tar -cf "$dir/systemd-files.tar" /etc/systemd/system 2>/dev/null || true; }
systemd_restore() { local dir="$1/systemd"; [[ -f "$dir/systemd-files.tar" ]] && tar -xf "$dir/systemd-files.tar" -C / 2>/dev/null || true; systemctl daemon-reload 2>/dev/null || true; }
