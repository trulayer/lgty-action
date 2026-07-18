# lgty-action — working context

The LGTY **tier-2 CI metadata uploader**: a single Go static binary that runs in the **customer's** CI, mints a short-lived **OIDC** token, and sends LGTY read-only database **metadata** (row-count estimates, sizes, dependency edges) to power **Production Impact**. Part of the LGTY platform; sibling of `lgty-backend`, `lgty-frontend`, `lgty-marketing`.

## Binding laws (non-negotiable)

1. **Public repo.** This is open source on purpose — auditability *is* the trust model. Keep the code readable and the README honest.
2. **Metadata only, never row data.** Row counts are **estimates** (`pg_class.reltuples` / `n_live_tup`) — never `SELECT count(*)`, never a row scan, never a column value. Enforced by `internal/collect/guard.go`; the complete query set is the constants in `internal/collect/collect.go`. Any new query MUST go through the guard and stay within the system-catalog allowlist.
3. **No long-lived secrets.** Auth is OIDC minted per-run by the CI provider. Never read, store, or transmit a customer credential beyond the scoped read-only DSN they pass in.
4. **Degrade honestly.** If OIDC or the DB is unavailable, fail with a clear message (or dry-run) — never send partial/guessed data.

## Layout

```
main.go                    orchestration: config → OIDC → collect (guarded) → ingest
internal/config            env/input parsing
internal/oidc              GitHub Actions OIDC token fetch (stdlib)
internal/collect           metadata queries + the guard (the trust-critical code)
internal/ingest            POST metadata / print in dry-run
action.yml Dockerfile      Docker-based GitHub Action
```

## Conventions

- **Go 1.26**, standard layout, table tests, contexts on I/O paths, wrapped errors (no panics).
- **Dependency-free** for now (stdlib only) so the build is trivially auditable. The one planned dependency is `github.com/jackc/pgx/v5` (registered via `import _ ".../stdlib"`) when the guarded queries are wired to a real DB — marked `TODO(LGT-)`.
- `make build` produces a static binary; `make fmt vet test` before a PR.
- Every change goes through a PR (no direct push to `main`). CI must be green (`gofmt`, `vet`, `build`, `test`).

## Project management

Tracked in **Linear**, team **LGTY**, prefix **LGT-**. Reference the LGT- issue in PRs.
