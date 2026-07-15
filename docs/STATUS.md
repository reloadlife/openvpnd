# Project status

openvpnd is under active development. This document tracks what works today vs planned work.

## Implemented (foundation)

- [x] Daemon + CLI skeleton (`openvpnd`, `openvpnctl`)
- [x] SQLite desired state + timeseries samples DB
- [x] Named multi-binary registry; per-instance pin
- [x] Instance CRUD: server + client roles
- [x] Server client CRUD (CN), auto static IP from pool, suspend/resume
- [x] Conf generation + CCD generation
- [x] Host process backend + mock backend
- [x] Reconciler loop (ensure conf/process, sample management)
- [x] REST API (bearer auth)
- [x] Prometheus exporter (instance/client metrics + reconcile histograms)
- [x] SNMPv2c agent (optional; GET/GETNEXT/GETBULK)
- [x] Managed PKI: CA create, server/client issue, tls-crypt, wire to instances/clients
- [x] CRL: revoke cert, rebuild CRL PEM, `crl-verify` on servers, renew leaf
- [x] Conf import/adopt (`POST /v1/instances/import` + TUI `I`)
- [x] CCD `iroute` for client-side subnets
- [x] Typed knobs: max-clients, tls-version-min, tun-mtu, sndbuf/rcvbuf, server-ipv6, auth-user-pass
- [x] Extensions: plugins, env vars, feature_sets (builtin + custom), multi-binary
- [x] Client `.ovpn` export (inline PEMs from paths)
- [x] Presigned profile links + `openvpn://import-profile/` deep links
- [x] Example configs, systemd unit, docs
- [x] Full-screen TUI — instances, clients, **PKI**, binaries, stats, events, import, profile QR

## Not 1:1 with OpenVPN

OpenVPN has hundreds of options. First-class coverage is intentional and incomplete.

**Canonical matrix:** [OPENVPN_FEATURES.md](OPENVPN_FEATURES.md) (tiers A–E).  
**How we test it:** [TESTING.md](TESTING.md) + `make test-feature` (1:1 confgen matrix).

| Tier | Meaning |
|------|---------|
| A | First-class typed field — must have confgen/API tests |
| B | Extensions (`feature_sets` / plugins / multi-binary) |
| C | `extra_directives` escape hatch |
| D | Planned |
| E | Out of scope |

### Major gaps (summary)

| Area | Status |
|------|--------|
| Managed PKI (CA + issue + tls-crypt + CRL/revoke/renew) | Done |
| Conf import / adopt | Done (parse + create; live process adopt later) |
| TAP / `server-bridge` / VLAN | Not started |
| IPv6 pools | Partial (`server-ipv6` typed; no dual-stack pool UX) |
| `auth-user-pass` / LDAP | Partial (client `auth_user_pass` flag; no LDAP) |
| CCD `iroute` | Done |
| OpenVPN plugins / custom builds | Done |
| Full management API surface | Partial |
| Bandwidth enforcement (tc/nft) | Fields only |
| TUI PKI | Done |
| Self-update from releases | Not started |
| Test matrix for tier A | Done |

## Roadmap (priority order)

1. Live process adopt + richer conf import (inline PEMs)
2. TAP / server-bridge
3. Bandwidth enforcement (tc/nft)
4. Host-backend integration tests
5. LDAP / auth-user-pass-verify plugins
6. Full dual-stack IPv6 pool model

## Comparison to wireguardd

openvpnd follows the same control-plane shape (SQLite SoT, reconciler, REST, ctl) but the dataplane is OpenVPN processes instead of kernel WireGuard.
