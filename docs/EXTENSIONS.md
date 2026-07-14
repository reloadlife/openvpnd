# OpenVPN extensions (plugins, custom builds, feature sets)

openvpnd does **not** reimplement every OpenVPN option. For custom forks (UDP stuffing,
obfuscation plugins, experimental flags) use the extension model:

1. **Multi-binary** — register a patched `openvpn` and pin `binary_name`
2. **Plugins** — structured `--plugin path args…`
3. **Feature sets** — named bundles (builtin + custom) of directives/plugins/env
4. **Extra directives** — raw conf lines escape hatch
5. **Env vars** — process environment for the openvpn child

## Example: custom binary + UDP stuffing plugin

```bash
# 1. Register forked OpenVPN
openvpnctl binary add stuffing /opt/openvpn-stuffing/sbin/openvpn

# 2. Optional: custom feature preset
curl -sS -X POST -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  http://127.0.0.1:51980/v1/features -d '{
  "id": "my_stuffing",
  "description": "Homelab UDP stuffing",
  "extra_directives": "stuffing-enable\nstuffing-size 128\n",
  "plugins": [{"path": "/opt/openvpn-stuffing/lib/stuffing.so", "args": ["mode=1"]}],
  "env_vars": [{"name": "STUFFING_DEBUG", "value": "0"}]
}'

# 3. Create instance using binary + features
curl -sS -X POST -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  http://127.0.0.1:51980/v1/instances -d '{
  "role": "server",
  "binary_name": "stuffing",
  "feature_sets": ["my_stuffing", "mssfix"],
  "public_endpoint": "vpn.example.com:1194",
  "create_ca_if_empty": true
}'
```

Builtin feature IDs (see `GET /v1/features`):

| ID | Purpose |
|----|---------|
| `udp_stuffing` | Template comments for stuffing forks |
| `mssfix` | `mssfix` |
| `explicit_exit_notify` | UDP client exit notify |
| `verb_4` | Louder logs |
| `fast_io` | `fast-io` |
| `comp_lzo_no` | Disable LZO negotiate |

## Conf output

Plugins render as:

```
plugin /path/to.so arg1 arg2
```

Feature extras + `extra_directives` are appended under:

```
# extensions (feature_sets + extra_directives)
…
```

Env vars are **not** in the conf file — they are set on the supervised process.

## API

| Method | Path |
|--------|------|
| GET | `/v1/features` |
| POST | `/v1/features` |
| DELETE | `/v1/features/{id}` |

Instance fields: `plugins`, `env_vars`, `feature_sets`, `extra_directives`, `binary_name` / `binary_path`.
