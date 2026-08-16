# lgty-action

<sub>Coverage upload is informational and does not gate merges. The badge will return after direct verification of Codecov's default-branch data and repository settings.</sub>

A single Go binary that runs in **your** CI and authenticates with a short-lived **OIDC** token (no long-lived secret). It has **two subcommands**, each a **separately auditable promise** about what leaves your pipeline — a workflow that only ever invokes one of them never runs the other's code, and you can verify that from your own workflow file alone, with nothing to take on trust:

| Subcommand | `command:` | What it sends | Powers |
|---|---|---|---|
| **metadata** (default) | `metadata`, or omit `command` entirely | Read-only Postgres **metadata** — table names, row-count **estimates**, sizes, foreign-key edges. Never row data. | **Production Impact** |
| **renders** | `renders` | PNG screenshots **your own CI already rendered**, plus the small set of identifiers your manifest names. Never a screenshot LGTY took, never anything outside your manifest. | **Visual Review** |

This repository is **public on purpose.** The value of LGTY's read-only integrations is that they never touch your data plane — so the code that talks to your database, and the code that uploads your screenshots, is open for you to read, audit, and pin.

If you never set `command: renders`, the renders code path never executes in your pipeline — the metadata guarantee below is exhaustive and unchanged, and you can confirm that by the fact that you never ran the other subcommand. If you never set anything at all, you are running `metadata` today exactly as before this change shipped.

## Metadata subcommand — what it sends

- Table and schema **names**.
- **Row-count estimates** — read cheaply from `pg_class.reltuples` / `pg_stat_user_tables.n_live_tup`. Never `SELECT count(*)`, never a row scan.
- Table **sizes** (`pg_total_relation_size`).
- **Column counts** (a count — never column values).
- **Foreign-key dependency edges** between tables.

## Metadata subcommand — what it never sends

- ❌ Row data — no values, ever.
- ❌ Column contents, PII, secrets.
- ❌ Anything that isn't in the fixed metadata query set.
- ❌ Anything from the renders subcommand — the two never share a request, a payload, or an OIDC audience.

This is **enforced in code**, not just promised. Every query passes through [`internal/collect/guard.go`](internal/collect/guard.go): SELECT-only, no mutating/file-reading keywords, and only Postgres system catalogs / `information_schema` are allowed. The complete, fixed set of queries this subcommand can ever run is the three constants in [`internal/collect/collect.go`](internal/collect/collect.go). Read them — that is the point.

## Renders subcommand — what it sends

- **Exactly the PNG files your `manifest.json` names**, from the directory you point `renders-dir` at — nothing captured, discovered, or globbed on its own.
- For each: the **commit SHA** (resolved from the pull request's own head, or the pushed commit — see below), the **state identifier** your renderer assigned it, and a **capture key** — viewport size, device scale factor, color scheme, browser engine/version, and (when your CI runner exposes it) the runner image. These are facts about *how the screenshot was taken*, never about what it shows.
- The image bytes themselves are opaque to this action: it decodes them only far enough to validate they are real PNGs within a size bound, the same check the backend repeats on arrival.

## Renders subcommand — what it never sends

- ❌ Anything not named in your manifest — no directory scan, no glob, no "upload everything that looks like a screenshot."
- ❌ A screenshot LGTY rendered — this subcommand renders nothing itself; naming no browser or test framework is deliberate (see below).
- ❌ Anything from the metadata subcommand, or a Postgres credential of any kind.
- ❌ An upload from a forked pull request. GitHub does not grant `id-token: write` OIDC minting to `pull_request`-triggered runs from forks, so this subcommand fails closed on a fork PR before it ever has a token to send — and the backend's own OIDC binding refuses it again independently ([`docs/inputs-outputs.md`](docs/inputs-outputs.md) has the full chain). Neither check is something this action can be configured around: there is no input that bypasses it.

This is **enforced in code**, not just promised. Every manifest passes through [`internal/renders/manifest.go`](internal/renders/manifest.go): strict decode (an unrecognized field fails the run), every image path is checked to resolve *inside* the directory you named (no `../` escape), and every file is validated as a decodable PNG under the size bound before it is read into memory. Nothing is transmitted until a capture has passed all three.

**Renders is renderer-agnostic on purpose.** Playwright, Cypress, Storybook's own test runner, or a hand-rolled script — this subcommand only ever reads a directory of PNGs and a small JSON manifest describing them. It does not launch a browser, does not know what a component is, and does not care which tool produced the pixels. See [`docs/inputs-outputs.md`](docs/inputs-outputs.md) for the exact manifest format — writing it is a few lines in whatever your test framework already runs at the end of a capture step.

## Audit it in 2 minutes

**Metadata:**
1. Read [`internal/collect/collect.go`](internal/collect/collect.go) — the *only* queries this subcommand runs.
2. Read [`internal/collect/guard.go`](internal/collect/guard.go) — the guard that rejects anything else.
3. Run it with `command: metadata, dry-run: true` — it **prints the exact JSON payload** it would send. Nothing leaves until you've seen it.

**Renders:**
1. Read [`internal/renders/manifest.go`](internal/renders/manifest.go) — the guard that decides which files this subcommand will ever open.
2. Read [`internal/renders/upload.go`](internal/renders/upload.go) — the only two requests it ever makes (`POST /v1/renders`, `POST /v1/renders/complete`).
3. Run it with `command: renders, dry-run: true` — it **prints a manifest** of what it would upload (file, dimensions, byte size, digest) without a single network call, OIDC or otherwise.

## If this action never runs

Production Impact and Visual Review are each only as complete as the uploads that reach them. If a subcommand isn't installed on a repo, a workflow run fails, or its OIDC/network access isn't available, LGTY simply has **no data from that path** for that repo — and that is rendered as **not connected / uninstrumented**, never as a clean bill of health. A missing upload does not read as "no impact" or "no visual change"; there is no code path here, or in the backend that consumes either payload, that turns silence into a pass. This is the failure mode Codecov's silent-by-default upload step reproduces (`fail_ci_if_error`/`handle_no_reports_found` both default to `false` in `codecov/codecov-action`) and this action refuses to.

## Set up the database role

**Do not `GRANT SELECT ON ALL TABLES`.** That is the obvious grant, and it is
wrong twice over: it hands this credential data-plane access — every row of
every table, including whatever PII or secrets live in them, which is exactly
what LGTY's architecture promises never to ask for — and it still doesn't work
for this collector (below). Use this instead:

```sql
CREATE ROLE lgty_metadata_ro LOGIN PASSWORD '<generate one, store it nowhere but the CI secret>'
  NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS
  CONNECTION LIMIT 3; -- this binary opens at most 2 (SetMaxOpenConns(2) in
  -- internal/collect/collect.go); 3 bounds a runaway or concurrent run
  -- without being effectively unlimited

GRANT REFERENCES ON ALL TABLES IN SCHEMA public TO lgty_metadata_ro;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT REFERENCES ON TABLES TO lgty_metadata_ro;
```

**This grant only covers the `public` schema — and the collector does not.**
It scans every schema in your database except Postgres' own `pg_catalog` and
`information_schema`, so if you keep tables outside `public`, repeat both the
`GRANT` and `ALTER DEFAULT PRIVILEGES` lines above for each one, substituting
its name. Skipping a schema is not an error anywhere in this pipeline: a
table in an ungranted schema still shows up in the payload (`pg_class` is
world-readable) but with `column_count: 0` and no foreign-key edges — a
silent, plausible-looking gap, not a failure you'd notice. If a table you
expect to see FK edges for doesn't have any, check this before assuming
there genuinely are none.

If a role other than the one running this DDL owns your migrations (e.g. a
dedicated migration user), also repeat the `ALTER DEFAULT PRIVILEGES` line
with `FOR ROLE <that role>` — default privileges are per-grantor, not
per-database, so a table created later by a different role won't inherit a
default privilege set by this one.

Build the DSN from that role with `sslmode` set explicitly (see below), store
it as a CI secret — never inline it in a workflow file — and use that secret
for `db-dsn` in the [workflow snippet below](#metadata).

### Why `REFERENCES`, never `SELECT`

This collector's queries — the three constants in
[`internal/collect/collect.go`](internal/collect/collect.go) — read
`pg_class`/`pg_stat_user_tables` (world-readable, no grant needed for row
estimates or sizes) and four `information_schema` views for column counts and
foreign-key edges. Each of those views only shows a row if you hold **at
least one** of a fixed list of privileges on the underlying table — and
`SELECT` is not the only privilege that qualifies:

| View this collector reads | Row is visible if you hold *any one* of |
|---|---|
| `columns`, `key_column_usage` | `SELECT`, `INSERT`, `UPDATE`, `REFERENCES` |
| `table_constraints`, `referential_constraints` | `INSERT`, `UPDATE`, `DELETE`, `TRUNCATE`, `REFERENCES`, `TRIGGER` |

Two consequences, both counter-intuitive:

1. **`SELECT` is absent from the last two.** A role granted `SELECT` on every
   table would still return **zero foreign-key edges** — the "obvious" grant
   is not just more dangerous, it's insufficient for the job.
2. **`REFERENCES` is the only privilege present in all four rows that conveys
   no ability to read or write a row.** It permits creating a foreign key
   that references a table, and nothing else.

### Skip `pg_monitor`

Don't reach for `pg_monitor` as a "safer read-only" alternative — it's
unnecessary (this collector needs no monitoring role) and it's worse than
either option above: `pg_monitor` confers `pg_read_all_stats`, which exposes
`pg_stat_statements` query text — including literal values from your
application's own queries — the moment that extension is installed. That's a
data leak sitting one `CREATE EXTENSION` away.

### Verify the grant yourself

Don't take this page's word for it — connect as the role and confirm a read
is actually denied:

```bash
psql "$DSN" -v ON_ERROR_STOP=1 -c "SELECT 1;" -c "SELECT 1 FROM <a table with rows> LIMIT 1;"
```

Use `SELECT 1 FROM <table>`, not `SELECT * FROM <table>` — Postgres checks
table-level `SELECT` privilege the moment a table is named in `FROM`,
regardless of which columns you ask for, so `SELECT 1` triggers the exact
same permission check without ever printing a real row into your terminal or
CI log if the grant turns out to be wrong. The whole point of this check is
confirming row data is unreachable; the command that confirms it shouldn't
be the thing that leaks it.

Run it with `-v ON_ERROR_STOP=1`. Without that flag, `psql` exits `0` even
when a statement is denied, so a real `permission denied` and a clean run are
indistinguishable from the exit code alone — a trap that's easy to hit while
setting this up. The `SELECT 1;` first is a control: it must succeed, so a
broken connection string can't masquerade as "row access denied." With
`ON_ERROR_STOP=1` set, the second statement should fail with
`ERROR: permission denied for table <name>` and a non-zero exit.

### Set `sslmode`

libpq's default, `prefer`, negotiates TLS but **silently falls back to
plaintext** if the server declines it — not what you want for a credential
crossing the public internet from a hosted CI runner. Set at least:

```
sslmode=require
```

and `sslmode=verify-full` wherever your provider's certificate chains to a
trusted CA, for server-identity verification on top of encryption. Some
managed Postgres proxies present a certificate that doesn't chain to the
system trust store — Railway's is one — which makes `verify-full` impossible
there without separately pinning that provider's CA; `require` still gets you
encryption in that case, just not protection against an on-path server
impersonating the real one.

### What actually leaves your database

Table names, row-count **estimates**, table sizes, column counts, and
foreign-key edges. Never row values, column values, PII, or secrets — see
[Metadata subcommand — what it sends](#metadata-subcommand--what-it-sends)
above for the full field list. Confirm it yourself rather than trust this
page: run with `dry-run: true` and it prints the exact payload with no
network call.

This exact grant — `REFERENCES` only, no `SELECT` — was tested against a live
PostgreSQL 18.4 database: run as this role, the collector reproduced the same
table count, foreign-key edges, and total column count as running the
identical queries as superuser, with every row-data read attempted against it
independently denied. That result describes what this grant *permits*, on the
database it was tested against — it says nothing about *your* copy of it. Run
the check above against your own database before you rely on it.

## Use it in GitHub Actions

### Metadata

Grant the job OIDC (`id-token: write`) and give it the DSN for the role you
[set up above](#set-up-the-database-role), stored as a secret:

```yaml
jobs:
  lgty-metadata:
    runs-on: ubuntu-latest
    permissions:
      id-token: write   # mint the short-lived OIDC token; no long-lived secret
      contents: read
    steps:
      - uses: trulayer/lgty-action@v1.0.0
        with:
          db-dsn: ${{ secrets.LGTY_METADATA_DB_DSN }}
          # dry-run: true   # print the payload instead of sending it
```

Use a dedicated role scoped to `REFERENCES`, never `SELECT` — see [Set up the
database role](#set-up-the-database-role) above for the exact grant, why it's
that privilege and not the obvious one, and how to verify it yourself.

### Renders

Run this after whatever step already writes screenshots to disk. It needs `id-token: write` too — on a **different** OIDC audience from metadata, requested independently:

```yaml
jobs:
  lgty-visual-review:
    runs-on: ubuntu-latest
    permissions:
      id-token: write
      contents: read
    steps:
      - uses: actions/checkout@v5
      # ... your own steps that render screenshots to visual-review-captures/,
      # writing visual-review-captures/manifest.json alongside them —
      # see docs/inputs-outputs.md for the manifest format.
      - uses: trulayer/lgty-action@v1.0.0
        with:
          command: renders
          renders-dir: visual-review-captures/
          # dry-run: true   # print the manifest instead of uploading
```

If your own org's CI policy rejects mutable action refs (for example Semgrep's
`yaml.github-actions.security.github-actions-mutable-action-tag` rule), it
will flag `actions/checkout@v5` above too, not just this action — that policy
applies to every `uses:` in your workflow file. You'll need to hand-pin the
standard actions (`actions/checkout`, your language setup action, etc.) to
full commit SHAs the same way described below for this action.

## Versioning

**Pin to a tagged release — `@v1.0.0` or later — never to a bare commit from `main`'s history.** `main` is a working branch: it can and has contained commits that fail before this action's own code ever runs (a broken Docker-action manifest is invisible to `go test` and only surfaces on a real `uses:` invocation). A tagged release is the one commit on this whole timeline we are telling you is safe to depend on — every `vX.Y.Z` tag is cut only from a commit that was already green on `main`, and receives its own final go/no-go verification (see [Verify a release](#verify-a-release)) before you'd ever reach for it.

```yaml
- uses: trulayer/lgty-action@v1.0.0
```

**The guarantee this gives you, stated explicitly:** this repository enforces a tag-protection ruleset on every `v*` tag — deletion and force-retargeting are both blocked, with no bypass actor, verifiable yourself via `gh api repos/trulayer/lgty-action/rulesets`. So on this specific repo, `@v1.0.0` is exactly as immutable as pinning the commit SHA it points to: the tag cannot be silently moved out from under you, and the release artifacts at that tag can be independently re-verified at any time via the cosign signature, checksum, and build-provenance attestation below. **What you give up** relative to a raw SHA is nothing on immutability — only that you're trusting this repo's own ruleset rather than needing to trust it, which is a small step, not a large one. Bumping to a new patch/minor release is still a manual, explicit edit of your workflow file, same as a SHA — nothing here auto-updates you.

If you'd rather not rely on the ruleset at all and want the tag reference itself to carry zero trust, resolve and pin the commit SHA the tag points to instead:

```bash
git rev-parse v1.0.0^{commit}
```

```yaml
- uses: trulayer/lgty-action@<the-sha-above>
```

This is the [strictest posture GitHub itself recommends for third-party actions](https://docs.github.com/en/actions/security-for-github-actions/security-guides/security-hardening-for-github-actions#using-third-party-actions) — worth considering here specifically, since this action authenticates to your database and uploads your screenshots. Either way, resolve the SHA from a **tagged release**, never from `main`'s commit list — a SHA you pick off `main` yourself has none of the pre-tag verification a release went through.

We deliberately have **not** cut a moving `@v1` major tag (the "pin the major, get patches for free" convention `actions/checkout` and most of the Marketplace use). A moving tag is convenient, but by definition it has to retarget on every patch release — that reopens exactly the risk pinning to an exact version closes. If enough adopters want that convenience despite the tradeoff, it's a separate, later call; today `@v1.0.0`-and-up exact tags are the only sanctioned mutable-looking reference, and they aren't actually mutable per the ruleset above.

Never pin to `@main` — it can change under you at any time, in ways this README hasn't necessarily caught up to yet, and it carries none of a tagged release's pre-verification.

### Verify a release

Every `vX.Y.Z` tag produces a GitHub Release with, for each platform: the binary archive, a shared `checksums.txt`, a **cosign keyless (Sigstore) signature bundle** over that checksum file (`checksums.txt.bundle`), and an **SPDX 2.3 JSON SBOM** per archive. Nothing is signed with a stored key — every signature and attestation below is keyless, backed by the release workflow's own short-lived GitHub OIDC identity, consistent with this repo's "no long-lived secrets" law.

**1. Verify the checksum file's cosign signature** (this transitively covers every archive, since each archive is a line in `checksums.txt`):

```bash
cosign verify-blob \
  --bundle checksums.txt.bundle \
  --certificate-identity-regexp '^https://github\.com/trulayer/lgty-action/\.github/workflows/release\.yml@refs/tags/v.*$' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt
```

Pinning `--certificate-identity-regexp` to this repo's own release workflow means a signature from a different repo or workflow fails verification — a signature nobody checks the identity of is not a control.

**2. Confirm the archive you downloaded matches `checksums.txt`:**

```bash
shasum -a 256 -c checksums.txt --ignore-missing
```

**3. Verify GitHub's native build-provenance attestation** (binds the artifact digest to this exact workflow run, commit, and runner — separate from, and in addition to, the cosign signature above):

```bash
gh attestation verify lgty-action_vX.Y.Z_linux_amd64.tar.gz --repo trulayer/lgty-action
```

All three checks are independent; running all three (not just one) is the point — signature, checksum, and provenance each catch a different failure mode.

## Run it locally

```bash
make build

# metadata
LGTY_DRY_RUN=true LGTY_DB_DSN='postgres://readonly@localhost/app' dist/lgty-action metadata

# renders — reads ./visual-review-captures/manifest.json and the PNGs it names
LGTY_DRY_RUN=true LGTY_RENDERS_DIR=./visual-review-captures dist/lgty-action renders
```

`dist/lgty-action` with no subcommand argument runs `metadata`, matching every `uses: trulayer/lgty-action@v1` step written before `renders` existed.

## Configuration

Shared by both subcommands:

| Env / input | Default | Purpose |
|---|---|---|
| `LGTY_BACKEND_URL` / `backend-url` | `https://api.lgty.ai` | LGTY ingest base URL |
| — / `command` | `metadata` | which subcommand to run: `metadata` or `renders` |
| `LGTY_DRY_RUN` / `dry-run` | `false` | print the payload/manifest instead of sending it; no OIDC token or network call is made |

Metadata subcommand:

| Env / input | Default | Purpose |
|---|---|---|
| `LGTY_DB_DSN` / `db-dsn` | — | Postgres DSN for a role granted `REFERENCES` (never `SELECT`), stored as a CI secret. Required unless `dry-run: true` — see [Set up the database role](#set-up-the-database-role) |
| `LGTY_DB_KIND` / `db-kind` | `postgres` | database engine — only `postgres` is supported currently |

Renders subcommand:

| Env / input | Default | Purpose |
|---|---|---|
| `LGTY_RENDERS_DIR` / `renders-dir` | — | directory holding `manifest.json` and the PNGs it names. Required when `command: renders` |
| `LGTY_COMMIT_SHA` / `commit-sha` | — | override the resolved commit SHA. Leave unset — the action resolves the pull request's real head itself |

This action defines **no step outputs** for either subcommand — it is an uploader, not a gate, and it
returns no verdict for a later step to branch on. For the precise, versioned
contract — every input, the exact payload shapes, the manifest format, and the
exit behavior — see [`docs/inputs-outputs.md`](docs/inputs-outputs.md).
The [`CHANGELOG`](CHANGELOG.md) records what moves within the `@v1` contract.

## Status

The metadata pipeline is complete: OIDC fetch, the guard, the three guarded queries wired to a real Postgres database, and the ingest client, all with unit + integration test coverage (`make test`; the integration test needs a real Postgres via `LGTY_TEST_DB_DSN` and is skipped otherwise).

The renders pipeline has been **exercised end-to-end against the deployed production backend, on two repos.** Two deliberately-labelled verification pull requests — [`lgty-frontend` #113](https://github.com/trulayer/lgty-frontend/pull/113) and [`kindscan-frontend` #526](https://github.com/trulayer/kindscan-frontend/pull/526), both closed unmerged once confirmed — went through `POST /v1/renders` and `POST /v1/renders/complete` against production, and each produced a real `lgty-app` brief with a before/after gallery whose artifact URLs were independently fetched and confirmed (`200`, `image/png`, correct byte sizes). `kindscan-frontend` in particular is a second, differently-shaped repo (Expo/React Native Web, not Next.js) with no relationship to the code this action was originally built against.

**What that does and doesn't show.** It shows the wire contract works against production end to end, on more than one codebase's rendering setup. It does **not** show Visual Review has been used by a customer — both runs were operator-initiated verification, not organic usage — and it does not cover authenticated-state capture: `kindscan-frontend` has no fixture mechanism for reaching a signed-in state (see its own `visual-review-capture.yml` comments), so its captures are limited to public, unauthenticated routes. A repo whose interesting states are all behind auth is not yet a proven case. If you hit a mismatch, that is a bug worth an issue.

Release automation is also complete: tagging `vX.Y.Z` produces a signed, checksummed, SBOM'd, attested GitHub Release with zero manual steps (see [Verify a release](#verify-a-release)). `v1.0.0` is the first cut release — pin to it or later per [Versioning](#versioning) rather than to a bare commit from `main`. What's still outstanding is the GitHub Marketplace submission itself, which needs org-admin rights and Developer Agreement acceptance — a one-time human step this repo's automation doesn't do for you.
