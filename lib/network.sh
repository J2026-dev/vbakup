#!/usr/bin/env bash
set -euo pipefail
network_discover() { local out="$1"; jq -n --arg hostname "$(hostname)" --rawfile hosts /etc/hosts --rawfile resolv /etc/resolv.conf '{hostname:$hostname,hosts:$hosts,resolv_conf:$resolv}' >"$out"; }
network_backup() { local dir="$1/network"; mkdir -p "$dir"; cp -a /etc/hosts /etc/resolv.conf "$dir/" 2>/dev/null || true; iptables-save >"$dir/iptables.rules" 2>/dev/null || true; nft list ruleset >"$dir/nftables.rules" 2>/dev/null || true; sysctl -a >"$dir/sysctl.txt" 2>/dev/null || true; cp -a /etc/ssh/sshd_config "$dir/" 2>/dev/null || true; }
network_restore() { local dir="$1/network"; [[ -d "$dir" ]] || return 0; [[ -f "$dir/hosts" ]] && cp -a "$dir/hosts" /etc/hosts; [[ -f "$dir/iptables.rules" ]] && iptables-restore <"$dir/iptables.rules" 2>/dev/null || true; }
