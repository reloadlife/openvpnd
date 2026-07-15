# Production readiness

openvpnd **v1.0 production posture** for a single Linux admin host (TUN mTLS VPN control plane).

## Supported production profile

| Item | Supported |
|------|-----------|
| OS | Linux amd64/arm64 |
| OpenVPN roles | **server** (TUN, PKI) + **client** (outbound) |
| Auth | mTLS (+ optional `auth-user-pass-verify` script) |
| Topology | `subnet` recommended; net30/p2p optional |
| Bridge / TAP | Available; **lab first** (needs host networking) |
| HA / multi-node | **Not supported** (single SQLite writer) |
| Multi-tenant SaaS RBAC | Scopes: admin / operator / readonly tokens only |

## Production checklist

- [ ] Config from [`configs/openvpnd.production.example.yaml`](../configs/openvpnd.production.example.yaml)
- [ ] `production: true` (strict startup checks)
- [ ] `auth.token` (or `OPENVPND_AUTH_TOKEN`) **not** empty/`change-me` — daemon refuses otherwise
- [ ] `OPENVPND_ALLOW_INSECURE=0` in `/etc/openvpnd/openvpnd.env` (see [`deploy/openvpnd.env.example`](../deploy/openvpnd.env.example)); ignored when `production: true`
- [ ] `openvpn.use_mock_backend: false` and a real `openvpn` binary in the registry
- [ ] `listen.http` bound to localhost or firewalled; TLS reverse-proxy if remote
- [ ] `public_base_url` set to public HTTPS origin for profile links (warned when empty in production)
- [ ] Short profile link TTL (template uses `1h`) + `default_max_uses: 1`
- [ ] Regular **`openvpnd backup`** (state DB + pki_dir + conf_dir) under `/var/backups/openvpnd`
- [ ] Prometheus scrape + disk alerts on `/var/lib/openvpnd`
- [ ] systemd unit enabled; logs via journald
- [ ] OpenVPN binary pinned in registry; `/readyz` reports `default_binary=ok`
- [ ] Documented restore drill once
- [ ] Profile link tokens treated as secrets
- [ ] Optional: `make test-soak` green after control-plane upgrades

### Startup hardening + readiness

| Check | Behavior |
|-------|----------|
| Weak `auth.token` / all weak `auth.tokens` | Refuse start unless `OPENVPND_ALLOW_INSECURE=1` |
| `production: true` + weak token | Always refuse (ALLOW_INSECURE ignored) |
| `production: true` + empty `public_base_url` | Warning log only |
| `GET /readyz` | JSON `status` + `checks` (db, default_binary, conf_dir, pki_dir, backend); **503** if `db` fail; **200** for ok/degraded |
| Mutations under `/v1` | Successful POST/PATCH/DELETE recorded as events kind=`api` (no tokens logged) |

## systemd unit

Unit: [`deploy/openvpnd.service`](../deploy/openvpnd.service)

Production-oriented settings:

| Setting | Value |
|---------|--------|
| `Restart` | `always` / `RestartSec=3` |
| `LimitNOFILE` | `65535` |
| `ProtectSystem` | `strict` |
| `ProtectHome` | `true` |
| `NoNewPrivileges` | `true` |
| `ReadWritePaths` | `/var/lib/openvpnd` `/etc/openvpnd` `/run/openvpnd` `/var/backups/openvpnd` |
| `EnvironmentFile` | `- /etc/openvpnd/openvpnd.env` (optional) |
| Caps | `CAP_NET_ADMIN` (+ bound set for raw/bind) |

Optional: uncomment `MemoryMax=` in the unit on memory-constrained hosts.

```bash
sudo install -m 0644 deploy/openvpnd.service /etc/systemd/system/openvpnd.service
sudo install -d -m 0755 /var/backups/openvpnd
sudo install -m 0600 deploy/openvpnd.env.example /etc/openvpnd/openvpnd.env
# edit token:
sudoedit /etc/openvpnd/openvpnd.env /etc/openvpnd/config.yaml
sudo systemctl daemon-reload
sudo systemctl enable --now openvpnd
```

## Explicit non-goals (still)

- Every OpenVPN manpage option as a typed field
- Clustered SQLite / multi-master
- Windows dataplane
- Full LDAP product (script/plugin only)

## Backup and restore

Archives are **gzip-compressed tar** files with a `MANIFEST.json` (version, host, timestamp, source paths) plus:

| Archive path | Source |
|--------------|--------|
| `db/state.db` (+ `-wal`/`-shm` when present) | `db.path` |
| `db/timeseries.db` (optional) | `db.timeseries_path` or sibling of state DB |
| `pki/**` | `openvpn.pki_dir` |
| `conf/**` | `openvpn.conf_dir` |
| `config/<basename>` | daemon `--config` file when provided |

```bash
# CLI — stop not required for backup; prefer a quiet window / API checkpoint
sudo openvpnd backup \
  --config /etc/openvpnd/config.yaml \
  --out /var/backups/openvpnd/openvpnd-$(date +%F).tar.gz

# API (admin bearer token) — write on host (preferred)
curl -sS -X POST http://127.0.0.1:51980/v1/system/backup \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"path":"/var/backups/openvpnd/openvpnd-api.tar.gz"}'

# API — stream archive to client (omit path)
curl -sS -X POST http://127.0.0.1:51980/v1/system/backup \
  -H "Authorization: Bearer $TOKEN" \
  -o openvpnd-backup.tar.gz

# System info (paths + readiness bits, non-secret)
curl -sS http://127.0.0.1:51980/v1/system/info \
  -H "Authorization: Bearer $TOKEN"

# Restore (service must be stopped; destinations empty or --force)
sudo systemctl stop openvpnd
sudo openvpnd restore \
  --config /etc/openvpnd/config.yaml \
  --in /var/backups/openvpnd/openvpnd-….tar.gz
# or: --force to overwrite existing db/pki/conf
sudo systemctl start openvpnd
```

**Notes**

- Live backups via API call `wal_checkpoint` on the state DB first; still treat restore as a full service outage.
- Restore refuses non-empty destinations unless `--force`.
- Backups include private keys under `pki/` — store offline / encrypted, mode `0600` on the archive.
- Do not restore while `openvpnd` is running (SQLite + conf rewrite races).

## Commands

```bash
# Health
curl -sS http://127.0.0.1:51980/healthz
curl -sS http://127.0.0.1:51980/readyz
```

## Stability soak (pre-cutover)

Mock-backend control-plane stress (no real OpenVPN required):

```bash
make test-soak
```

Creates 8 instances and runs 50 up/down/list/reconcile iterations. Details: [TESTING.md](TESTING.md#api-stability-soak-make-test-soak).

## Security notes

- Bearer tokens are full control when role=admin
- Use **readonly** tokens for dashboards/scrapers that only GET
- Do not enable `openvpn.allow_hooks` unless you trust every instance hook string
- CRL revoke + suspend for compromised clients
- Keep `bandwidth_enforcement` at `off` or `log` until `tc` is validated on the host
