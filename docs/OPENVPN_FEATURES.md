# OpenVPN feature matrix

openvpnd is **not** a 1:1 OpenVPN option encyclopedia. Coverage is intentional:

| Tier | Meaning | How |
|------|---------|-----|
| **A — First-class** | Typed API/DB/TUI field, validated, tested | Instance / client models + confgen |
| **B — Extension** | Via `plugins`, `feature_sets`, `env_vars`, multi-binary | [EXTENSIONS.md](EXTENSIONS.md) |
| **C — Escape hatch** | Raw `extra_directives` | Conf append only; operator owns correctness |
| **D — Planned** | Not implemented; tracked below | No conf emission yet |
| **E — Out of scope** | Explicit non-goals for now | See STATUS |

This document is the source of truth for “is option X supported?” and for test planning.

---

## A — First-class (implemented + must stay tested)

### Instance identity / process

| OpenVPN / concept | openvpnd field | Conf / behavior | Tests |
|-------------------|----------------|-----------------|-------|
| Instance name | `name` | conf filename, paths | instance, db, api |
| Role server/client | `role` | `server` / `client` mode | confgen, instance |
| Enabled | `enabled` | start/stop reconciler | api, mock backend |
| Binary pin | `binary_name` / `binary_path` | process exec path | db binaries, api |
| Device type | `dev_type` | `dev` / `dev-type` | confgen |
| Device name | `device` | `dev NAME` + `dev-type` | confgen |
| Protocol | `proto` | `proto` | confgen, instance |
| Listen port (server) | `port` | `port` | confgen, instance auto |
| Local bind | `local_bind` | `local` | confgen |
| Remotes (client) | `remotes` / `remote` | `remote host port [proto]` | confgen, instance |

### Server pool / topology

| OpenVPN | Field | Conf | Tests |
|---------|-------|------|-------|
| `--server` | `server_network` | `server net mask` | confgen, netutil |
| Topology | `topology` | `topology` | confgen |
| ifconfig-pool range | `pool_start` / `pool_end` | pool directives when set | instance validation |
| CCD | clients + `static_ip` | `client-config-dir` + CCD files | confgen CCD, reconcile |
| Suspend | `suspended` | CCD `disable` | confgen CCD |
| Push DNS | `push_dns` | `push "dhcp-option DNS …"` | confgen |
| Push routes | `push_routes` | `push "route …"` | confgen |
| Push domain | `push_domain` | `push "dhcp-option DOMAIN …"` | confgen |
| Redirect GW | `redirect_gateway` | `push "redirect-gateway def1"` | confgen |

### Crypto / PKI

| OpenVPN | Field | Conf / behavior | Tests |
|---------|-------|-----------------|-------|
| Auth mode PKI vs secret | `auth_mode` | ca/cert/key vs `secret` | confgen |
| CA/cert/key paths | `pki_*_path` | `ca`/`cert`/`key` | confgen, pki |
| DH | `pki_dh_path` or none | `dh` / `dh none` | confgen |
| tls-crypt | `pki_tls_crypt_path` | `tls-crypt` | confgen, pki, profile |
| Static key | `static_key_path` | `secret` | confgen |
| Cipher (legacy) | `cipher` | `cipher` | confgen, profile |
| data-ciphers | `data_ciphers` | `data-ciphers` | confgen, profile |
| auth digest | `auth_digest` | `auth` | confgen, profile |
| Managed CA issue | PKI API | files on disk + DB | pki, api |
| Client issue | client create / issue-cert | client cert paths | api, pki |
| CRL / crl-verify | revoke + rebuild-crl | `crl-verify` path | pki, confgen, api |
| Renew leaf | POST renew | re-issue same CN | pki, api |
| max-clients | `max_clients` | `max-clients N` | confgen |
| tls-version-min | `tls_version_min` | `tls-version-min` | confgen |
| tun-mtu / buffers | `tun_mtu` / `sndbuf` / `rcvbuf` | directives | confgen |
| server-ipv6 | `server_ipv6` | `server-ipv6` | confgen |
| auth-user-pass (client) | `auth_user_pass` | `auth-user-pass` | confgen |
| CCD iroute | client `iroutes` | `iroute net mask` | confgen CCD |
| Conf import | POST import | parse → instance | confimport, api |

### Control plane (always injected)

| OpenVPN | Source | Conf | Tests |
|---------|--------|------|-------|
| writepid | runtime | `writepid` | confgen |
| status | runtime | `status … 1` | confgen |
| management unix | runtime | `management … unix` | confgen |
| keepalive | fixed defaults | `keepalive 10 60` | confgen |
| persist-key/tun | fixed | present | confgen |

### Extensions (first-class plumbing)

| OpenVPN | Field | Behavior | Tests |
|---------|-------|----------|-------|
| `--plugin` | `plugins[]` | conf `plugin path args` | confgen, features |
| Process env | `env_vars[]` | child process env | features expand, reconciler uses Expand |
| Feature presets | `feature_sets[]` | expand → plugins/env/extra | features |
| Extra lines | `extra_directives` | appended block | confgen |
| Custom binary | registry + pin | multi-version | db binaries |

### Client profiles (end-user .ovpn)

| Concept | Behavior | Tests |
|---------|----------|-------|
| Inline PEMs | `<ca>`/`<cert>`/`<key>`/`<tls-crypt>` | confgen profile |
| remote from public_endpoint | host:port split | confgen profile |
| explicit-exit-notify (UDP) | always for UDP profiles | confgen profile |
| auth-nocache | always | confgen profile |
| Presigned `/p/{token}` | public download | api |
| openvpn://import-profile/ | deep link | api |

### VPN users (server clients)

| Concept | Field / API | Tests |
|---------|-------------|-------|
| CN | `common_name` | api, db |
| Auto static IP | empty/`auto` | netutil, api |
| Issue cert default | `issue_cert` nil→auto | api |
| Mint profile link | `mint_profile_link` | api |
| Suspend/resume | endpoints | api (via CRUD flow) |

---

## B — Extension / preset catalog (builtin)

| Preset ID | Emits | Status |
|-----------|-------|--------|
| `explicit_exit_notify` | `explicit-exit-notify 1` | Done + tested expand |
| `mssfix` | `mssfix` | Done |
| `verb_4` | `verb 4` | Done |
| `fast_io` | `fast-io` | Done |
| `udp_stuffing` | commented fork recipe + binary_name notes | Done (practical template) |
| `udp_stuffing_env` | env `STUFFING_ENABLE=1` | Done |
| `auth_script_template` | `script-security 2` + example auth-user-pass-verify | Done |
| `tls_modern` | `tls-version-min 1.2` + `tls-groups X25519:P-256` | Done |
| `comp_lzo_no` | `comp-lzo no` + push | Done |

Custom presets: `POST /v1/features` — tested via features package merge.

---

## C — Escape hatch only (no typed field)

Anything else in the OpenVPN manpage that is not listed under A/B, for example:

- `tls-version-min`, `tls-cipher`, `tls-groups`
- `sndbuf` / `rcvbuf` / `txqueuelen`
- `fragment`, `mssfix` (also via preset)
- `comp-lzo` / `compress` (also partial preset)
- `http-proxy`, `socks-proxy`
- `script-security`, `up`/`down` scripts (hooks are separate `pre_up` etc. — partial)
- `auth-user-pass`, `auth-user-pass-verify`
- `client-connect` / `client-disconnect` scripts
- `duplicate-cn`, `max-clients`
- `ifconfig-ipv6`, `server-ipv6`
- `port-share`, `multihome`
- `replay-window`, `mute-replay-warnings`
- Provider-specific stuffing options → custom binary + `extra_directives` / plugin

**Rule:** prefer a typed field only when we validate it and test conf emission. Otherwise use C or a named feature preset.

### Hooks (partial first-class)

| Field | Status | Notes |
|-------|--------|-------|
| `pre_up` / `post_up` / `pre_down` / `post_down` | Stored + backend RunHook | Host backend only when `allow_hooks`; needs more tests |

---

## D — Planned (tracked as GitHub issues)

Implementation plans live on the tracker (label `roadmap`):
https://github.com/reloadlife/openvpnd/issues?q=is%3Aissue+is%3Aopen+label%3Aroadmap

| Area | Issue | Notes |
|------|-------|--------|
| Live adopt running openvpn | [#1](https://github.com/reloadlife/openvpnd/issues/1) | `/proc` discover + `POST /adopt` (`take_over` stops verified openvpn PID) |
| Inline PEM on conf import | [#2](https://github.com/reloadlife/openvpnd/issues/2) | materialize under pki_dir |
| Full management API | [#3](https://github.com/reloadlife/openvpnd/issues/3) | kill, hold, status, logs — API done (`POST …/mgmt`, `GET …/status`); TUI pending |
| Bandwidth enforcement | [#4](https://github.com/reloadlife/openvpnd/issues/4) | Done: tc / shaper / log + traffic_limit suspend |
| Full IPv6 pool UX | [#5](https://github.com/reloadlife/openvpnd/issues/5) | beyond `server-ipv6` string |
| TAP / server-bridge | [#6](https://github.com/reloadlife/openvpnd/issues/6) | bridge mode |
| auth-user-pass-verify / LDAP | [#7](https://github.com/reloadlife/openvpnd/issues/7) | server auth plugins |
| CCD multi-ACL | [#8](https://github.com/reloadlife/openvpnd/issues/8) | per-client push overrides |
| tls-ciphers / tls-groups | [#9](https://github.com/reloadlife/openvpnd/issues/9) | typed control-channel crypto |
| Self-update | [#10](https://github.com/reloadlife/openvpnd/issues/10) | GitHub Releases |
| Host integration tests | [#11](https://github.com/reloadlife/openvpnd/issues/11) | real openvpn binary |
| UDP stuffing first-class | [#12](https://github.com/reloadlife/openvpnd/issues/12) | after known fork |

Done earlier (not open issues): CRL/revoke/renew, file conf import, max-clients/tls-version-min/mtu/buffers, iroute, TUI PKI.

---

## E — Out of scope (for now)

- Windows GUI management
- Non-Linux hosts as daemon target
- Replacing OpenVPN with a custom dataplane
- Every historical OpenVPN 2.3 flag

---

## Test ownership matrix

| Area | Primary package | Suite tag (`-run`) |
|------|-----------------|--------------------|
| IP / CIDR / pool | `internal/netutil` | `TestNetutil` / package |
| Instance prepare | `internal/instance` | `TestPrepare` |
| Conf emission | `internal/confgen` | `TestRender` |
| Profile .ovpn | `internal/confgen` | `TestRenderClientProfile` |
| Feature expand | `internal/features` | `TestExpand` |
| SQLite SoT | `internal/db` | `TestStore` |
| PKI | `internal/pki` | `TestCreate` |
| HTTP API | `internal/api` | `TestAPI` / `TestPKI` / `TestProfile` |
| Backend mock | `internal/ovpnbackend` | `TestMock` |
| Metrics | `internal/metrics` | `TestCollector` |
| SNMP | `internal/snmp` | `TestAgent` |
| TUI import | `internal/tui` | `TestParse` |
| Reconcile | `internal/reconcile` | `TestReconcile` (planned expand) |

See [TESTING.md](TESTING.md) for how to run suites and coverage gates.
