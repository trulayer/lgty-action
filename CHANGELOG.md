# Changelog

All notable changes to `lgty-action` are recorded here. This project follows
[Semantic Versioning](https://semver.org): the public contract is the action's
[inputs](docs/inputs-outputs.md#inputs) and the [metadata payload
shape](docs/inputs-outputs.md#what-leaves-your-perimeter--the-payload). A change
to either is a breaking change and bumps the major version; the moving `@v1` tag
only ever moves forward within a compatible contract (see the [README
versioning section](README.md#versioning)).

## [Unreleased]

Nothing has been tagged for public release yet. Until a signed `v1.0.0` and the
moving `v1` tag exist, pin to a full commit SHA rather than `@v1` (see the
README). Entries below accumulate here and are cut into a version at release time.

### Added
- Metadata pipeline: OIDC token fetch, the metadata-only guard, three guarded
  Postgres system-catalog queries wired to a real database, and the ingest
  client.
- `tables[].analyzed` in the metadata payload: `false` when a table has never
  been vacuumed/analyzed, signaling that `row_estimate` came from the
  `pg_stat_user_tables.n_live_tup` fallback rather than a post-`ANALYZE`
  planner statistic (audit 2026-07-31 finding F6).

### Fixed
- `tables[].row_estimate` no longer leaks Postgres's `reltuples = -1`
  "never analyzed" sentinel for tables that have never been vacuumed/analyzed
  — the common case right after a migration creates a table. It now falls
  back to `pg_stat_user_tables.n_live_tup` (audit 2026-07-31 finding F6).
- `dry-run` mode: prints the exact JSON payload to the job log and sends nothing.
- Signed release automation (checksums, cosign keyless signature, SPDX SBOM,
  and GitHub build-provenance attestation per artifact).
- Developer docs: the [inputs/outputs contract](docs/inputs-outputs.md) and the
  [Marketplace listing draft](docs/marketplace-listing.md).

[Unreleased]: https://github.com/trulayer/lgty-action/commits/main
