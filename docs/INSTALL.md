# Installation

## Requirements

- **Linux** for `openvpnd` (process supervision, TUN, typically root / `CAP_NET_ADMIN`)
- **Linux or macOS** for `openvpnctl` (API client only)
- OpenVPN 2.5+ binary on the host (or registered custom paths)
- Recommended: `iproute2`, OpenSSL tools for PKI generation outside the daemon

## Quick install (from this repo / releases)

When a release is published:

```bash
curl -fsSL https://raw.githubusercontent.com/reloadlife/openvpnd/main/scripts/install.sh | sudo bash
```

Until then, install from source (below).

## From source

```bash
git clone https://github.com/reloadlife/openvpnd.git
cd openvpnd
make build
sudo ./scripts/install-local.sh
```

## Self-update from GitHub Releases

Both binaries can refresh themselves from published release assets:

```bash
# Check only (no download)
openvpnd update --check
openvpnctl update --check

# Install latest release for this OS/arch into the current executable path
sudo openvpnd update
# or pin a tag / fork:
sudo openvpnd update --version v0.2.0
sudo openvpnd update --repo reloadlife/openvpnd

# openvpnctl uses the same flags
sudo openvpnctl update --check
sudo openvpnctl update
```

Behavior:

1. Resolves `https://api.github.com/repos/{repo}/releases/latest` (or `/tags/vX.Y.Z`)
2. Prefers `openvpnd_VERSION_linux_{amd64|arm64}.tar.gz`; falls back to bare `openvpnd` / `openvpnctl` assets
3. Verifies `SHA256SUMS` when the release includes it
4. Atomically replaces the running binary path (`os.Executable`), and updates the sibling (`openvpnd`/`openvpnctl`) in the same directory when that file already exists and is in the archive
5. Prints `systemctl restart openvpnd` instructions (does **not** restart automatically)

```bash
sudo systemctl restart openvpnd
openvpnd version
openvpnctl version
```

Requires write access to the install directory (typically `sudo` for `/usr/local/bin`).

## After install

```bash
# 1. Set a strong API token + binary paths
sudoedit /etc/openvpnd/config.yaml

# 2. Matching ctl config
sudo tee /etc/openvpnctl/config.yaml >/dev/null <<EOF
server:
  url: "http://127.0.0.1:51980"
  token: "SAME_TOKEN_AS_DAEMON"
EOF
sudo chmod 600 /etc/openvpnctl/config.yaml

# 3. Start
sudo systemctl enable --now openvpnd

# 4. Verify
openvpnctl version
openvpnctl binary list
openvpnctl instance list
```

## Profile links in production

```yaml
# /etc/openvpnd/config.yaml
public_base_url: "https://vpn.example.com"  # reverse-proxy to openvpnd /p/*
profile_links:
  default_ttl: 24h
  default_max_uses: 1
```

Terminate TLS on the reverse proxy; only expose what you need (`/p/` for users, keep `/v1` private).

## Directory layout

| Path | Purpose |
|------|---------|
| `/etc/openvpnd/config.yaml` | Daemon config (start from [`configs/openvpnd.production.example.yaml`](../configs/openvpnd.production.example.yaml) on real hosts) |
| `/etc/openvpnd/openvpnd.env` | Optional systemd `EnvironmentFile` (see [`deploy/openvpnd.env.example`](../deploy/openvpnd.env.example)) |
| `/etc/openvpnd/instances/` | Generated confs + CCD |
| `/var/lib/openvpnd/` | SQLite + PKI |
| `/run/openvpnd/` | PID, management sockets, status, logs |
| `/var/backups/openvpnd/` | Backup archives (`openvpnd backup`); allowed by systemd `ReadWritePaths` |

### Backups

```bash
sudo install -d -m 0750 /var/backups/openvpnd
sudo openvpnd backup --config /etc/openvpnd/config.yaml \
  --out /var/backups/openvpnd/openvpnd-$(date +%F).tar.gz
# restore (daemon stopped): openvpnd restore --in … --config /etc/openvpnd/config.yaml
```

Full procedure: [PRODUCTION.md](PRODUCTION.md#backup-and-restore). Ops checklist: [PRODUCTION.md](PRODUCTION.md).

## Uninstall

```bash
sudo systemctl disable --now openvpnd
sudo rm -f /usr/local/bin/openvpnd /usr/local/bin/openvpnctl
sudo rm -f /etc/systemd/system/openvpnd.service
sudo systemctl daemon-reload
# optional: sudo rm -rf /var/lib/openvpnd /etc/openvpnd
```
