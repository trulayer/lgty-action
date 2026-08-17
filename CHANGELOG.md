# Changelog

All notable changes to `lgty-action` are recorded here. This project follows
[Semantic Versioning](https://semver.org): the public contract is the action's
[inputs](docs/inputs-outputs.md#inputs) and the [metadata payload
shape](docs/inputs-outputs.md#what-leaves-your-perimeter--the-payload). A change
to either is a breaking change and bumps the major version (see the [README
versioning section](README.md#versioning) for how to pin, and why there is no
moving major-version tag yet).

## [Unreleased]

Entries accumulate here and are cut into a version at release time.

### Fixed
- **`commit-sha` was read without trimming, and never checked.** It was one of
  the last inputs read via raw `os.Getenv`, so a value carrying a newline from
  a workflow expression or secret store was passed through as-is. It is now
  trimmed like every other input, and a value that survives trimming without
  being a full 40-character hex commit id — an abbreviated SHA, a branch name,
  a tag, a `refs/...` ref — stops the run at startup with a message naming the
  input, instead of being uploaded as an attribution nothing can match.
  Leaving `commit-sha` unset remains the normal path and is unaffected; so is
  an uppercase SHA, which the ingest API accepts. `LGTY_WORKSPACE` (a payload
  field, not an action input) and the `renders-dir` input are trimmed on read
  for the same reason. A `renders-dir` of nothing but whitespace now stops the
  run, and says the value was only whitespace rather than claiming it was
  never set.

## [1.1.0] - 2026-08-16

### Added
- **`tables[].analyzed_at` in the metadata payload:** the timestamp the
  database itself last computed each table's row estimate (`GREATEST` of
  `pg_stat_user_tables.last_analyze` / `last_autoanalyze`). Omitted, not
  zeroed, when the table has never been analyzed — that absence is itself
  meaningful and lets a consumer decline to date an estimate rather than
  invent a date for it. Optional field, additive to the existing payload
  shape; older pinned versions of this action are unaffected.

### Fixed
- **A malformed `LGTY_DB_DSN` could leak the connection string into CI
  logs.** `LGTY_DB_DSN` was the one input read via raw `os.Getenv`, bypassing
  the trimming applied to every other input — a value with stray leading
  whitespace (common from CI secret stores) failed the driver's
  `postgres://` scheme check and fell through to keyword/value parsing,
  whose own error message quotes the connection string (host, port,
  database, role) back into the log. The DSN is now trimmed and parsed with
  `pgx.ParseConfig` at config load, and a parse failure reports only the
  value's length and whether it carried a URL scheme — never the value
  itself.
- README/docs pointed customers at the wrong Postgres grant. The `db-dsn`
  setup guidance previously said only "read-only role," which the obvious
  `GRANT SELECT ON ALL TABLES` satisfies — but that grant is both data-plane
  access (every row, readable) and insufficient for this collector (the
  foreign-key-edge queries need `REFERENCES`, not `SELECT`, and return zero
  edges under a `SELECT`-only grant). README now documents the exact DDL
  (`GRANT REFERENCES`, not `SELECT`), why, a self-serve verification step, and
  `sslmode` guidance — see [Set up the database
  role](README.md#set-up-the-database-role). Also fixed a stale example
  secret name (`LGTY_READONLY_DSN` → `LGTY_METADATA_DB_DSN`) in the README,
  `docs/inputs-outputs.md`, and `docs/marketplace-listing.md` examples — not a
  contract change, just a broken first copy-paste.

## [1.0.0] - 2026-08-12

First tagged, signed public release. Pin to `@v1.0.0` or later — see the
[README versioning section](README.md#versioning) for the exact guarantee
and how to resolve the underlying commit SHA if you'd rather pin that
directly.

### Added
- **`renders` subcommand:** uploads PNG screenshots your own CI
  already rendered to `POST /v1/renders`, then signals `POST
  /v1/renders/complete` so an already-published Visual Review brief can be
  upgraded in place. Authenticates with its own short-lived OIDC token on a
  distinct audience from the metadata subcommand's — the two never share a
  token, a request, or a payload shape. Reads a customer-authored
  `manifest.json` (see [`docs/inputs-outputs.md`](docs/inputs-outputs.md)
  "Renders subcommand") so this stays renderer-agnostic: Playwright, Cypress,
  and Storybook's own runner all satisfy it identically. Resolves the pull
  request's real head SHA itself (never `GITHUB_SHA`'s ephemeral merge commit
  on a `pull_request` event) so a customer never has to compute it by hand.
  `dry-run: true` prints the planned upload — file, dimensions, byte size,
  digest per capture — with no OIDC token or network call, mirroring the
  metadata subcommand's dry-run behavior.
- **`main.go` gained subcommand dispatch** (`lgty-action metadata` /
  `lgty-action renders`; the `command` action input, defaulting to
  `metadata`). No argument at all still runs `metadata` — every
  `uses: trulayer/lgty-action@v1` step written before this change keeps
  behaving exactly as before.
- Metadata pipeline: OIDC token fetch, the metadata-only guard, three guarded
  Postgres system-catalog queries wired to a real database, and the ingest
  client.
- `tables[].analyzed` in the metadata payload: `false` when a table has never
  been vacuumed/analyzed, signaling that `row_estimate` came from the
  `pg_stat_user_tables.n_live_tup` fallback rather than a post-`ANALYZE`
  planner statistic.

### Added (tests / docs, no behavior change)
- Subprocess-level tests asserting the actual exit code and stderr of the
  compiled binary on a 4xx ingest rejection and on a refused connection —
  closing a coverage gap where the only prior tests asserted `Send()`'s
  returned Go error rather than the observable CI outcome.
- [Exit behavior](docs/inputs-outputs.md#exit-behavior) now states explicitly
  whether a failed upload can block a merge, and how to opt a step into
  `continue-on-error: true`.

### Changed
- **The README's "complete set of data" claim is now scoped to the metadata
  subcommand explicitly**, not the binary as a whole — it was true of the
  whole binary only because the binary did one thing. These are two
  separately auditable promises, not one relaxed promise:
  a workflow that never sets `command: renders` never runs that code path,
  and the metadata claim stays exhaustive and literally true for it, exactly
  as before.

### Fixed
- **`action.yml` never actually worked from a real `uses:` step, for either
  subcommand.** `runs.env` tried to forward the runner's OIDC request env
  vars with `${{ env.ACTIONS_ID_TOKEN_REQUEST_URL }}` — the `env` context
  does not exist at that scope in a Docker container action, so this was
  invalid syntax that failed the job before a single step could run,
  independent of `command`. Found by the first real
  `uses: trulayer/lgty-action@...` invocation this action has had. The fix
  is to remove those lines: the
  runner's own container handler already injects
  `ACTIONS_ID_TOKEN_REQUEST_URL`/`_TOKEN` (and the standard `GITHUB_*` vars)
  into every Docker container action automatically once the job has
  `permissions: id-token: write` — no explicit forwarding was ever needed.
- `tables[].row_estimate` no longer leaks Postgres's `reltuples = -1`
  "never analyzed" sentinel for tables that have never been vacuumed/analyzed
  — the common case right after a migration creates a table. It now falls
  back to `pg_stat_user_tables.n_live_tup`.
- `dry-run` mode: prints the exact JSON payload to the job log and sends nothing.
- Signed release automation (checksums, cosign keyless signature, SPDX SBOM,
  and GitHub build-provenance attestation per artifact).
- Developer docs: the [inputs/outputs contract](docs/inputs-outputs.md) and the
  [Marketplace listing draft](docs/marketplace-listing.md).

[Unreleased]: https://github.com/trulayer/lgty-action/compare/v1.1.0...main
[1.1.0]: https://github.com/trulayer/lgty-action/releases/tag/v1.1.0
[1.0.0]: https://github.com/trulayer/lgty-action/releases/tag/v1.0.0
