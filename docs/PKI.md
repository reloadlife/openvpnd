# PKI / mTLS

openvpnd can **generate and manage** a CA plus server/client certificates for OpenVPN TLS.

Material lives under `openvpn.pki_dir` (default `/var/lib/openvpnd/pki`), mode `0700`. Metadata is in SQLite.

## Quick path (recommended)

```bash
# 1. Create CA
openvpnctl pki ca-create default --cn "Example CA" --org "example"

# 2. Server instance + cert + tls-crypt
openvpnctl instance create ovpn0 --role server --network 10.8.0.0/24
# set public endpoint so profiles work:
#   PATCH instance public_endpoint: vpn.example.com:1194
openvpnctl instance issue-cert ovpn0 --ca default --tls-crypt

# 3. Client identity + cert
openvpnctl client create ovpn0 alice --name Alice
openvpnctl client issue-cert ovpn0 alice --ca default

# 4. One-click profile
openvpnctl client link ovpn0 alice
```

## Layout on disk

```text
pki_dir/
  cas/<name>/ca.crt
  cas/<name>/ca.key
  cas/<name>/serial
  certs/<ca>/server/<cn>.crt|.key
  certs/<ca>/client/<cn>.crt|.key
  tls-crypt/<name>.key
```

## API

| Method | Path | Purpose |
|--------|------|---------|
| GET/POST | `/v1/pki/cas` | List / create CA |
| GET/DELETE | `/v1/pki/cas/{name}` | Get / delete metadata |
| GET/POST | `/v1/pki/certs` | List / issue leaf |
| GET | `/v1/pki/certs/{id}` | Get cert record |
| GET/POST | `/v1/pki/tls-crypt` | List / generate tls-crypt |
| POST | `/v1/pki/certs/{id}/revoke` | Revoke leaf + rebuild CA CRL |
| POST | `/v1/pki/certs/{id}/renew` | Re-issue same CN (new key/serial) |
| POST | `/v1/pki/cas/{name}/rebuild-crl` | Rebuild `ca.crl` from revoked list |
| POST | `/v1/instances/{name}/issue-server-cert` | Issue + wire instance paths (+ CRL if any) |
| POST | `/v1/instances/{name}/clients/{cn}/issue-cert` | Issue + wire client paths |

Client create accepts `"issue_cert": true` (+ optional `ca_name`).

## CRL

- On revoke, openvpnd writes `pki_dir/cas/<name>/ca.crl` and sets `cas.crl_path`.
- Server instances using that CA get `pki_crl_path` and conf emits `crl-verify <path>`.
- TUI: **PKI** tab → select cert → `r` revoke, `R` rebuild CRL.

## Conf generation

- Server without `pki_dh_path` emits **`dh none`** (ECDHE; OpenVPN 2.4+)
- Optional `tls-crypt` path when generated/attached
- Client profiles inline CA + client cert/key

## Notes

- Default leaf key: **ECDSA P-256** (`key_type: "rsa"` for RSA 2048+)
- CA default validity: 3650 days; leaves: 825 days
- Re-issue overwrites files for the same CA/kind/CN
- Deleting a CA from the DB does **not** wipe PEMs on disk (safe)
