#!/usr/bin/env bash
# Install openvpnd from GitHub Releases (when published) or fall back to source build instructions.
set -euo pipefail

REPO="${REPO:-reloadlife/openvpnd}"
VERSION="${VERSION:-latest}"
BIN_DIR="${BIN_DIR:-/usr/local/bin}"
CTL_ONLY=0

usage() {
  cat <<'EOF'
Usage: install.sh [--version vX.Y.Z] [--ctl-only] [--repo owner/name]

Downloads release assets openvpnd / openvpnctl for linux/amd64 or arm64.
If no release assets exist yet, prints how to build from source.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) VERSION="$2"; shift 2 ;;
    --ctl-only) CTL_ONLY=1; shift ;;
    --repo) REPO="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown arg: $1" >&2; usage; exit 1 ;;
  esac
done

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) GOARCH=amd64 ;;
  aarch64|arm64) GOARCH=arm64 ;;
  *) echo "unsupported arch: $ARCH" >&2; exit 1 ;;
esac
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
if [[ "$OS" != "linux" && "$CTL_ONLY" -eq 0 ]]; then
  echo "daemon install requires Linux; use --ctl-only on other OS" >&2
  exit 1
fi

api="https://api.github.com/repos/${REPO}/releases"
if [[ "$VERSION" == "latest" ]]; then
  url="${api}/latest"
else
  url="${api}/tags/${VERSION}"
fi

echo "resolving ${REPO} @ ${VERSION} …"
if ! json="$(curl -fsSL "$url" 2>/dev/null)"; then
  cat <<EOF
No GitHub release found for ${REPO} (${VERSION}).

Build from source:

  git clone https://github.com/${REPO}.git
  cd openvpnd
  make build
  sudo ./scripts/install-local.sh
EOF
  exit 1
fi

tag="$(printf '%s' "$json" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1)"
echo "release: $tag"

download() {
  local name="$1" dest="$2"
  local asset="${name}-${OS}-${GOARCH}"
  local dl
  dl="$(printf '%s' "$json" | sed -n "s/.*\"browser_download_url\": *\"\\([^\"]*${asset}[^\"]*\\)\".*/\\1/p" | head -1)"
  if [[ -z "$dl" ]]; then
    # try bare name
    dl="$(printf '%s' "$json" | sed -n "s/.*\"browser_download_url\": *\"\\([^\"]*${name}[^\"]*\\)\".*/\\1/p" | head -1)"
  fi
  if [[ -z "$dl" ]]; then
    echo "asset not found for $name ($OS/$GOARCH)" >&2
    return 1
  fi
  curl -fsSL "$dl" -o "$dest"
  chmod 0755 "$dest"
}

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

if [[ "$CTL_ONLY" -eq 0 ]]; then
  download openvpnd "$tmp/openvpnd"
  install -d "$BIN_DIR"
  install -m 0755 "$tmp/openvpnd" "$BIN_DIR/openvpnd"
fi
download openvpnctl "$tmp/openvpnctl"
install -d "$BIN_DIR"
install -m 0755 "$tmp/openvpnctl" "$BIN_DIR/openvpnctl"

echo "installed to $BIN_DIR"
if [[ "$CTL_ONLY" -eq 0 ]]; then
  echo "Next: configure /etc/openvpnd/config.yaml and systemctl enable --now openvpnd"
  echo "See docs/INSTALL.md"
fi
