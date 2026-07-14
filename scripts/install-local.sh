#!/usr/bin/env bash
# Install openvpnd/openvpnctl from a local build into /usr/local.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN_DIR="${BIN_DIR:-/usr/local/bin}"
ETC_DIR="${ETC_DIR:-/etc/openvpnd}"
LIB_DIR="${LIB_DIR:-/var/lib/openvpnd}"
RUN_DIR="${RUN_DIR:-/run/openvpnd}"

if [[ ! -x "$ROOT/bin/openvpnd" || ! -x "$ROOT/bin/openvpnctl" ]]; then
  echo "binaries missing; run: make build" >&2
  exit 1
fi

install -d "$BIN_DIR" "$ETC_DIR" "$LIB_DIR" "$RUN_DIR" /etc/openvpnctl
install -m 0755 "$ROOT/bin/openvpnd" "$BIN_DIR/openvpnd"
install -m 0755 "$ROOT/bin/openvpnctl" "$BIN_DIR/openvpnctl"

if [[ ! -f "$ETC_DIR/config.yaml" ]]; then
  install -m 0600 "$ROOT/configs/openvpnd.example.yaml" "$ETC_DIR/config.yaml"
  # Point state paths at standard locations
  sed -i \
    -e 's|/tmp/openvpnd-state.db|'"$LIB_DIR"'/state.db|g' \
    -e 's|/var/lib/openvpnd/state.db|'"$LIB_DIR"'/state.db|g' \
    "$ETC_DIR/config.yaml" 2>/dev/null || true
  echo "wrote $ETC_DIR/config.yaml — change auth.token before production"
fi

if [[ ! -f /etc/openvpnctl/config.yaml ]]; then
  install -m 0600 "$ROOT/configs/openvpnctl.example.yaml" /etc/openvpnctl/config.yaml
fi

if [[ -d /etc/systemd/system ]]; then
  install -m 0644 "$ROOT/deploy/openvpnd.service" /etc/systemd/system/openvpnd.service
  systemctl daemon-reload
  echo "systemd unit installed: systemctl enable --now openvpnd"
fi

echo "installed: $BIN_DIR/openvpnd $BIN_DIR/openvpnctl"
