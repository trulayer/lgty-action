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
- `dry-run` mode: prints the exact JSON payload to the job log and sends nothing.
- Signed release automation (checksums, cosign keyless signature, SPDX SBOM,
  and GitHub build-provenance attestation per artifact).
- Developer docs: the [inputs/outputs contract](docs/inputs-outputs.md) and the
  [Marketplace listing draft](docs/marketplace-listing.md).

[Unreleased]: https://github.com/trulayer/lgty-action/commits/main
