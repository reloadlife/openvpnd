# Project status

openvpnd is under active development. This document tracks what works today vs planned work.

## Shipped

| Release | Notes |
|---------|--------|
| [v0.1.0](https://github.com/reloadlife/openvpnd/releases/tag/v0.1.0) | Foundation: instances, PKI/CRL, profiles, TUI, import, iroutes |
| [v0.2.0](https://github.com/reloadlife/openvpnd/releases/tag/v0.2.0) | Roadmap wave: adopt, mgmt API, bandwidth, bridge/TLS/auth knobs, self-update, TUI |
| [v1.0.0](https://github.com/reloadlife/openvpnd/releases/tag/v1.0.0) | Production posture: backup/restore, multi-token RBAC, hardened readyz, audit, adopt take-over, systemd/prod config |

Production guide: [PRODUCTION.md](PRODUCTION.md).

## Implemented

- [x] Daemon + CLI (`openvpnd`, `openvpnctl`) + TUI
- [x] SQLite desired state + reconciler + host/mock backends
- [x] Multi-binary registry; plugins / feature_sets / env / extra_directives
- [x] Server + client instances; clients CN pool / CCD / iroute / ACL push overrides
- [x] Managed PKI: CA, issue, revoke, CRL, renew, tls-crypt
- [x] One-shot client create + profile links + QR
- [x] Conf import (paths + **inline PEM materialize**)
- [x] **Discover + adopt** running/file OpenVPN confs
- [x] **Management API** (status, kill, signal, hold, log, …)
- [x] **Bandwidth enforcement** (tc / shaper / log) + traffic-limit suspend
- [x] Bridge mode (`server-bridge`), TLS control-channel fields, auth-user-pass-verify
- [x] Dual-stack helpers (`server-ipv6`, `ifconfig-ipv6`)
- [x] Self-update from GitHub Releases (`openvpnd update` / `openvpnctl update`)
- [x] Feature presets: stuffing, auth script, tls_modern
- [x] Prometheus + SNMPv2c
- [x] Integration tests (`make test-integration`)

## Not 1:1 with OpenVPN

OpenVPN has hundreds of options. Coverage is intentional tiers A–E — see [OPENVPN_FEATURES.md](OPENVPN_FEATURES.md).

Remaining long-tail options still use **`extra_directives`**. Full LDAP product, multi-tenant VLAN NFV, and Windows are out of scope.

## Remaining / follow-ups (lower urgency)

| Topic | Status |
|-------|--------|
| Live PID take-over without restart | Done (`take_over` + verified SIGTERM/SIGKILL; `openvpn.adopt_takeover_enabled`) |
| Full LDAP/IdP product | Partial (script verify + preset; no bundled IdP) |
| Auto-update without operator | Out of scope (explicit `update` only) |
| Every manpage flag as typed field | Escape hatch |

## Comparison to wireguardd

Same control-plane shape (SQLite SoT, reconciler, REST, ctl); dataplane is OpenVPN processes.
