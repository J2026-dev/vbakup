#!/bin/sh
set -eu

INSTALL_DIR=${VBAKUP_INSTALL_DIR:-/opt/vbakup}

usage() {
  echo "Usage: vbakupctl status | logs | update | delete <node|task|backup|repository> <id>"
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
    docker compose pull
    docker compose up -d
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
