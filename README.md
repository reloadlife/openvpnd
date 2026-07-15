# openvpnd

**openvpnd** is a Linux daemon that manages OpenVPN **server-role and client-role** instances with a REST API, SQLite desired-state store, process reconciler, multi-binary registry, and Prometheus metrics.

**openvpnctl** is the control panel: **full-screen TUI** (default) plus CLI subcommands.

How it works: [docs/HOW_IT_WORKS.md](docs/HOW_IT_WORKS.md) · design: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)

[![License: AGPL-3.0](https://img.shields.io/badge/License-AGPL%203.0-blue.svg)](LICENSE)
[![Release](https://img.shields.io/github/v/release/reloadlife/openvpnd)](https://github.com/reloadlife/openvpnd/releases)

> **Status:** See [docs/STATUS.md](docs/STATUS.md) for capability matrix and gaps. Not every OpenVPN option is modeled 1:1.

## Why

- **Desired state in SQLite**, applied live by starting/stopping `openvpn` processes
- **Server + client roles** as first-class instances
- **Multiple OpenVPN binaries** (named registry; pin per instance)
- **No external DHCP** for TUN servers — built-in pool + CCD static IPs + `push` DNS/routes
- **Management interface** sampling for connected clients and traffic counters
- **One-click profiles** — presigned URLs + `openvpn://import-profile/` for OpenVPN Connect

## Architecture

```
openvpnctl  ── HTTP / Unix ──►  openvpnd
                                   │
          ┌────────────────────────┼────────────────────────┐
          │                        │                        │
     state.db               timeseries.db            Prometheus
  (instances, clients,         (traffic samples)
   binaries, tokens)
          │
     reconciler ──► conf render + openvpn process + management iface
```

Details: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)

## Documentation

| Doc | Contents |
|-----|----------|
| [docs/HOW_IT_WORKS.md](docs/HOW_IT_WORKS.md) | How openvpnd works (operator overview) |
| [docs/INSTALL.md](docs/INSTALL.md) | Install from source / releases |
| [docs/CONFIGURATION.md](docs/CONFIGURATION.md) | Daemon + ctl config reference |
| [docs/API.md](docs/API.md) | HTTP API routes |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Design and domain model |
| [docs/STATUS.md](docs/STATUS.md) | What works / roadmap / gaps |
| [docs/OBSERVABILITY.md](docs/OBSERVABILITY.md) | Prometheus + SNMP |
| [docs/PKI.md](docs/PKI.md) | CA / mTLS certs |
| [docs/EXTENSIONS.md](docs/EXTENSIONS.md) | Plugins, custom builds, feature sets |
| [docs/OPENVPN_FEATURES.md](docs/OPENVPN_FEATURES.md) | OpenVPN option matrix (A–E) + test ownership |
| [docs/TESTING.md](docs/TESTING.md) | Suites, coverage targets, `make test-*` |
| [docs/PRODUCTION.md](docs/PRODUCTION.md) | Production checklist, systemd, backups, soak |
| [SECURITY.md](SECURITY.md) | Reporting + hardening |
| [CONTRIBUTING.md](CONTRIBUTING.md) | Dev workflow |

Example configs: [`configs/`](configs/) · production template: [`configs/openvpnd.production.example.yaml`](configs/openvpnd.production.example.yaml) · systemd: [`deploy/openvpnd.service`](deploy/openvpnd.service)

## DHCP?

**No separate DHCP server for normal TUN mode.** OpenVPN’s `--server` / `ifconfig-pool` assigns tunnel addresses; CCD (`ifconfig-push`) pins static IPs; DNS/domain are OpenVPN `push` options (DHCP-*like*, not the DHCP protocol). TAP bridge + LAN DHCP is out of scope for now.

## Quick start (dev)

```bash
make deps
make build

cat > /tmp/ovpnd.yaml <<'EOF'
listen:
  http: "127.0.0.1:51980"
  metrics: "127.0.0.1:9092"
db:
  path: "/tmp/openvpnd-state.db"
auth:
  token: "dev-token"
public_base_url: "http://127.0.0.1:51980"
openvpn:
  conf_dir: "/tmp/openvpnd/conf"
  runtime_dir: "/tmp/openvpnd/run"
  pki_dir: "/tmp/openvpnd/pki"
  default_binary: "default"
  binaries:
    default: "/usr/sbin/openvpn"
  use_mock_backend: true
log:
  level: info
  format: text
EOF

./bin/openvpnd run --config /tmp/ovpnd.yaml &

export OPENVPNCTL_URL=http://127.0.0.1:51980
export OPENVPNCTL_TOKEN=dev-token

./bin/openvpnctl binary list
./bin/openvpnctl instance create ovpn0 --role server --network 10.8.0.0/24 --binary default
./bin/openvpnctl client create ovpn0 alice --name Alice --link
# → cert issued, free IP, optional install URL/QR in TUI
./bin/openvpnctl instance list
./bin/openvpnctl   # full-screen TUI
```

### TUI keys (summary)

| Key | Action |
|-----|--------|
| `1`–`5` / Tab | Instances · Clients · Binaries · Stats · Events |
| `n` | Create (context-aware form) |
| `enter` | Detail view |
| `u` / `d` | Instance up / down |
| `s` / `S` | Suspend / resume client |
| `p` / `L` | Mint profile link + QR |
| `c` | Show `.ovpn` |
| `R` | Force reconcile |
| `q` | Quit |

## Multi-binary

```bash
./bin/openvpnctl binary add v26 /opt/openvpn-2.6/sbin/openvpn
./bin/openvpnctl instance create home --role client --remote vpn.example.com --binary v26
```

Each instance stores `binary_name` (or `binary_path` override). Changing the binary restarts the process on the next reconcile.

## PKI / mTLS

```bash
openvpnctl pki ca-create default --cn "Example CA"
openvpnctl instance issue-cert ovpn0 --ca default --tls-crypt
openvpnctl client issue-cert ovpn0 alice --ca default
```

Details: [docs/PKI.md](docs/PKI.md)

## One-click client install (presigned URL)

Users import profiles into **OpenVPN Connect** without the admin API token:

```bash
# Requires: instance public_endpoint + pki_ca_path; client cert/key paths
./bin/openvpnctl client link ovpn0 alice --ttl 24h --max-uses 1
# download: https://vpn.example.com/p/<token>
# import:   openvpn://import-profile/https://vpn.example.com/p/<token>
```

- `download_url` — browser / file download of inline `.ovpn`
- `import_url` — deep link for OpenVPN Connect
- Links expire (`ttl`) and can be single-use (`max_uses: 1`)
- Public route `GET /p/{token}` does **not** use the admin bearer token

Set `public_base_url: "https://vpn.example.com"` in production (HTTPS reverse proxy).

## Install (host)

```bash
make build
sudo ./scripts/install-local.sh
sudoedit /etc/openvpnd/config.yaml   # set auth.token
sudo systemctl enable --now openvpnd
```

See [docs/INSTALL.md](docs/INSTALL.md).

## Configuration

- Daemon (dev): [`configs/openvpnd.example.yaml`](configs/openvpnd.example.yaml)
- Daemon (production): [`configs/openvpnd.production.example.yaml`](configs/openvpnd.production.example.yaml)
- CLI: [`configs/openvpnctl.example.yaml`](configs/openvpnctl.example.yaml)
- systemd env: [`deploy/openvpnd.env.example`](deploy/openvpnd.env.example)
- Details: [docs/CONFIGURATION.md](docs/CONFIGURATION.md) · ops: [docs/PRODUCTION.md](docs/PRODUCTION.md)

Env prefixes: `OPENVPND_*`, `OPENVPNCTL_*`.

Default API: `127.0.0.1:51980` · metrics `9092` (avoids clash with wireguardd).

## Observability

- **Prometheus:** `listen.metrics` (default `127.0.0.1:9092`) and `/metrics` on the API
- **SNMP:** optional SNMPv2c agent (`snmp.enabled`, default port `1162`)

Metric names, scrape config, and OID map: [docs/OBSERVABILITY.md](docs/OBSERVABILITY.md) · MIB: [`deploy/mibs/OPENVPND-MIB.txt`](deploy/mibs/OPENVPND-MIB.txt)

## API

Bearer token on `/v1/*`. Full list: [docs/API.md](docs/API.md).

| Area | Paths |
|------|--------|
| Core | `/healthz`, `/readyz`, `/v1/version`, `/v1/config`, `/v1/reconcile`, `/v1/events`, `/v1/stats` |
| Binaries | `/v1/binaries` |
| Instances | `/v1/instances`, `.../up`, `.../down`, `.../restart`, `.../export` |
| Clients | `/v1/instances/{name}/clients`, suspend/resume, profile-link, client-config |
| Public | `/p/{token}` — profile download |

## Security

- **Never** leave `auth.token: change-me` on a real host
- Bind API to localhost or Unix socket; terminate TLS externally if remote
- Keep `allow_hooks: false` unless every API client is trusted
- Treat profile tokens as secrets (they deliver private key material)
- See [SECURITY.md](SECURITY.md)

## Development

```bash
make test
make build
# optional stability soak (mock backend):
make test-soak
```

Contributions: [CONTRIBUTING.md](CONTRIBUTING.md)

## Donations

If this project is useful to you, donations are welcome:

| Network | Address |
|---------|---------|
| **Bitcoin** (BTC) | `bc1qy08pk2teys968hphh98rv8y9azeraf2c8vsdm8` |
| **EVM** (ETH, BNB, USDT, and other EVM chains) | `0x8B6CE1EA8F17f6941F13A621b92Af345a75D8c41` |
| **TRON** (TRX) | `TGXJToyAsUtw1388jR5aW9ZohjSCDtmKbg` |

## License

[GNU Affero General Public License v3.0](LICENSE) (AGPL-3.0).

If you run a modified version of `openvpnd` as a network service, you must offer the corresponding source to users who interact with it over the network (AGPL §13).

## Related

Sibling projects with a similar control-plane shape: [wireguardd](https://github.com/reloadlife/wireguardd), [netpolicyd](https://github.com/reloadlife/netpolicyd), [dnsd](https://github.com/reloadlife/dnsd).
