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
| POST | `/v1/instances/import` |
| POST | `/v1/instances/adopt` |
| GET | `/v1/instances/discover` |
| GET/PATCH/DELETE | `/v1/instances/{name}` |
| POST | `/v1/instances/{name}/up` |
| POST | `/v1/instances/{name}/down` |
| POST | `/v1/instances/{name}/restart` |
| GET | `/v1/instances/{name}/export` |
| GET | `/v1/instances/{name}/status` |
| POST | `/v1/instances/{name}/mgmt` |

`role` is `server` or `client`. `binary_name` selects a registry entry; `binary_path` overrides.

### Live status (management interface)

`GET /v1/instances/{name}/status` returns structured live status sampled from the OpenVPN management socket when the process is up:

```json
{
  "name": "ovpn0",
  "up": true,
  "pid": 1234,
  "rx_bytes": 1000,
  "tx_bytes": 2000,
  "connected_clients": 1,
  "clients": [
    {
      "common_name": "alice",
      "real_address": "1.2.3.4:5678",
      "virtual_address": "10.8.0.2",
      "rx_bytes": 1000,
      "tx_bytes": 2000
    }
  ],
  "updated_at": "…"
}
```

If the instance exists but management is unreachable, the handler still returns `200` with `up: false` and an `error` string (not a hard 5xx).

### Management commands (whitelist)

`POST /v1/instances/{name}/mgmt` runs a **single whitelisted** OpenVPN management command. There is no unrestricted raw shell or free-form management RPC.

Body:

```json
{ "command": "status|kill|signal|hold|log|state|bytecount|pid|version", "args": ["..."] }
```

| Command | Args | Notes |
|---------|------|--------|
| `status` | optional (e.g. `"2"`) | Dump status; prefer version 2 CSV |
| `kill` | **required** `args[0]` = CN or `IP:port` | Disconnect a client without suspend flag |
| `signal` | **required** `args[0]` | `SIGUSR1`, `SIGHUP`, `SIGTERM`, `SIGUSR2`, `SIGINT` |
| `hold` | optional (`on` / `release` / …) | Hold / release start |
| `log` | optional | Management log on/off/n |
| `state` | optional | Connection state dump |
| `bytecount` | optional interval | Enable periodic bytecount |
| `pid` | none | OpenVPN process pid |
| `version` | none | OpenVPN + management version |

Response:

```json
{ "output": "SUCCESS: …" }
```

Unknown commands, missing kill/signal args, disallowed signals, or newlines in args → `400`. Instance not running → `409 not_running`. Management ERROR reply → `502 mgmt_error`.

### Import / adopt existing conf

`POST /v1/instances/import` parses an OpenVPN `.conf` / `.ovpn` (server or client) into create fields.

Body:

```json
{
  "name": "optional",
  "content": "port 1194\nproto udp\nserver 10.8.0.0 255.255.255.0\n...",
  "enabled": true,
  "create": true,
  "binary_name": "default",
  "public_endpoint": "vpn.example.com:1194"
}
```

| Field | Notes |
|-------|--------|
| `content` | Required raw conf text |
| `create` | `false` → parse preview only (`200`); `true` or omitted → create instance (`201`) |
| `name` / `enabled` / `binary_name` / `public_endpoint` | Optional overrides applied after parse |

Mapped: role, proto, port, local, dev/dev-type, topology, `server` net+mask → `server_network` CIDR, remotes, cipher/data-ciphers/auth, PKI file paths (`ca`/`cert`/`key`/`dh`/`tls-crypt`/`secret`), push DNS/routes/domain/redirect-gateway, plugins, max-clients/tls-version-min/tun-mtu/buffers/server-ipv6 (folded into `extra_directives`). Control-plane lines (`management`, `status`, `writepid`, `verb`, `persist-*`, `keepalive`) are ignored. Inline `<ca>`… blocks are skipped with a warning (file path refs only).

Response:

```json
{
  "parsed": { "role": "server", "port": 1194, "server_network": "10.8.0.0/24", "...": "..." },
  "warnings": ["..."],
  "instance": { "name": "ovpn0", "...": "..." },
  "auto_filled": ["binary_name=default"]
}
```

`instance` / `auto_filled` are present only when `create=true`. When the conf already has cert+key paths, auto PKI issue is disabled.

### Adopt on-disk conf / discover running processes

`POST /v1/instances/adopt` reads a conf **path on the daemon host**, parses it (same mapper as import), and creates an instance.

```json
{
  "conf_path": "/etc/openvpn/server.conf",
  "name": "optional",
  "enabled": true,
  "binary_name": "default",
  "take_over": false,
  "public_endpoint": "",
  "pid": 0
}
```

| Field | Notes |
|-------|--------|
| `conf_path` | Required absolute path readable by openvpnd |
| `take_over` | When true, response `notes` tell the operator to stop the foreign process so openvpnd can manage it (v1 does **not** SIGTERM foreign PIDs) |
| `pid` | Optional context from discover; recorded in notes only |

Response includes `instance`, `parsed`, `warnings`, `auto_filled`, `notes`, `conf_path`.

`GET /v1/instances/discover` lists running OpenVPN candidates from `/proc` (`pid`, `conf_path`, `cmdline`, `binary`). Conf path is taken from `--config` / bare `*.conf` argv when present.

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
| POST | `.../issue-cert` |
| POST | `.../profile-link` |
| GET | `.../client-config` |

### Create smart defaults

Minimal body: `{ "common_name": "alice" }`.

| Field | Default |
|-------|---------|
| `static_ip` empty/`auto` | next free host in `server_network` |
| `name` empty | same as `common_name` |
| `issue_cert` omitted | **true** when no cert paths and a CA exists |
| `mint_profile_link` | false; set true for one-click install URL in the response |

Response includes `auto_filled`, optional `profile_link` (`download_url` + `import_url`), and `warnings`.

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

## Extensions (custom OpenVPN features)

For forks/plugins (e.g. UDP stuffing), multi-binary registry, and named feature sets:

| Method | Path | Notes |
|--------|------|--------|
| GET/POST | `/v1/features` | List builtin+custom / upsert custom preset |
| DELETE | `/v1/features/{id}` | Delete custom preset (not pure builtins) |

Instance create/update fields:

| Field | Purpose |
|-------|---------|
| `binary_name` / `binary_path` | Pin a registered or absolute OpenVPN build |
| `plugins` | `[{"path":"/opt/p.so","args":["a=1"]}]` → `--plugin` |
| `env_vars` | Process env for the openvpn child |
| `feature_sets` | Preset IDs expanded into plugins/env/extra |
| `extra_directives` | Raw conf lines escape hatch |

See [EXTENSIONS.md](EXTENSIONS.md).
