#!/bin/sh
set -eu

CONFIG=${VBAKUP_CONFIG:-/etc/vbakup/agent.json}

usage() {
  echo "Usage: vbakup-agentctl status | logs | update | auto-update on|off | uninstall"
}

require_root() {
  [ "$(id -u)" = "0" ] || { echo "Run as root (use sudo)." >&2; exit 1; }
}

json_string() {
  sed -n "s/.*\"$1\"[[:space:]]*:[[:space:]]*\"\([^\"]*\)\".*/\1/p" "$CONFIG" | head -n 1
}

set_auto_update() {
  enabled=$1
  temporary=$(mktemp)
  if grep -q '"auto_update"' "$CONFIG"; then
    sed "s/\"auto_update\"[[:space:]]*:[[:space:]]*\(true\|false\)/\"auto_update\": $enabled/" "$CONFIG" > "$temporary"
  elif [ "$(wc -l < "$CONFIG")" -le 1 ]; then
    sed "s/}[[:space:]]*$/,\"auto_update\":$enabled}/" "$CONFIG" > "$temporary"
  else
    awk -v enabled="$enabled" '{ lines[NR]=$0 } END { for (i=1;i<NR;i++) print (i==NR-1 ? lines[i] "," : lines[i]); print "  \"auto_update\": " enabled; print lines[NR] }' "$CONFIG" > "$temporary"
  fi
  chmod 0600 "$temporary"
  mv "$temporary" "$CONFIG"
}

[ -r "$CONFIG" ] || { echo "vBakup agent is not installed." >&2; exit 1; }

case "${1:-}" in
  status)
    if command -v systemctl >/dev/null 2>&1; then
      systemctl status vbakup-agent --no-pager
      echo
      systemctl status vbakup-agent-update.timer --no-pager 2>/dev/null || true
    else
      rc-service vbakup-agent status
    fi
    echo "node_id=$(json_string node_id)"
    echo "controller=$(json_string controller)"
    ;;
  logs)
    if command -v journalctl >/dev/null 2>&1; then
      journalctl -u vbakup-agent -n 200 -f
    else
      tail -n 200 -f /var/log/vbakup-agent.log
    fi
    ;;
  update)
    require_root
    controller=$(json_string controller)
    [ -n "$controller" ] || { echo "Controller URL is missing from $CONFIG" >&2; exit 1; }
    curl -fsSL "$controller/install.sh" | sh -s -- --controller "$controller"
    ;;
  auto-update)
    require_root
    case "${2:-}" in
      on) enabled=true;;
      off) enabled=false;;
      *) usage; exit 1;;
    esac
    set_auto_update "$enabled"
    if command -v systemctl >/dev/null 2>&1; then
      if [ "$enabled" = true ]; then
        systemctl enable --now vbakup-agent-update.timer
      else
        systemctl disable --now vbakup-agent-update.timer
      fi
      systemctl restart vbakup-agent
    fi
    echo "Automatic agent updates: $enabled"
    ;;
  uninstall)
    require_root
    printf "This removes the agent, identity, and local vBakup data. Type UNINSTALL to continue: " >&2
    read -r confirmation
    [ "$confirmation" = "UNINSTALL" ] || { echo "Cancelled."; exit 1; }
    if command -v systemctl >/dev/null 2>&1; then
      systemctl disable --now vbakup-agent vbakup-agent-update.timer >/dev/null 2>&1 || true
      rm -f /etc/systemd/system/vbakup-agent.service /etc/systemd/system/vbakup-agent-update.service /etc/systemd/system/vbakup-agent-update.timer
      systemctl daemon-reload
    elif command -v rc-service >/dev/null 2>&1; then
      rc-service vbakup-agent stop >/dev/null 2>&1 || true
      rc-update del vbakup-agent default >/dev/null 2>&1 || true
      rm -f /etc/init.d/vbakup-agent
    fi
    rm -f /usr/local/bin/vbakup-agent
    rm -rf /etc/vbakup /var/lib/vbakup
    rm -f /usr/local/bin/vbakup-agentctl
    echo "Agent removed. Delete its offline node record in the controller panel when appropriate."
    ;;
  *) usage; exit 1;;
esac
