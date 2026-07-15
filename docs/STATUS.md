# Project status

openvpnd is under active development. This document tracks what works today vs planned work.

## Shipped

| Release | Notes |
|---------|--------|
| [v0.1.0](https://github.com/reloadlife/openvpnd/releases/tag/v0.1.0) | Foundation: instances, PKI/CRL, profiles, TUI, import, iroutes |
| [v0.2.0](https://github.com/reloadlife/openvpnd/releases/tag/v0.2.0) | Roadmap wave: adopt, mgmt API, bandwidth, bridge/TLS/auth knobs, self-update, TUI |
| [v1.0.0](https://github.com/reloadlife/openvpnd/releases/tag/v1.0.0) | **Production agent**: backup/RBAC/readyz/audit/take-over; mgmt thrash fix; client adopt+throughput; role-aware bandwidth; node agent for higher-layer multi-tenant controllers |

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

## Remaining / follow-ups (tracked as GitHub issues post-v1.0.0)

openvpnd **v1.0.0** is the production **node agent** (single host). Multi-tenant multi-node belongs in a higher-layer controller that drives openvpnd + wireguardd.

| Topic | Notes |
|-------|--------|
| Controller agent integration | Secure path, sole writer, enrollment — see issues |
| Event push / webhooks | Controller currently polls |
| Snapshot apply API | Full node desired-state apply |
| Control-plane TLS / ACME | Reverse-proxy or sidecar; not VPN PKI |
| Full LDAP/IdP product | Partial (script verify + preset) |
| Every manpage flag typed | `extra_directives` escape hatch |

## Comparison to wireguardd

Same control-plane shape (SQLite SoT, reconciler, REST, ctl); dataplane is OpenVPN processes. Both are intended as **per-node agents** under a shared higher-layer controller.
