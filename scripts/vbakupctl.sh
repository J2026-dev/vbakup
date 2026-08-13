#!/bin/sh
set -eu

INSTALL_DIR=${VBAKUP_INSTALL_DIR:-/opt/vbakup}

usage() {
  echo "Usage: vbakupctl status | logs | update | uninstall [--purge] | delete <node|task|backup|repository> <id>"
}

[ -r "$INSTALL_DIR/.env" ] || { echo "vBakup is not installed in $INSTALL_DIR" >&2; exit 1; }
. "$INSTALL_DIR/.env"
cd "$INSTALL_DIR"

case "${1:-}" in
  status)
    docker compose ps
    echo
    docker system df
    echo
    docker compose exec -T controller sh -c '
      printf "controller_data="; du -sh /data 2>/dev/null | cut -f1
      printf "nodes="; grep -o '"'"'"token_hash"'"'"' /data/state.json 2>/dev/null | wc -l
      printf "plans="; grep -o '"'"'"schedule"'"'"' /data/state.json 2>/dev/null | wc -l
      printf "snapshots="; grep -o '"'"'"remote_path"'"'"' /data/state.json 2>/dev/null | wc -l
      printf "repositories="; grep -o '"'"'"password_encrypted"'"'"' /data/state.json 2>/dev/null | wc -l
    '
    ;;
  logs)
    docker compose logs --tail=200 -f
    ;;
  update)
    REPO_RAW=${VBAKUP_REPO_RAW:-https://raw.githubusercontent.com/J2026-dev/vbakup/main}
    TMP_DIR=$(mktemp -d)
    trap 'rm -rf "$TMP_DIR"' EXIT INT TERM
    curl -fsSL "$REPO_RAW/compose.yaml" -o "$TMP_DIR/compose.yaml"
    curl -fsSL "$REPO_RAW/scripts/vbakupctl.sh" -o "$TMP_DIR/vbakupctl"
    install -m 0644 "$TMP_DIR/compose.yaml" "$INSTALL_DIR/compose.yaml"
    install -m 0755 "$TMP_DIR/vbakupctl" /usr/local/bin/vbakupctl
    docker compose pull
    docker compose up -d
    ATTEMPTS=0
    until docker compose exec -T controller wget -q -O /dev/null http://127.0.0.1:8080/healthz 2>/dev/null; do
      ATTEMPTS=$((ATTEMPTS + 1))
      [ "$ATTEMPTS" -lt 30 ] || { docker compose logs --tail=80 >&2; exit 1; }
      sleep 2
    done
    echo "vBakup controller updated successfully."
    ;;
  uninstall)
    printf "This stops and removes the vBakup controller. Type UNINSTALL to continue: " >&2
    read -r CONFIRMATION
    [ "$CONFIRMATION" = "UNINSTALL" ] || { echo "Cancelled."; exit 1; }
    docker compose down
    if [ "${2:-}" = "--purge" ]; then
      printf "This permanently deletes controller settings, credentials, and indexes. Type PURGE to continue: " >&2
      read -r PURGE_CONFIRMATION
      [ "$PURGE_CONFIRMATION" = "PURGE" ] || { echo "Purge cancelled; data remains in $INSTALL_DIR."; exit 1; }
      rm -rf "$INSTALL_DIR/data"
      rm -f "$INSTALL_DIR/.env" "$INSTALL_DIR/compose.yaml"
      echo "Controller and local controller data removed. WebDAV backup objects were not deleted."
    else
      echo "Controller removed. Data is preserved in $INSTALL_DIR; use --purge to delete it."
    fi
    rm -f /usr/local/bin/vbakupctl
    ;;
  delete)
    KIND=${2:-}
    OBJECT_ID=${3:-}
    case "$KIND" in node) PATH_NAME=nodes;; task) PATH_NAME=tasks;; backup) PATH_NAME=backups;; repository) PATH_NAME=repositories;; *) usage; exit 1;; esac
    [ -n "$OBJECT_ID" ] || { usage; exit 1; }
    printf "Administrator password: " >&2
    stty -echo 2>/dev/null || true
    read -r ADMIN_PASSWORD
    stty echo 2>/dev/null || true
    printf '\n' >&2
    curl -fsS -u "${VBAKUP_ADMIN_USER:-admin}:$ADMIN_PASSWORD" -X DELETE "${VBAKUP_PUBLIC_URL}/api/${PATH_NAME}/${OBJECT_ID}"
    echo
    ;;
  *) usage; exit 1 ;;
esac
