#!/bin/sh
set -eu

CONTROLLER=""
SECRET=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --controller) CONTROLLER="$2"; shift 2 ;;
    --secret) SECRET="$2"; shift 2 ;;
    *) echo "Unknown argument: $1" >&2; exit 1 ;;
  esac
done
[ "$(id -u)" = "0" ] || { echo "Run as root (use sudo)." >&2; exit 1; }
[ -n "$CONTROLLER" ] && [ -n "$SECRET" ] || { echo "--controller and --secret are required." >&2; exit 1; }
case "$(uname -m)" in x86_64) ARCH=amd64;; aarch64|arm64) ARCH=arm64;; *) echo "Unsupported architecture." >&2; exit 1;; esac

if ! command -v curl >/dev/null 2>&1; then
  if command -v apt-get >/dev/null 2>&1; then apt-get update && apt-get install -y curl ca-certificates
  elif command -v dnf >/dev/null 2>&1; then dnf install -y curl ca-certificates
  elif command -v yum >/dev/null 2>&1; then yum install -y curl ca-certificates
  elif command -v apk >/dev/null 2>&1; then apk add --no-cache curl ca-certificates
  else echo "Install curl and CA certificates first." >&2; exit 1; fi
fi

install -d -m 0700 /etc/vbakup /var/lib/vbakup
TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT INT TERM
curl -fL "__RELEASE_BASE__/vbakup-agent-linux-$ARCH" -o "$TMP_DIR/vbakup-agent"
curl -fL "__RELEASE_BASE__/vbakup-agent-linux-$ARCH.sha256" -o "$TMP_DIR/vbakup-agent.sha256"
(cd "$TMP_DIR" && sed "s#vbakup-agent-linux-$ARCH#vbakup-agent#" vbakup-agent.sha256 | sha256sum -c -)
install -m 0755 "$TMP_DIR/vbakup-agent" /usr/local/bin/vbakup-agent
if [ -s /etc/vbakup/agent.json ]; then
  NODE_ID=$(sed -n 's/.*"node_id":"\([^"]*\)".*/\1/p' /etc/vbakup/agent.json)
  [ -n "$NODE_ID" ] || { echo "Existing agent configuration is invalid; move /etc/vbakup/agent.json aside and retry." >&2; exit 1; }
  echo "Existing node configuration found; updating the agent without registering a duplicate node."
else
  HOST=$(hostname | tr -cd 'A-Za-z0-9._-')
  RESPONSE=$(curl -fsS -X POST "$CONTROLLER/api/agent/register" -H 'Content-Type: application/json' --data "{\"name\":\"$HOST\",\"secret\":\"$SECRET\",\"os\":\"linux\",\"architecture\":\"$ARCH\",\"agent_version\":\"0.1.0\"}")
  NODE_ID=$(printf '%s' "$RESPONSE" | sed -n 's/.*"node_id":"\([^"]*\)".*/\1/p')
  TOKEN=$(printf '%s' "$RESPONSE" | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')
  [ -n "$NODE_ID" ] && [ -n "$TOKEN" ] || { echo "Registration failed: $RESPONSE" >&2; exit 1; }
  printf '{"controller":"%s","node_id":"%s","token":"%s"}\n' "$CONTROLLER" "$NODE_ID" "$TOKEN" > /etc/vbakup/agent.json
fi
chmod 0600 /etc/vbakup/agent.json
if command -v systemctl >/dev/null 2>&1; then
  cat > /etc/systemd/system/vbakup-agent.service <<'UNIT'
[Unit]
Description=vBakup Agent
After=network-online.target
Wants=network-online.target
[Service]
ExecStart=/usr/local/bin/vbakup-agent
Restart=always
RestartSec=10
NoNewPrivileges=true
PrivateTmp=true
[Install]
WantedBy=multi-user.target
UNIT
  systemctl daemon-reload
  systemctl enable --now vbakup-agent
elif command -v rc-service >/dev/null 2>&1; then
  cat > /etc/init.d/vbakup-agent <<'OPENRC'
#!/sbin/openrc-run
name="vBakup Agent"
command="/usr/local/bin/vbakup-agent"
command_background="yes"
pidfile="/run/vbakup-agent.pid"
output_log="/var/log/vbakup-agent.log"
error_log="/var/log/vbakup-agent.log"
depend() { need net; after firewall; }
OPENRC
  chmod 0755 /etc/init.d/vbakup-agent
  rc-update add vbakup-agent default
  rc-service vbakup-agent restart
else
  echo "Neither systemd nor OpenRC is available." >&2
  exit 1
fi
echo "vBakup agent installed: $NODE_ID"
