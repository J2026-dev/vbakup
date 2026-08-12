#!/bin/sh
set -eu

REPO_RAW="https://raw.githubusercontent.com/J2026-dev/vbakup/main"
INSTALL_DIR="/opt/vbakup"
DOMAIN=""

usage() {
  echo "Usage: sudo sh install-controller.sh --domain backup.example.com"
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --domain) DOMAIN="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown argument: $1" >&2; usage; exit 1 ;;
  esac
done

[ "$(id -u)" = "0" ] || { echo "Run as root (use sudo)." >&2; exit 1; }
[ -n "$DOMAIN" ] || { usage; exit 1; }
case "$DOMAIN" in http://*|https://*|*/*|*:*) echo "Use a hostname only, without scheme, port, or path." >&2; exit 1;; esac
if command -v getent >/dev/null 2>&1 && ! getent hosts "$DOMAIN" >/dev/null 2>&1; then
  echo "Domain $DOMAIN does not resolve yet. Create its DNS record first." >&2
  exit 1
fi

install_docker() {
  if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then return; fi
  if command -v apk >/dev/null 2>&1; then
    apk add --no-cache ca-certificates curl docker docker-cli-compose
    rc-update add docker default 2>/dev/null || true
    rc-service docker start
    docker compose version >/dev/null 2>&1 || { echo "Docker Compose plugin is required." >&2; exit 1; }
    return
  elif command -v apt-get >/dev/null 2>&1; then
    # Docker's convenience script currently requests optional packages that are
    # unavailable on older Ubuntu releases. Install only the required packages.
    [ -r /etc/os-release ] || { echo "Cannot identify this Linux distribution." >&2; exit 1; }
    . /etc/os-release
    case "${ID:-}" in
      ubuntu|debian) ;;
      *) echo "Unsupported apt distribution: ${ID:-unknown}. Install Docker Compose first." >&2; exit 1 ;;
    esac
    CODENAME=${VERSION_CODENAME:-}
    if [ -z "$CODENAME" ] && command -v lsb_release >/dev/null 2>&1; then
      CODENAME=$(lsb_release -cs)
    fi
    [ -n "$CODENAME" ] || { echo "Cannot determine the distribution codename." >&2; exit 1; }

    apt-get update
    DEBIAN_FRONTEND=noninteractive apt-get install -y ca-certificates curl
    install -m 0755 -d /etc/apt/keyrings
    curl -fsSL "https://download.docker.com/linux/${ID}/gpg" -o /etc/apt/keyrings/docker.asc
    chmod a+r /etc/apt/keyrings/docker.asc
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/${ID} ${CODENAME} stable" > /etc/apt/sources.list.d/docker.list
    apt-get update
    DEBIAN_FRONTEND=noninteractive apt-get install -y \
      docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
  elif command -v dnf >/dev/null 2>&1; then
    dnf install -y ca-certificates curl
    curl -fsSL https://get.docker.com | sh
  elif command -v yum >/dev/null 2>&1; then
    yum install -y ca-certificates curl
    curl -fsSL https://get.docker.com | sh
  else
    echo "Unsupported Linux package manager. Install Docker Compose first." >&2
    exit 1
  fi
  systemctl enable --now docker 2>/dev/null || service docker start
  docker compose version >/dev/null 2>&1 || { echo "Docker Compose plugin is required." >&2; exit 1; }
}

random_secret() {
  if command -v openssl >/dev/null 2>&1; then openssl rand -hex 24
  else head -c 48 /dev/urandom | od -An -tx1 | tr -d ' \n'; fi
}

install_docker
install -d -m 0700 "$INSTALL_DIR/data"
curl -fsSL "$REPO_RAW/compose.yaml" -o "$INSTALL_DIR/compose.yaml"
curl -fsSL "$REPO_RAW/scripts/vbakupctl.sh" -o /usr/local/bin/vbakupctl
chmod 0755 /usr/local/bin/vbakupctl

if [ -f "$INSTALL_DIR/.env" ]; then
  ADMIN_PASSWORD=$(sed -n 's/^VBAKUP_ADMIN_PASSWORD=//p' "$INSTALL_DIR/.env")
  BOOTSTRAP_SECRET=$(sed -n 's/^VBAKUP_BOOTSTRAP_SECRET=//p' "$INSTALL_DIR/.env")
  [ -n "$ADMIN_PASSWORD" ] || ADMIN_PASSWORD=$(random_secret)
  [ -n "$BOOTSTRAP_SECRET" ] || BOOTSTRAP_SECRET=$(random_secret)
else
  ADMIN_PASSWORD=$(random_secret)
  BOOTSTRAP_SECRET=$(random_secret)
fi
cat > "$INSTALL_DIR/.env.tmp" <<EOF
VBAKUP_DOMAIN=$DOMAIN
VBAKUP_PUBLIC_URL=https://$DOMAIN
VBAKUP_ADMIN_USER=admin
VBAKUP_ADMIN_PASSWORD=$ADMIN_PASSWORD
VBAKUP_BOOTSTRAP_SECRET=$BOOTSTRAP_SECRET
VBAKUP_RELEASE_BASE=https://github.com/J2026-dev/vbakup/releases/latest/download
EOF
mv "$INSTALL_DIR/.env.tmp" "$INSTALL_DIR/.env"
chmod 0600 "$INSTALL_DIR/.env"

cd "$INSTALL_DIR"
docker compose pull
docker compose up -d

ATTEMPTS=0
until docker compose exec -T controller wget -q -O /dev/null http://127.0.0.1:8080/healthz 2>/dev/null; do
  ATTEMPTS=$((ATTEMPTS + 1))
  if [ "$ATTEMPTS" -ge 30 ]; then
    echo "Controller did not become ready. Recent logs:" >&2
    docker compose logs --tail=80 >&2
    exit 1
  fi
  sleep 2
done

cat <<EOF

vBakup controller is starting.

Panel:    https://$DOMAIN
Username: admin
Password: $ADMIN_PASSWORD

Bootstrap secret: $BOOTSTRAP_SECRET
Credentials are saved in $INSTALL_DIR/.env (mode 0600).

DNS must point $DOMAIN to this VPS. Caddy will request the HTTPS certificate automatically.
Check status: cd $INSTALL_DIR && docker compose ps
View logs:   cd $INSTALL_DIR && docker compose logs -f
Quick status: vbakupctl status
EOF
