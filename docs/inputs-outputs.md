# `lgty-action` — inputs / outputs contract

This is the precise, versioned contract for the action. Because this action runs
inside **your** CI, connects to **your** database, and (for the renders
subcommand) uploads **your** screenshots, its developer surface is a
trust surface — so this document describes only what the shipped binary actually
does, not an aspirational interface. If something here disagrees with
[`action.yml`](../action.yml) or the code, the code wins and this is a bug —
please open an issue.

The inputs below are **public API**: they are covered by the versioning contract
in the [README](../README.md#versioning). A change to an input's name, default,
or required-ness, to either payload shape below, or to the manifest format, is a
breaking change and bumps the major version.

**Two subcommands, two separately auditable promises.** `command: metadata`
(the default — set it explicitly or omit `command` entirely) and
`command: renders` never share a request, a payload shape, or an OIDC audience.
Each subcommand's "what leaves your perimeter" section below is a complete,
standalone claim about *that subcommand alone* — not about the binary as a
whole. If your workflow only ever runs `metadata`, the renders code path
(`internal/renders/`) never executes, and the metadata claim is exhaustive on
its own; you can verify this from your own workflow file, without needing to
trust anything else in this document.

## Shared inputs

| Input | Env (when run as a binary) | Default | Required | Description |
|---|---|---|---|---|
| `command` | positional CLI argument (`lgty-action metadata` / `lgty-action renders`) | `metadata` | No | Which subcommand to run. |
| `backend-url` | `LGTY_BACKEND_URL` | `https://api.lgty.ai` | No | LGTY ingest base URL. Override only for self-hosted / staging ingest. Shared by both subcommands; each still uses its own path and OIDC audience underneath it. |
| `dry-run` | `LGTY_DRY_RUN` | `false` | No | If `true`, print what the subcommand would send to the job log and send nothing. No OIDC token or network call is made in this mode, for either subcommand. Accepts `true`/`1`/`TRUE` (anything else is treated as `false`). |

### Inputs you do not set, for either subcommand

- **Identity / workspace.** You never pass a workspace ID, API key, or account
  identifier. Each subcommand authenticates with its own short-lived **OIDC**
  token minted per-run by GitHub Actions; the LGTY backend resolves *which
  workspace and repo this is* from that token's validated claims (repo → App
  installation → workspace). There is no long-lived credential to leak, and
  nothing to misconfigure into the wrong tenant.
- **OIDC audience.** Each subcommand's audience is fixed to match the
  backend's expected value for that endpoint and is not a documented input.
  (`LGTY_OIDC_AUDIENCE` and `LGTY_RENDERS_OIDC_AUDIENCE` env vars exist for
  LGTY's own staging use; they are not part of the public contract and you
  should not need them.)

---

## Metadata subcommand (`command: metadata`, the default)

### Inputs

| Input | Env | Default | Required | Description |
|---|---|---|---|---|
| `db-dsn` | `LGTY_DB_DSN` | — | **Yes**, unless `dry-run: true` | Read-only Postgres DSN, scoped to a **read-only role**. Store it as a CI secret; never inline it. |
| `db-kind` | `LGTY_DB_KIND` | `postgres` | No | Database engine. Only `postgres` is supported today; any other value fails fast with a clear error. |

### Outputs

**This subcommand defines no [step outputs](https://docs.github.com/en/actions/sharing-automations/creating-actions/metadata-syntax-for-github-actions#outputs).**
It is an **uploader, not a gate**. It does not return a verdict about your
database or your change for a downstream step to branch on, and LGTY does not
block merges (see the [no-merge-blocking design law](../README.md)).

It has exactly two observable effects:

1. **On a normal run** — it `POST`s the metadata payload (below) to
   `{backend-url}/v1/ingest/metadata` with its own OIDC token as a bearer token.
2. **On `dry-run: true`** — it writes that same payload to stdout (the job log)
   as indented JSON and sends nothing over the network.

### What leaves your perimeter — the payload

This JSON document is the ***complete* set of data the metadata subcommand can
ever transmit**, and it is scoped to this subcommand alone — it says nothing
about the renders subcommand, which is documented, and audited, separately
below. It is **metadata only** — identifiers, estimates, sizes, and counts. It
contains no row data, no column values, no column names or types, no DDL, no
PII, and no secrets.

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
| `tables[].row_estimate` | integer | An **estimate** — from `pg_class.reltuples` once the table has been analyzed, or from `pg_stat_user_tables.n_live_tup` before then. Never a `SELECT count(*)`, never a row scan. Postgres reports `reltuples` as `-1` (a sentinel, not a value) for a table that has never been vacuumed/analyzed — e.g. a table a migration just created; this subcommand always resolves that sentinel to the fallback estimate before it leaves your CI, never `-1`. |
| `tables[].analyzed` | boolean | `false` when `row_estimate` came from the `n_live_tup` fallback rather than a post-`ANALYZE` planner statistic — a signal that the estimate is less certain, **not** that the table is empty. |
| `tables[].total_bytes` | integer | `pg_total_relation_size` — on-disk size, not contents. |
| `tables[].column_count` | integer | A **count** of columns. Never column names, types, or values. |
| `dependencies[]` | array | Foreign-key edges between tables, as `(from_schema, from_table) → (to_schema, to_table)`. Table identifiers only — never the FK column names. |

The three queries that produce this — and the guard that rejects anything else —
are the point of this repo being public. Read
[`internal/collect/collect.go`](../internal/collect/collect.go) and
[`internal/collect/guard.go`](../internal/collect/guard.go), or just run with
`command: metadata, dry-run: true` and read the payload it prints.

### Exit behavior

The metadata subcommand exits non-zero (failing the step) in these cases:

| Situation | Behavior |
|---|---|
| `db-kind` is not `postgres` | Fails fast before connecting to anything. |
| `db-dsn` is unset and `dry-run` is not `true` | Fails fast — never silently sends an empty payload. |
| OIDC token cannot be obtained (and not `dry-run`) | Fails with a clear message (most often: the job is missing `permissions: id-token: write`). |
| The database is unreachable or a query errors | Fails with the wrapped error. |
| The ingest endpoint returns a non-2xx status | Fails with the status and response body. |
| `dry-run: true` and OIDC is unavailable | **Does not fail** — logs that OIDC was skipped and prints the payload. |
| Success | Exits `0`. |

### Does a failed upload block my merge?

**By default, yes it fails the step — and that is deliberate, not a violation of
LGTY's no-merge-blocking design law.** That law says LGTY's
*review verdict* — the brief content — never gates a merge. This subcommand ships
zero verdict and zero output (see Outputs above); it is plumbing,
not a review opinion. A metadata upload that gets silently dropped and reports
success is the [exact Codecov failure mode](../README.md#if-this-action-never-runs)
this product refuses to reproduce: it would mean Production Impact's coverage
went to zero with nothing in the CI log to notice it by. Failing the step is
the cheapest, earliest place to surface that.

Whether a failed step actually blocks a *merge* is entirely **your** call, made
in your own branch-protection settings — LGTY does not register this as a
required check and never will. If you don't want a metadata-upload failure to
ever show red on an unrelated PR, add `continue-on-error: true` to your own
step, the same way you would for any other CI step you consider advisory:

```yaml
- uses: trulayer/lgty-action@v1
  continue-on-error: true
  with:
    db-dsn: ${{ secrets.LGTY_READONLY_DSN }}
```

That failure is still logged and still visible in the job summary either way —
`continue-on-error` only changes whether it turns the *step* (and therefore,
by default, the job) red. LGTY's own CI makes exactly this choice for its own
(unrelated) Codecov upload — see [`.github/workflows/ci.yml`](../.github/workflows/ci.yml)
— because a coverage report really is advisory. A customer's Production Impact
metadata is not advisory to LGTY the same way, so this subcommand's own default
stays a hard failure; `continue-on-error` is there for you to opt into, not a
default LGTY chooses for you.

### What a failure means for coverage

If this subcommand never runs, a run fails, or OIDC/database access is unavailable,
LGTY simply has **no production metadata** for that repo. That absence is
rendered as **not connected / uninstrumented** — never as a clean bill of health.
A missing or failed upload does not read as "no production impact." There is no
code path here, or in the backend that consumes this payload, that turns silence
into a pass. This is the [Codecov failure mode](../README.md#if-this-action-never-runs)
LGTY refuses to reproduce.

---

## Renders subcommand (`command: renders`, LGT-404)

Uploads PNG screenshots your own CI already rendered, so **Visual Review** works
for repos that operate no build LGTY can reach by URL. This subcommand renders
nothing — it reads a directory you point it at and uploads exactly what a
manifest in that directory names.

### Inputs

| Input | Env | Default | Required | Description |
|---|---|---|---|---|
| `renders-dir` | `LGTY_RENDERS_DIR` | — | **Yes** | Directory containing `manifest.json` (below) and the PNG files it names. |
| `commit-sha` | `LGTY_COMMIT_SHA` | — (auto-resolved) | No | Overrides the commit this run's captures are attributed to. Leave unset in normal use. |

### Commit SHA resolution

By default this subcommand resolves the commit itself, from GitHub Actions'
own environment — you do not compute or pass one:

- On a `pull_request`-triggered run, it reads `pull_request.head.sha` from the
  event payload (`GITHUB_EVENT_PATH`) — **never `GITHUB_SHA`**, which on this
  event is GitHub's own ephemeral merge commit, not your pull request's real
  head. Uploading under the merge commit would be silently refused by the
  backend's own binding, which checks the token-derived pull request's actual
  head SHA.
- On any other trigger (`push`, `merge_group`, `workflow_dispatch`), it uses
  `GITHUB_SHA` directly.

Set `commit-sha` explicitly only if you are running this outside a standard
GitHub Actions checkout of the commit you rendered.

### The manifest — `manifest.json`

`renders-dir` must contain a file named `manifest.json`: a bare JSON array,
one entry per capture. Nothing else about your renderer, test framework, or
directory layout matters — this is the entire integration surface.

```json
[
  {
    "file": "dashboard.png",
    "state_id": "dashboard",
    "capture_key": {
      "viewport_width": 1280,
      "viewport_height": 720,
      "device_scale_factor": 1,
      "color_scheme": "light",
      "browser_engine": "chromium",
      "browser_version": "128.0.6613.137"
    }
  }
]
```

| Field | Type | Required | Meaning |
|---|---|---|---|
| `file` | string | Yes | Path to the PNG, relative to `renders-dir`. Must resolve *inside* `renders-dir` (no `..`) and end in `.png`. |
| `state_id` | string | Yes | The state this capture shows — a story ID, a route, or whatever your renderer uses to name it. 1–512 characters. Compared for equality between base and head; two different states are never treated as one change. |
| `capture_key.viewport_width` / `viewport_height` | integer | Yes | 1–20000. |
| `capture_key.device_scale_factor` | number | Yes | 0.1–8. |
| `capture_key.color_scheme` | string | Yes | `"light"` or `"dark"`. |
| `capture_key.browser_engine` | string | Yes | e.g. `chromium`, `firefox`, `webkit`. |
| `capture_key.browser_version` | string | Yes | Whatever version string your renderer reports. |
| `capture_key.runner_image` | string | No | Auto-filled from the CI environment (`ImageOS`/`ImageVersion`) when your CI exposes one and the manifest omits it. The one optional capture-key field — a CI system that does not expose this cannot be made to. |
| `capture_index` | integer | No, defaults to `0` | This capture's position among multiple captures of the *same* state, 0-based. |
| `capture_count` | integer | No, defaults to `1` | How many captures this run took of this state. 1–16. Recorded, not inferred, because an uploaded image cannot be re-rendered to check for flake — with 2 or more, the backend's own same-side agreement check can run; with exactly 1, it structurally cannot, and that fact travels with the outcome rather than being silently dropped. |

`image_format` and `commit_sha` are **not** manifest fields — the subcommand
sets `image_format` itself (always `"png"`, the only format the backend
accepts in v1) and resolves `commit_sha` once for the whole run (see above),
never per capture.

**Why a manifest, not a filename convention or auto-detection.** The capture
key's fields — viewport, browser engine/version, color scheme — are facts
about *how* a screenshot was taken. No generic binary can infer them from
pixels alone, and guessing would be exactly the kind of "roughly true" claim
this action's public commitments refuse to make. A manifest is a few lines for
any renderer to emit at the end of its own capture step (see
[`lgty-frontend`'s own capture workflow](https://github.com/trulayer/lgty-frontend/blob/main/.github/workflows/visual-review-capture.yml)
for a real example) and keeps this subcommand tool-agnostic: Playwright,
Cypress, and Storybook's own runner all satisfy it identically.

### What leaves your perimeter — the payloads

**Per capture — `POST /v1/renders`**, `multipart/form-data` with exactly two
parts, in order:

1. `capture` (`application/json`) — commit SHA, state ID, the capture key
   above (plus the subcommand-filled `image_format`), and the capture
   index/count.
2. `image` (`image/png`) — the exact bytes of the file your manifest named.

This is the *complete* set of data one upload request can ever carry, and it
is scoped to the renders subcommand alone. The order is part of the contract,
not an implementation detail: the backend authorizes the request from the
`capture` part before it reads a byte of `image`, so untrusted image bytes are
never decoded on behalf of an unauthorized caller.

**Once per run, after every capture has been attempted — `POST
/v1/renders/complete`**, a small JSON body: `{"commit_sha": "..."}` and
nothing else. This is what tells the backend a capture run has finished, so an
already-published brief can be upgraded in place with the rendered-state
comparison — see "Timing" below.

### Outputs

**This subcommand defines no step outputs**, for the same reason the metadata
subcommand does not: it is an uploader, not a gate, and returns no verdict.

### Timing — why a completion call exists at all

The pull-request webhook that triggers LGTY's brief lands in about a second;
your CI capture run takes minutes. So the first brief is **always** assembled
before any head capture exists, and it publishes with the rendered-state check
declared not run. Nothing polls or waits. The completion call above is what
tells the backend this run is done, so it can upgrade that same brief in place
— if it never arrives, the first "not run" declaration was already the honest
state, and it simply stands.

A completion for a commit that is **no longer** the pull request's head is a
success that updates nothing (`brief_update_queued: false` in the response) —
those captures are already stored and become the *next* pull request's base
once this one merges, so nothing is lost.

### Fork pull requests are refused

GitHub does not grant `id-token: write` OIDC token minting to a
`pull_request`-triggered workflow run from a fork — so this subcommand fails
closed before it ever has a token to send, with no code path here that works
around that. Independently, the backend's own OIDC-claim binding refuses a
fork PR's captures again: the target pull request is derived from the token's
own `refs/pull/N/...` ref, confirmed against the GitHub App, and a fork's
`pr.Head.Repo.ID` never matches the token's own repository ID. Neither of
these is a configurable behavior — there is no input on this subcommand that
relaxes it.

### Exit behavior

| Situation | Behavior |
|---|---|
| `renders-dir` is unset | Fails fast before reading anything. |
| `manifest.json` is missing, malformed, or fails validation (bad path, wrong extension, missing/invalid capture-key field) | Fails fast, naming the offending capture — no request is ever sent for a manifest that fails validation. |
| The manifest names zero captures | Fails — treated as a likely broken renderer, not an intentional no-op (see README "If this action never runs"). |
| A capture file cannot be read or decoded as PNG, or exceeds the size limit | That capture fails; the rest of the run continues. |
| OIDC token cannot be obtained (and not `dry-run`) | Fails with a clear message. |
| `POST /v1/renders` returns a non-2xx status for a capture (including a fork-PR refusal) | That capture fails with the backend's status and code; the rest of the run continues. |
| One or more captures failed | The run exits non-zero after attempting every capture and the completion call — see below. |
| `POST /v1/renders/complete` fails | The run exits non-zero. |
| `dry-run: true` | Prints the planned upload manifest (file, dimensions, byte size, digest per capture) and makes no network call at all — not even OIDC. Always exits `0`, matching the metadata subcommand's dry-run behavior. |
| Success | Exits `0`. |

**Why the completion call still fires after a partial failure.** A capture
run that uploaded 8 of 10 states successfully still queues the brief upgrade
for those 8 — see TDD §4.3.1's "some states uploaded, others not" outcome.
Skipping the completion call on any individual capture failure would leave a
partially-successful run's coverage stuck on "not run" forever, which is a
worse outcome than reporting the failure loudly (this subcommand still exits
non-zero) while still telling the backend what *did* land.

### What a failure means for coverage

If this subcommand never runs, a run fails, or OIDC access is unavailable,
LGTY simply has **no rendered-state capture** for that commit. That absence is
rendered as **not run** — never as "no visual change." A missing or failed
upload does not read as a clean bill of health; this is the same
[Codecov failure mode](../README.md#if-this-action-never-runs) the metadata
subcommand refuses to reproduce, applied to pixels instead of tables.
