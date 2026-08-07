#!/usr/bin/env bash
set -euo pipefail
discovery_run() { local out="$1"; mkdir -p "$out"; docker_discover "$out/docker.json"; systemd_discover "$out/systemd.json"; webserver_discover "$out/webserver.json"; network_discover "$out/network.json"; database_discover "$out/database.json"; }
