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
- [x] REST API (bearer auth) + Prometheus stub `/metrics`
- [x] Client `.ovpn` export (inline PEMs from paths)
- [x] Presigned profile links + `openvpn://import-profile/` deep links
- [x] Example configs, systemd unit, docs
- [x] Full-screen TUI (`openvpnctl` / `openvpnctl tui`) — instances, clients, binaries, stats, events, forms, profile link + QR

## Not 1:1 with OpenVPN

OpenVPN has hundreds of options. First-class coverage is intentional and incomplete. Use `extra_directives` for the long tail.

### Major gaps

| Area | Status |
|------|--------|
| Managed PKI (issue/sign/revoke/CRL) | Planned |
| Conf import / adopt running processes | Planned |
| TAP / `server-bridge` / VLAN | Not started |
| IPv6 pools | Not started |
| `auth-user-pass` / plugins / LDAP | Not started |
| Full management API surface | Partial |
| Bandwidth enforcement (tc/nft) | Fields only |
| TUI | Done (foundation screens) |
| Self-update from releases | Not started |
| QR codes for profile URLs | Planned |

## Roadmap (priority order)

1. Managed PKI + seamless profile minting
2. Conf parse/import + adopt
3. Richer typed options (common 80%)
4. CCD/`iroute`/ACL model
5. TUI + operator polish
6. Advanced modes (bridge, IPv6, proxies) as needed

## Comparison to wireguardd

openvpnd follows the same control-plane shape (SQLite SoT, reconciler, REST, ctl) but the dataplane is OpenVPN processes instead of kernel WireGuard.
