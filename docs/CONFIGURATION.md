# Configuration

## openvpnd

Example: [`configs/openvpnd.example.yaml`](../configs/openvpnd.example.yaml)

Default path after install: `/etc/openvpnd/config.yaml`

### Essential keys

| Key | Description |
|-----|-------------|
| `auth.token` | Bearer token for `/v1/*` (**change the default**) |
| `listen.http` | API listen address (prefer `127.0.0.1:51980`) |
| `listen.unix` | Optional Unix socket path |
| `listen.metrics` | Prometheus listen (empty to disable dedicated listener) |
| `snmp.enabled` | Enable SNMPv2c agent |
| `snmp.listen` | UDP bind (default `127.0.0.1:1162`) |
| `snmp.community` | SNMPv2c community (**change default**) |
| `snmp.enterprise_oid` | Private enterprise base OID |
| `db.path` | State SQLite path |
| `openvpn.conf_dir` | Generated instance confs + CCD |
| `openvpn.runtime_dir` | PID, management sockets, status, logs |
| `openvpn.pki_dir` | PKI material root |
| `openvpn.default_binary` | Registry name used when instance omits `binary_name` |
| `openvpn.binaries` | Map of name → absolute openvpn path |
| `openvpn.use_mock_backend` | Dev/CI without real openvpn |
| `openvpn.allow_hooks` | Allow PreUp/PostUp shell hooks |
| `openvpn.bandwidth_enforcement` | `off` (default) \| `tc` \| `shaper` \| `log` — per-client bandwidth shaping |
| `public_base_url` | Public origin for profile links (`https://vpn.example.com`) |
| `profile_links.default_ttl` | Default link lifetime (`24h`) |
| `profile_links.default_max_uses` | Default downloads per link (`1`; `0` = unlimited until expiry) |

See also [OBSERVABILITY.md](OBSERVABILITY.md) for Prometheus metric names and SNMP OID layout.

### Environment overrides

Prefix `OPENVPND_`, nested keys with `_`:

```bash
export OPENVPND_AUTH_TOKEN=...
export OPENVPND_DB_PATH=/var/lib/openvpnd/state.db
export OPENVPND_LISTEN_HTTP=127.0.0.1:51980
```

## openvpnctl

Example: [`configs/openvpnctl.example.yaml`](../configs/openvpnctl.example.yaml)

Search order:

1. `--config path`
2. `$HOME/.config/openvpnctl/config.yaml`
3. `/etc/openvpnctl/config.yaml`

```bash
export OPENVPNCTL_URL=http://127.0.0.1:51980
export OPENVPNCTL_TOKEN=...
```

## Multi-binary

```yaml
openvpn:
  default_binary: default
  binaries:
    default: /usr/sbin/openvpn
    v24: /opt/openvpn-2.4/sbin/openvpn
    v26: /opt/openvpn-2.6/sbin/openvpn
```

Per instance: `binary_name: v24` or `binary_path: /custom/openvpn`.

## Bandwidth enforcement

Client fields `bandwidth_rx_bps` / `bandwidth_tx_bps` (bits/sec) and `traffic_limit_bytes` are stored in SQLite. Enforcement mode:

| Mode | Behavior |
|------|----------|
| `off` | Default. No shaping. `traffic_limit_bytes` still suspends clients when exceeded. |
| `tc` | Linux `tc` HTB (egress = client download) + ingress police (upload). Requires instance `device` (e.g. `tun0`) and `iproute2`. Soft no-op if `tc` is missing. |
| `shaper` | Confgen emits global OpenVPN `shaper N` (bytes/sec) from max client limit. Outgoing only; not per-client. |
| `log` | Plans the same `tc` rules as `tc` mode and logs them (dry-run). |

```yaml
openvpn:
  bandwidth_enforcement: tc   # off | tc | shaper | log
```

Reconciler applies/removes rules after `EnsureInstance`. Removed clients or cleared limits drop host rules. Over-quota clients are suspended (`disable` in CCD) and killed via management if connected.

## IP assignment (no DHCP)

Server TUN mode uses OpenVPN pool from `server_network` (e.g. `10.8.0.0/24`). Client static IPs go to CCD via `ifconfig-push`. DNS/routes are `push` options, not a DHCP daemon.
