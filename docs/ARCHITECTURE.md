# Architecture

## Overview

`openvpnd` manages OpenVPN as **userspace processes**. Desired state lives in SQLite; a reconciler renders configs, starts/stops processes, and samples the management interface.

```
openvpnctl  ── HTTP / Unix ──►  openvpnd
                                   │
          ┌────────────────────────┼────────────────────────┐
          │                        │                        │
     state.db               timeseries.db            Prometheus
  (instances, clients,         (samples)
   binaries, tokens)
          │
     reconciler ──► conf + CCD + openvpn process + management
```

## Domain model

| Concept | Meaning |
|---------|---------|
| **Binary** | Named path to an `openvpn` executable (multi-version support) |
| **Instance** | One OpenVPN process: `role=server` or `role=client` |
| **Client** | Server-side identity (certificate CN) + CCD policy |
| **Profile token** | Time-limited secret for public `.ovpn` download |

## Why not “kernel API like WireGuard”?

WireGuard is configured via netlink/`wgctrl`. OpenVPN is a process with a conf file and optional management socket. openvpnd therefore:

1. Renders conf under `conf_dir`
2. Supervises the process with the selected binary
3. Always injects management/status/pid for control plane access
4. Writes CCD files for per-client policy

## IP assignment (no external DHCP)

For TUN + `--server`:

- Pool from `server_network` (OpenVPN `server` / `ifconfig-pool`)
- Static IPs via CCD `ifconfig-push`
- DNS/domain via OpenVPN `push "dhcp-option …"` (not a DHCP server)

TAP bridge + LAN DHCP is out of scope for the current design.

## Profile distribution

1. Admin mints token: `POST …/profile-link` (bearer auth)
2. User fetches `GET /p/{token}` (token = credential)
3. Optional deep link: `openvpn://import-profile/<download_url>` for OpenVPN Connect

Inline `.ovpn` embeds CA/cert/key so mobile clients need only the URL.

## Security boundaries

- `/v1/*` — admin API, bearer token
- `/p/{token}` — end-user profile, single-purpose secret
- Hooks (`pre_up` / …) disabled by default
- PKI files on disk; daemon does not yet issue certificates (import paths)

## Escaping the model

`extra_directives` appends raw OpenVPN config lines. This is an advanced escape hatch, not a substitute for first-class fields.

## Status vs full OpenVPN

openvpnd is a control plane, not a 1:1 reimplementation of every `openvpn(8)` option. See [STATUS.md](STATUS.md).
