# Contributing

Thanks for contributing to openvpnd.

## Development

```bash
# Go 1.24+
make deps
make test-unit      # fast pure logic
make test-feature   # OpenVPN conf 1:1 matrix + presets
make test-api       # REST + PKI + DB
make test           # full race suite
make cover          # coverage summary
make lint           # requires golangci-lint
make build
```

- Format with `gofmt`
- Prefer small, focused PRs
- **New tier-A OpenVPN options require a confgen subtest** (see [docs/OPENVPN_FEATURES.md](docs/OPENVPN_FEATURES.md) + [docs/TESTING.md](docs/TESTING.md))
- Do not claim “supported” for options that only exist as untested `extra_directives`
- Do not commit secrets, private keys, PEMs, or host-specific configs

## Project layout

| Path | Role |
|------|------|
| `cmd/openvpnd` | Daemon CLI |
| `cmd/openvpnctl` | Control CLI |
| `internal/` | Implementation (not a stable public API) |
| `pkg/api` | HTTP client + types (usable by integrations) |
| `migrations/` | SQLite schema (goose) |
| `configs/` | Example configs only |
| `docs/` | Operator documentation |
| `deploy/` | systemd unit and env example |
| `scripts/` | Install helpers |

## Design notes

- OpenVPN is a **userspace process**; the unit of management is an **instance**, not a kernel interface.
- Prefer typed fields for common options; use `extra_directives` only for the long tail.
- Real DHCP is out of scope for TUN; use OpenVPN pool + CCD.
- Profile links (`/p/{token}`) must never require the admin bearer token.

## License

By contributing, you agree that your contributions are licensed under the
[AGPL-3.0](LICENSE) license of this project.
