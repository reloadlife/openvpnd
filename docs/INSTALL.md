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
| `/etc/openvpnd/config.yaml` | Daemon config |
| `/etc/openvpnd/instances/` | Generated confs + CCD |
| `/var/lib/openvpnd/` | SQLite + PKI |
| `/run/openvpnd/` | PID, management sockets, status, logs |

## Uninstall

```bash
sudo systemctl disable --now openvpnd
sudo rm -f /usr/local/bin/openvpnd /usr/local/bin/openvpnctl
sudo rm -f /etc/systemd/system/openvpnd.service
sudo systemctl daemon-reload
# optional: sudo rm -rf /var/lib/openvpnd /etc/openvpnd
```
