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

## Shipped baseline

| Item | Location |
|------|----------|
| Tag / release | [v0.1.0](https://github.com/reloadlife/openvpnd/releases/tag/v0.1.0) |
| Commit | `bf4e225` |
| Roadmap issues | [label:roadmap](https://github.com/reloadlife/openvpnd/issues?q=is%3Aissue+is%3Aopen+label%3Aroadmap) |

## Roadmap (GitHub issues)

Tracked with detailed plans for later implementation. Prefer working an issue over ad-hoc scope creep.

### High priority

| Issue | Topic |
|-------|--------|
| [#1](https://github.com/reloadlife/openvpnd/issues/1) | Adopt live OpenVPN processes |
| [#2](https://github.com/reloadlife/openvpnd/issues/2) | Conf import materializes inline PEMs |
| [#3](https://github.com/reloadlife/openvpnd/issues/3) | Richer management API surface |

### Medium priority

| Issue | Topic |
|-------|--------|
| [#4](https://github.com/reloadlife/openvpnd/issues/4) | Bandwidth / traffic limit enforcement |
| [#5](https://github.com/reloadlife/openvpnd/issues/5) | Full dual-stack IPv6 |
| [#6](https://github.com/reloadlife/openvpnd/issues/6) | TAP server-bridge |
| [#7](https://github.com/reloadlife/openvpnd/issues/7) | auth-user-pass-verify / LDAP |
| [#8](https://github.com/reloadlife/openvpnd/issues/8) | Richer CCD ACL |
| [#11](https://github.com/reloadlife/openvpnd/issues/11) | Host-backend integration tests |

### Lower priority

| Issue | Topic |
|-------|--------|
| [#9](https://github.com/reloadlife/openvpnd/issues/9) | Typed tls-cipher / tls-groups |
| [#10](https://github.com/reloadlife/openvpnd/issues/10) | Self-update from Releases |
| [#12](https://github.com/reloadlife/openvpnd/issues/12) | First-class UDP stuffing preset |

Feature matrix: [OPENVPN_FEATURES.md](OPENVPN_FEATURES.md). Testing: [TESTING.md](TESTING.md).

## Comparison to wireguardd

openvpnd follows the same control-plane shape (SQLite SoT, reconciler, REST, ctl) but the dataplane is OpenVPN processes instead of kernel WireGuard.
