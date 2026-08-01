# `lgty-action` — inputs / outputs contract

This is the precise, versioned contract for the action. Because this action runs
inside **your** CI and connects to **your** database, its developer surface is a
trust surface — so this document describes only what the shipped binary actually
does, not an aspirational interface. If something here disagrees with
[`action.yml`](../action.yml) or the code, the code wins and this is a bug —
please open an issue.

The inputs below are **public API**: they are covered by the versioning contract
in the [README](../README.md#versioning). A change to an input's name, default,
or required-ness, or to the payload shape below, is a breaking change and bumps
the major version.

## Inputs

The action takes **four** inputs. All are optional at the `action.yml` level; the
"Required" column describes when a value is actually needed at runtime.

| Input | Env (when run as a binary) | Default | Required | Description |
|---|---|---|---|---|
| `backend-url` | `LGTY_BACKEND_URL` | `https://api.lgty.ai` | No | LGTY ingest base URL. Override only for self-hosted / staging ingest. |
| `db-dsn` | `LGTY_DB_DSN` | — | **Yes**, unless `dry-run: true` | Read-only Postgres DSN, scoped to a **read-only role**. Store it as a CI secret; never inline it. |
| `db-kind` | `LGTY_DB_KIND` | `postgres` | No | Database engine. Only `postgres` is supported today; any other value fails fast with a clear error. |
| `dry-run` | `LGTY_DRY_RUN` | `false` | No | If `true`, print the exact metadata payload to the job log and send nothing. No OIDC token or database connection is required in this mode. Accepts `true`/`1`/`TRUE` (anything else is treated as `false`). |

### Inputs you do not set

- **Identity / workspace.** You never pass a workspace ID, API key, or account
  identifier. The action authenticates with a short-lived **OIDC** token minted
  per-run by GitHub Actions; the LGTY backend resolves *which workspace and repo
  this is* from that token's validated claims (repo → App installation →
  workspace). There is no long-lived credential to leak, and nothing to
  misconfigure into the wrong tenant.
- **OIDC audience.** The audience the token is requested for is fixed to match
  the backend's expected value and is not a documented input. (An
  `LGTY_OIDC_AUDIENCE` env var exists for LGTY's own staging use; it is not part
  of the public contract and you should not need it.)

## Outputs

**This action defines no [step outputs](https://docs.github.com/en/actions/sharing-automations/creating-actions/metadata-syntax-for-github-actions#outputs).**
There is no `outputs:` block in `action.yml`, so there is nothing to read via
`steps.<id>.outputs.*` in a later step. This is deliberate: the action is an
**uploader, not a gate**. It does not return a verdict about your database or
your change for a downstream step to branch on, and LGTY does not block merges
(see the [no-merge-blocking design law](../README.md)).

The action has exactly two observable effects:

1. **On a normal run** — it `POST`s the metadata payload (below) to
   `{backend-url}/v1/ingest/metadata` with the OIDC token as a bearer token.
2. **On `dry-run: true`** — it writes that same payload to stdout (the job log)
   as indented JSON and sends nothing over the network.

### What leaves your perimeter — the payload

This JSON document is the *complete* set of data the action can ever transmit.
It is **metadata only** — identifiers, estimates, sizes, and counts. It contains
no row data, no column values, no column names or types, no DDL, no PII, and no
secrets.

```json
{
  "workspace": "",
  "repo": "trulayer/kindscan-backend",
  "collected_at": "2026-07-26T12:00:00Z",
  "tables": [
    {
      "schema": "public",
      "name": "orders",
      "row_estimate": 128934,
      "analyzed": true,
      "total_bytes": 41943040,
      "column_count": 12
    }
  ],
  "dependencies": [
    {
      "from_schema": "public",
      "from_table": "line_items",
      "to_schema": "public",
      "to_table": "orders"
    }
  ]
}
```

| Field | Type | Meaning |
|---|---|---|
| `workspace` | string | Usually empty from the action; the backend fills tenancy in from the validated OIDC claims. |
| `repo` | string | `owner/name`, taken from `GITHUB_REPOSITORY`. |
| `collected_at` | string (RFC 3339, UTC) | When the metadata was gathered. |
| `tables[].schema` / `tables[].name` | string | Table **identifiers** in non-system schemas. Never column-level identifiers. |
| `tables[].row_estimate` | integer | An **estimate** — from `pg_class.reltuples` once the table has been analyzed, or from `pg_stat_user_tables.n_live_tup` before then. Never a `SELECT count(*)`, never a row scan. Postgres reports `reltuples` as `-1` (a sentinel, not a value) for a table that has never been vacuumed/analyzed — e.g. a table a migration just created; this action always resolves that sentinel to the fallback estimate before it leaves your CI, never `-1`. |
| `tables[].analyzed` | boolean | `false` when `row_estimate` came from the `n_live_tup` fallback rather than a post-`ANALYZE` planner statistic — a signal that the estimate is less certain, **not** that the table is empty. |
| `tables[].total_bytes` | integer | `pg_total_relation_size` — on-disk size, not contents. |
| `tables[].column_count` | integer | A **count** of columns. Never column names, types, or values. |
| `dependencies[]` | array | Foreign-key edges between tables, as `(from_schema, from_table) → (to_schema, to_table)`. Table identifiers only — never the FK column names. |

The three queries that produce this — and the guard that rejects anything else —
are the point of this repo being public. Read
[`internal/collect/collect.go`](../internal/collect/collect.go) and
[`internal/collect/guard.go`](../internal/collect/guard.go), or just run with
`dry-run: true` and read the payload it prints.

## Exit behavior

The action exits non-zero (failing the step) in these cases:

| Situation | Behavior |
|---|---|
| `db-kind` is not `postgres` | Fails fast before connecting to anything. |
| `db-dsn` is unset and `dry-run` is not `true` | Fails fast — the action never silently sends an empty payload. |
| OIDC token cannot be obtained (and not `dry-run`) | Fails with a clear message (most often: the job is missing `permissions: id-token: write`). |
| The database is unreachable or a query errors | Fails with the wrapped error. |
| The ingest endpoint returns a non-2xx status | Fails with the status and response body. |
| `dry-run: true` and OIDC is unavailable | **Does not fail** — logs that OIDC was skipped and prints the payload. |
| Success | Exits `0`. |

## What a failure means for coverage

If this action never runs, a run fails, or OIDC/database access is unavailable,
LGTY simply has **no production metadata** for that repo. That absence is
rendered as **not connected / uninstrumented** — never as a clean bill of health.
A missing or failed upload does not read as "no production impact." There is no
code path here, or in the backend that consumes this payload, that turns silence
into a pass. This is the [Codecov failure mode](../README.md#if-this-action-never-runs)
LGTY refuses to reproduce.
