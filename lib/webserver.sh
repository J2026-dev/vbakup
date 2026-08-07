#!/usr/bin/env bash
set -euo pipefail
webserver_discover() { local out="$1"; jq -n --arg nginx "$(command -v nginx >/dev/null 2>&1 && echo true || echo false)" --arg apache "$(command -v apache2 >/dev/null 2>&1 && echo true || echo false)" --arg caddy "$(command -v caddy >/dev/null 2>&1 && echo true || echo false)" '{nginx:($nginx=="true"),apache:($apache=="true"),caddy:($caddy=="true"),roots:["/var/www","/opt/www"],ssl:["/etc/letsencrypt","/etc/ssl"]}' >"$out"; }
webserver_backup() { local dir="$1/web"; mkdir -p "$dir"; tar -cf "$dir/webroots.tar" /var/www /opt/www /home/*/public_html 2>/dev/null || true; tar -cf "$dir/web-config.tar" /etc/nginx /etc/apache2 /etc/caddy 2>/dev/null || true; tar -cf "$dir/ssl.tar" /etc/letsencrypt /etc/ssl 2>/dev/null || true; }
webserver_restore() { local dir="$1/web"; for f in webroots web-config ssl; do [[ -f "$dir/$f.tar" ]] && tar -xf "$dir/$f.tar" -C / 2>/dev/null || true; done; }
