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
| `udp_stuffing` | Comment recipe for stuffing forks (`binary_name` required) |
| `udp_stuffing_env` | Sets `STUFFING_ENABLE=1` on the openvpn process |
| `auth_script_template` | `script-security 2` + example `auth-user-pass-verify` |
| `tls_modern` | `tls-version-min 1.2` + `tls-groups X25519:P-256` |
| `mssfix` | `mssfix` |
| `explicit_exit_notify` | UDP client exit notify |
| `verb_4` | Louder logs |
| `fast_io` | `fast-io` |
| `comp_lzo_no` | Disable LZO negotiate |

## UDP stuffing recipe (issue #12)

openvpnd does **not** ship a forked openvpn. Stuffing still needs your binary:

1. Build/install a stuffing-capable openvpn fork somewhere on disk.
2. Register it: `openvpnctl binary add stuffing /path/to/forked/openvpn` (or `POST /v1/binaries`).
3. Create/update the instance with `binary_name: "stuffing"` and feature sets:
   - `udp_stuffing` — well-commented fork-style options in conf (safe for stock openvpn if left commented; uncomment names that match your fork).
   - `udp_stuffing_env` — injects `STUFFING_ENABLE=1` into the supervised process env (for forks that read env instead of conf).
4. Optionally attach a real `.so` via `plugins[]` or a custom preset (`POST /v1/features`).

Stock OpenVPN will **reject** un-commented fork options — leave comments until the fork is registered and option names are verified.

```json
{
  "role": "server",
  "binary_name": "stuffing",
  "feature_sets": ["udp_stuffing", "udp_stuffing_env", "mssfix"],
  "public_endpoint": "vpn.example.com:1194"
}
```

## Auth script recipe

For server-side username/password checks without an LDAP plugin:

1. Install a verifier script (example path only): `/usr/local/libexec/openvpnd-auth.sh`
   - OpenVPN invokes it with `via-env` (`username` / `password` in the environment).
   - Exit `0` accept, non-zero reject. Keep `script-security` ≥ 2.
2. Enable the builtin preset:

```json
{
  "role": "server",
  "feature_sets": ["auth_script_template"],
  "public_endpoint": "vpn.example.com:1194"
}
```

That expands to:

```
script-security 2
# auth-user-pass-verify path is an EXAMPLE — install your script and adjust:
auth-user-pass-verify /usr/local/libexec/openvpnd-auth.sh via-env
```

Replace the path (custom preset or `extra_directives`) before production. Clients still need certs unless you also change `auth` mode; this is dual-factor style (cert + password) when the server already requires TLS certs.

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
