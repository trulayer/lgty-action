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

## Use it in GitHub Actions

### Metadata

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
      - uses: trulayer/lgty-action@v1
        with:
          command: renders
          renders-dir: visual-review-captures/
          # dry-run: true   # print the manifest instead of uploading
```

## Versioning

Pin this action the standard GitHub Actions way — a moving major-version tag:

```yaml
- uses: trulayer/lgty-action@v1
```

`@v1` will move forward as fixes and additive capability ship within the v1 input/output contract; a breaking change to `action.yml`'s inputs, either payload shape, or the manifest format bumps to `@v2`. That's the same convention used by `actions/checkout`, `codecov/codecov-action`, and most of the Marketplace — pin the major, get patches for free, opt in to breaking changes explicitly.

**This depends on a tagged, signed release existing.** Until this repo's release automation ships a signed `v1.0.0` and the moving `v1` tag, `@v1` in the example above is aspirational — pin to a full commit SHA instead:

```yaml
- uses: trulayer/lgty-action@<40-character-commit-sha>
```

For the strictest supply-chain posture — worth considering for this action specifically, since it authenticates to your database and uploads your screenshots — pin to a full commit SHA even after `@v1` exists, per [GitHub's own guidance on using third-party actions](https://docs.github.com/en/actions/security-for-github-actions/security-guides/security-hardening-for-github-actions#using-third-party-actions). Tags are mutable; a commit SHA is not, and because this repo is public you can diff exactly what changed between any two SHAs before you move.

Never pin to `@main` — it can change under you at any time, in ways this README hasn't necessarily caught up to yet.

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
| `LGTY_DB_DSN` / `db-dsn` | — | read-only Postgres DSN, scoped role, stored as a CI secret. Required unless `dry-run: true` |
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

The renders pipeline is implemented and unit-tested against a fake backend server, and its wire contract is written against the backend's merged handler code and API spec. It has **not yet been exercised end-to-end against the deployed production backend** — no run of this subcommand against the real ingest endpoint has been observed, so treat the contract as matched, not as verified in production. Visual Review itself is also not yet enabled for general use, so a successful upload today may have nothing downstream to show for it. If you hit a mismatch, that is a bug worth an issue — this section will be updated when a real end-to-end run has been confirmed.

Release automation is also complete: tagging `vX.Y.Z` produces a signed, checksummed, SBOM'd, attested GitHub Release with zero manual steps (see [Verify a release](#verify-a-release)). What's still outstanding is the GitHub Marketplace submission itself, which needs org-admin rights and Developer Agreement acceptance — a one-time human step this repo's automation doesn't do for you.
