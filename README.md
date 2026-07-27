# lgty-action

The LGTY **tier-2 metadata uploader**. A single Go binary — one direct dependency, the Postgres driver — that runs in **your** CI, authenticates with a short-lived **OIDC** token (no long-lived secret), and sends LGTY the read-only database **metadata** it needs to power **Production Impact** — table names, row-count **estimates**, sizes, and foreign-key dependency edges.

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

## If this action never runs

Production Impact is only as complete as the metadata that reaches it. If this action isn't installed on a repo, a workflow run fails, or OIDC/database access isn't available, LGTY simply has no production metadata for that repo — and that is rendered as **not connected / uninstrumented**, never as a clean bill of health. A missing upload does not read as "no impact"; there is no code path here, or in the backend that consumes this payload, that turns silence into a pass.

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

## Versioning

Pin this action the standard GitHub Actions way — a moving major-version tag:

```yaml
- uses: trulayer/lgty-action@v1
```

`@v1` will move forward as fixes and additive capability ship within the v1 input/output contract; a breaking change to `action.yml`'s inputs or the payload shape bumps to `@v2`. That's the same convention used by `actions/checkout`, `codecov/codecov-action`, and most of the Marketplace — pin the major, get patches for free, opt in to breaking changes explicitly.

**This depends on a tagged, signed release existing.** Until this repo's release automation ships a signed `v1.0.0` and the moving `v1` tag, `@v1` in the example above is aspirational — pin to a full commit SHA instead:

```yaml
- uses: trulayer/lgty-action@<40-character-commit-sha>
```

For the strictest supply-chain posture — worth considering for this action specifically, since it authenticates to your database — pin to a full commit SHA even after `@v1` exists, per [GitHub's own guidance on using third-party actions](https://docs.github.com/en/actions/security-for-github-actions/security-guides/security-hardening-for-github-actions#using-third-party-actions). Tags are mutable; a commit SHA is not, and because this repo is public you can diff exactly what changed between any two SHAs before you move.

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
LGTY_DRY_RUN=true LGTY_DB_DSN='postgres://readonly@localhost/app' dist/lgty-action
```

## Configuration

| Env / input | Default | Purpose |
|---|---|---|
| `LGTY_BACKEND_URL` / `backend-url` | `https://api.lgty.ai` | LGTY ingest base URL |
| `LGTY_DB_DSN` / `db-dsn` | — | read-only Postgres DSN, scoped role, stored as a CI secret. Required unless `dry-run: true` |
| `LGTY_DB_KIND` / `db-kind` | `postgres` | database engine — only `postgres` is supported currently |
| `LGTY_DRY_RUN` / `dry-run` | `false` | print the payload instead of sending it; no OIDC token or DB connection required |

This action defines **no step outputs** — it is an uploader, not a gate, and it
returns no verdict for a later step to branch on. For the precise, versioned
contract — every input, the exact metadata payload that leaves your perimeter,
and the exit behavior — see [`docs/inputs-outputs.md`](docs/inputs-outputs.md).
The [`CHANGELOG`](CHANGELOG.md) records what moves within the `@v1` contract.

## Status

The metadata pipeline is complete: OIDC fetch, the guard, the three guarded queries wired to a real Postgres database, and the ingest client, all with unit + integration test coverage (`make test`; the integration test needs a real Postgres via `LGTY_TEST_DB_DSN` and is skipped otherwise).

Release automation is also complete: tagging `vX.Y.Z` produces a signed, checksummed, SBOM'd, attested GitHub Release with zero manual steps (see [Verify a release](#verify-a-release)). What's still outstanding is the GitHub Marketplace submission itself, which needs org-admin rights and Developer Agreement acceptance — a one-time human step this repo's automation doesn't do for you.

Tracked in Linear under **LGT-**.
