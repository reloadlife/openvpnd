# Testing

## Goals

1. **1:1 tests for every first-class OpenVPN feature** we claim under tier A in [OPENVPN_FEATURES.md](OPENVPN_FEATURES.md) — each typed field that emits conf must have a confgen assertion.
2. **Package suites** for control plane (API, DB, PKI, backend mock, features, profiles).
3. **No silent regressions** on client one-shot create (cert + IP + profile link).
4. **Document gaps** (tier D) instead of fake green coverage.

We do **not** require 100% line coverage on TUI/CLI chrome; we require behavioral coverage of confgen, validation, API, and PKI.

## Quick commands

```bash
make test              # race + count=1, all packages (excludes //go:build integration|soak)
make test-unit         # fast packages (no race): netutil instance confgen features db
make test-api          # API + PKI + profile integration-style tests
make test-race         # full with -race
make cover             # total coverage summary
make cover-html        # coverage.html for browser
make test-feature      # confgen + features matrices
make test-verify       # master manageability e2e + supporting suites
make test-integration  # host OpenVPN backend (skips if openvpn/CAP missing)
make test-soak         # API stability soak (mock backend; //go:build soak)
```

### Master feature e2e

`TestAllManageabilityFeatures` (`internal/api/features_e2e_test.go`) verifies:

- builtin feature presets · CA/server issue · client one-shot (cert, profile link, iroutes, CCD ACL, bandwidth)
- CRL revoke / rebuild / renew · `crl-verify` · bridge mode · auth-user-pass-verify
- import inline PEMs · adopt from disk · management status/kill/signal + whitelist
- custom plugins + multi-binary pin

## Suites

| Suite | Packages | What it guards |
|-------|----------|----------------|
| **unit** | netutil, instance, confgen, features, stats (when present) | Pure logic, conf emission |
| **store** | db | Migrations, CRUD, tokens, features table |
| **pki** | pki, api (PKI cases) | CA/issue/tls-crypt files |
| **api** | api | REST contract, client create, profiles |
| **backend** | ovpnbackend | Mock ensure/stop/list |
| **obs** | metrics, snmp | Scrapes / MIB |
| **tui-logic** | tui (parse/import) | .ovpn import parsing only |
| **feature** | confgen + features | OpenVPN directive emission |
| **integration** | ovpnbackend (+ reconcile) with `-tags=integration` | Live host backend / openvpn binary |
| **soak** | api with `-tags=soak` | Stability: N instances × 50 up/down/list/reconcile (mock) |

### Recommended CI order

```bash
make test-unit
make test-feature
make test-api
make test-race
# optional on privileged lab hosts with openvpn installed:
make test-integration
# optional longer stability pass (still mock-only, no openvpn required):
make test-soak
```

## CI (GitHub Actions)

Workflows live under [`.github/workflows/`](../.github/workflows/):

| Workflow | Trigger | What it runs |
|----------|---------|--------------|
| **`ci.yml`** | push / PR → `main` | `go mod download` → `go test -race -count=1 ./...` → `TestAllManageabilityFeatures` → `make build` |
| **`smoke.yml`** | tag `v*` + `workflow_dispatch` | `make build`, start daemon with `use_mock_backend: true` on `127.0.0.1:51980`, `curl` `/healthz`, `/readyz`, `/v1/version` (bearer), then `make test-verify` if present |
| **`release.yml`** | tag `v*` | linux/amd64 binaries with `-ldflags` version from tag; draft/non-draft GitHub Release via `softprops/action-gh-release` with `tar.gz` + `SHA256SUMS` |

Notes:

- Actions: `actions/checkout@v4`, `actions/setup-go@v5` (Go **1.24.x**).
- No project secrets required; release uses the default `GITHUB_TOKEN`.
- Unit/API suites are mock-only. Host OpenVPN (`make test-integration`) stays optional / self-hosted — see above.
- Local equivalent of CI: `make test` then `make build`; smoke path matches the mock config in the README quick start.

## Coverage targets (policy)

| Package | Target | Notes |
|---------|--------|-------|
| `internal/confgen` | ≥ 75% | Every tier-A conf path |
| `internal/features` | ≥ 80% | Expand + merge |
| `internal/instance` | ≥ 70% | Prepare/validate |
| `internal/netutil` | ≥ 85% | Pool/CIDR |
| `internal/pki` | ≥ 65% | Issue paths |
| `internal/api` | ≥ 45% | Critical handlers (not every error branch) |
| `internal/db` | ≥ 40% | CRUD + migrations smoke |
| `internal/tui` | best-effort | Logic only (import); full Bubble Tea E2E later |
| `cmd/*` | optional | Manual / smoke |

Measure:

```bash
make cover
go tool cover -func=coverage.out | sort -k3 -n
```

## 1:1 confgen rule

For each **tier A** row in OPENVPN_FEATURES that emits a conf line:

1. A table-driven test in `internal/confgen` sets the field.
2. Asserts the expected directive substring (or absence).
3. Name the subtest after the OpenVPN option: `t.Run("data-ciphers", …)`.

Feature presets: one expand test per builtin ID (or a single table over all builtins).

## Adding a new OpenVPN option

1. Classify tier A / B / C / D in OPENVPN_FEATURES.md.
2. If A: add field → confgen → validation → **test before merge**.
3. If B: add preset or document plugin/binary recipe + expand test.
4. If C: document example `extra_directives` only.
5. If D: add row under Planned; do not emit half-supported conf.

## Fixtures

- Prefer in-memory SQLite (`:memory:`) and `ovpnbackend.NewMock()`.
- Temp dirs for PKI PEM material (`t.TempDir()`).
- No live OpenVPN process required for unit/api suites (host backend is integration-only).

## Host backend integration (`make test-integration`)

Live tests live under `internal/ovpnbackend/host_integration_test.go` with:

```go
//go:build integration
```

Default `go test ./...` and `make test` **do not** build or run them.

### Requirements (lab / deploy host)

| Requirement | Behavior if missing |
|-------------|---------------------|
| `openvpn` on `PATH` or a common path (`/usr/sbin/openvpn`, …) | test **skips** |
| root **or** effective `CAP_NET_ADMIN` (TUN) | test **skips** |
| ability to bind a high localhost UDP port (default `25194`) | may fail (not silent skip) |

What the suite does (short timeout, `t.TempDir` for conf/runtime):

1. `ProbeBinary` against the discovered openvpn binary.
2. Generate a static key (`openvpn --genkey secret …`).
3. `EnsureInstance` with a minimal p2p/static-key conf (no PKI).
4. Assert the process reports **Up**, optionally dial management, then `StopInstance`.

Run:

```bash
make test-integration
# equivalent:
go test -tags=integration -count=1 ./internal/ovpnbackend/ ./internal/reconcile/ -timeout 120s
```

Optional later: privileged / self-hosted CI job. Unit suites stay mock-only.

Pure helpers used by the host path (`FindOpenVPN`, version/PID parsing, …) have ordinary unit tests (no build tag).

## API stability soak (`make test-soak`)

Longer control-plane stress lives in `internal/api/soak_test.go` with:

```go
//go:build soak
```

Default `go test ./...` and `make test` **do not** build or run it.

What it does (mock OpenVPN backend, in-memory SQLite, `t.TempDir` conf/runtime):

1. Create **N=8** server instances (no PKI issue; distinct ports/networks).
2. For **50 iterations**: flip each instance **up** or **down**, **list** instances, **POST /v1/reconcile**, assert `/healthz`.
3. Final down + list — instance count must not drift.

Run:

```bash
make test-soak
# equivalent:
go test -tags=soak -count=1 ./internal/api/ -run TestSoakStability -timeout 120s
```

Use before production cutover or after reconciler/API changes that touch ensure/stop paths. See also [PRODUCTION.md](PRODUCTION.md).

## Planned tests (not yet full)

| Gap | Plan |
|-----|------|
| Reconciler unit | Ensure conf write + EnsureInstance with mock |
| Host backend CI | Privileged / self-hosted job for `make test-integration` |
| TUI model keys | golden / scripted bubbletea later |
| CLI cobra | smoke `go test` with args if extracted from main |
| SNMP bulk walk | Expand agent_test |
| DB features/presets CRUD | db_test table cases |
| Config load | config package YAML round-trip |

## Anti-patterns

- Asserting full conf string equality (brittle) — prefer contains + critical order when needed.
- Testing OpenVPN itself — we test **our generation and control plane**.
- Claiming tier-A support without a confgen subtest.
