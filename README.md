# lgty-action

The LGTY **tier-2 metadata uploader**. A single, dependency-free Go binary that runs in **your** CI, authenticates with a short-lived **OIDC** token (no long-lived secret), and sends LGTY the read-only database **metadata** it needs to power **Production Impact** — table names, row-count **estimates**, sizes, and foreign-key dependency edges.

This repository is **public on purpose.** The value of LGTY's Production Impact is that it never touches your data plane — so the code that talks to your database is open for you to read, audit, and pin.

## What it sends

- Table and schema **names**.
- **Row-count estimates** — read cheaply from `pg_class.reltuples` / `pg_stat_user_tables.n_live_tup`. Never `SELECT count(*)`, never a row scan.
- Table **sizes** (`pg_total_relation_size`).
- **Column counts** (a count — never column values).
- **Foreign-key dependency edges** between tables.

## What it never sends

- ❌ Row data — no values, ever.
- ❌ Column contents, PII, secrets.
- ❌ Anything that isn't in the fixed metadata query set.

This is **enforced in code**, not just promised. Every query passes through [`internal/collect/guard.go`](internal/collect/guard.go): SELECT-only, no mutating/file-reading keywords, and only Postgres system catalogs / `information_schema` are allowed. The complete, fixed set of queries the action can ever run is the three constants in [`internal/collect/collect.go`](internal/collect/collect.go). Read them — that is the point.

## Audit it in 2 minutes

1. Read [`internal/collect/collect.go`](internal/collect/collect.go) — the *only* queries this action runs.
2. Read [`internal/collect/guard.go`](internal/collect/guard.go) — the guard that rejects anything else.
3. Run it against your DB with `dry-run: true` — it **prints the exact JSON payload** it would send. Nothing leaves until you've seen it.

## Use it in GitHub Actions

Grant the job OIDC (`id-token: write`) and give it a **read-only** DSN stored as a secret:

```yaml
jobs:
  lgty-metadata:
    runs-on: ubuntu-latest
    permissions:
      id-token: write   # mint the short-lived OIDC token; no long-lived secret
      contents: read
    steps:
      - uses: trulayer/lgty-action@v1
        with:
          db-dsn: ${{ secrets.LGTY_READONLY_DSN }}
          # dry-run: true   # print the payload instead of sending it
```

Use a dedicated **read-only** Postgres role — the guard makes row reads impossible, and a read-only role makes them impossible twice.

## Run it locally

```bash
make build
LGTY_DRY_RUN=true LGTY_DB_DSN='postgres://readonly@localhost/app' dist/lgty-action
```

## Configuration

| Env / input | Default | Purpose |
|---|---|---|
| `LGTY_BACKEND_URL` / `backend-url` | `https://api.lgty.ai` | LGTY ingest base URL |
| `LGTY_DB_DSN` / `db-dsn` | — | read-only Postgres DSN (use a CI secret) |
| `LGTY_DB_KIND` / `db-kind` | `postgres` | database engine |
| `LGTY_DRY_RUN` / `dry-run` | `false` | print the payload instead of sending |

## Status

Phase-0 skeleton: OIDC fetch, the guard, the metadata query set, and the ingest client are in place and the binary builds with zero external dependencies. Wiring the guarded queries to `pgx` (marked `TODO(LGT-)`) is the remaining step.

Tracked in Linear under **LGT-**.
