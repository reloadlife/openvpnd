# How openvpnd works

`openvpnd` is a Linux daemon that **manages OpenVPN processes** (server-role and client-role instances) from a desired-state database. It renders configuration, starts/stops the `openvpn` binary, samples the management interface for connected clients and traffic, and exposes a REST API. `openvpnctl` is the TUI/CLI control panel.

## Components

```
openvpnctl  ── HTTP / Unix ──►  openvpnd
                                   │
          ┌────────────────────────┼────────────────────────┐
          │                        │                        │
     state.db               timeseries.db            Prometheus
  (instances, clients,         (traffic samples)
   binaries, tokens, PKI)
          │
     reconciler ──► conf files + openvpn process + management socket
```

| Piece | Role |
|-------|------|
| **state.db** | Instances, clients, binary registry, tokens, PKI metadata |
| **timeseries.db** | Traffic samples across restarts |
| **Reconciler** | Ensures each instance’s process and conf match desired state |
| **Binary registry** | Named OpenVPN binaries; pin per instance |
| **Management interface** | Samples connected clients, bytes, status |
| **PKI helpers** | CA / cert lifecycle for TLS modes |
| **openvpnctl** | Full-screen TUI (default) + CLI |

## Instance model

An **instance** is one OpenVPN process with a role:

- **Server role** — listens for peers; uses pool / CCD-style static mapping for tunnel addresses; can push routes/DNS via OpenVPN options.  
- **Client role** — connects outbound to a remote endpoint using stored profile/config.

Desired fields (protocol, ports, topology, push options, binary name, enabled flag, …) live in SQLite. The reconciler writes conf under a runtime directory and supervises the process.

## Address assignment (TUN)

For normal TUN server mode there is **no separate DHCP server**. Tunnel addresses come from OpenVPN’s pool / `ifconfig-push` (CCD). DNS and routes are OpenVPN `push` options (DHCP-*like* signaling inside OpenVPN, not the DHCP protocol).

## Clients and profiles

- Client records track identity, certs/keys as applicable, and static IP assignments.  
- Profile export can produce downloadable/importable client configs (including signed URL flows where configured) for OpenVPN Connect–compatible import schemes.

## Multi-binary registry

Hosts may ship several OpenVPN builds. The registry maps a **name** → binary path (and metadata). Each instance can select which binary to run.

## Observation

- Management-interface polling for online clients and counters  
- Timeseries DB for traffic that survives daemon restart  
- Prometheus metrics endpoint  
- TUI live views for instances, clients, and traffic  

## Related binaries

| Binary | Purpose |
|--------|---------|
| `openvpnd` | Daemon |
| `openvpnctl` | Operator TUI/CLI |

See also [ARCHITECTURE.md](ARCHITECTURE.md), [CONFIGURATION.md](CONFIGURATION.md), and [API.md](API.md).
