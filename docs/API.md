# HTTP API

Base URL: configured `listen.http` (example `http://127.0.0.1:51980`).

Authentication: `Authorization: Bearer <auth.token>` on all `/v1/*` routes.

## Core

| Method | Path | Notes |
|--------|------|--------|
| GET | `/healthz` | Liveness (no auth) |
| GET | `/readyz` | DB ping (no auth) |
| GET | `/v1/version` | Version |
| GET | `/v1/config` | Non-secret runtime config |
| POST | `/v1/reconcile` | Force reconcile |
| GET | `/v1/events` | Audit log |
| GET | `/v1/stats` | Global rollup |

## Binaries

| Method | Path | Notes |
|--------|------|--------|
| GET/POST | `/v1/binaries` | List / register OpenVPN executables |
| GET/DELETE | `/v1/binaries/{name}` | Get / remove |

## Instances

| Method | Path |
|--------|------|
| GET/POST | `/v1/instances` |
| GET/PATCH/DELETE | `/v1/instances/{name}` |
| POST | `/v1/instances/{name}/up` |
| POST | `/v1/instances/{name}/down` |
| POST | `/v1/instances/{name}/restart` |
| GET | `/v1/instances/{name}/export` |

`role` is `server` or `client`. `binary_name` selects a registry entry; `binary_path` overrides.

### Create smart defaults / validation

Empty or `auto` fields are filled:

| Field | Default |
|-------|---------|
| `name` | `ovpn0`, `ovpn1`, … |
| `port` | next free from 1194 |
| `server_network` | free `10.x.0.0/24` |
| `proto` / `topology` / `dev_type` | `udp` / `subnet` / `tun` |
| `data_ciphers` / `auth` | modern GCM set / SHA256 |
| `issue_server_cert` | true when cert paths empty and CA exists |
| `generate_tls_crypt` | true when issuing |

Also: `create_ca_if_empty`, `ca_name`, `server_cn`, `remote` shorthand for clients.

Validated: name pattern, port range, proto, topology, network CIDR + overlap, remotes, public_endpoint, DNS IPs, absolute PKI paths, known binary names.

Response includes `auto_filled: ["name=ovpn1", ...]`.

## Clients (server instances only)

| Method | Path |
|--------|------|
| GET/POST | `/v1/instances/{name}/clients` |
| GET/PATCH/DELETE | `/v1/instances/{name}/clients/{cn}` |
| POST | `.../suspend` · `.../resume` · `.../reset-traffic` |

Empty / `auto` `static_ip` allocates the next free host from `server_network`.

## Client profiles (one-click install)

| Method | Path | Auth | Notes |
|--------|------|------|--------|
| GET | `/v1/instances/{name}/clients/{cn}/client-config` | Bearer | Inline `.ovpn` download |
| POST | `/v1/instances/{name}/clients/{cn}/profile-link` | Bearer | Mint presigned link |
| GET | `/v1/instances/{name}/clients/{cn}/profile-links` | Bearer | List tokens |
| DELETE | `/v1/profile-tokens/{token}` | Bearer | Revoke |
| GET | `/p/{token}` | **Token only** | Public profile download |

`POST profile-link` body (optional):

```json
{ "ttl": "24h", "max_uses": 1, "note": "send to alice" }
```

Response:

```json
{
  "token": "...",
  "download_url": "https://vpn.example.com/p/...",
  "import_url": "openvpn://import-profile/https://vpn.example.com/p/...",
  "expires_at": "...",
  "max_uses": 1
}
```

- `download_url` — open in browser / share; serves `application/x-openvpn-profile`
- `import_url` — OpenVPN Connect deep link (Access Server style)

Requirements to generate a profile:

1. Instance `public_endpoint` (`host` or `host:port`)
2. Instance `pki_ca_path` (and optional `pki_tls_crypt_path`)
3. Client `client_cert_path` + `client_key_path`

Set `public_base_url` in daemon config so links use your public HTTPS origin (not localhost).
