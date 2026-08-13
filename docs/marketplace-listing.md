# GitHub Marketplace listing — draft copy

Draft copy for the `trulayer/lgty-action` GitHub Marketplace listing. This is the
text a human with `trulayer` org-admin rights pastes into the Marketplace
publishing flow. It is not the listing itself.

> **This listing cannot be submitted by an agent.** Publishing to GitHub
> Marketplace requires (a) a tagged public release of this repo, (b) an account
> with two-factor authentication enabled, and (c) a human accepting the GitHub
> Marketplace Developer Agreement. Steps (b) and (c) are one-time human actions.
> See the [submission checklist](#submission-checklist) at the bottom.

All copy below obeys the LGTY banned-language / never-certify rule: this action
**transmits metadata**; it does not make anything "safe", "verified", or
"proven", and it renders no verdict. Keep it that way on every edit.

> **Note (2026-08-09):** the binary now has a second subcommand, `renders`,
> which uploads CI-rendered PNG screenshots for Visual Review — see the
> [README](../README.md) and [`docs/inputs-outputs.md`](inputs-outputs.md).
> The listing name and tagline below still describe the `metadata` subcommand
> only. That stays accurate for `metadata` alone but is no longer a complete
> description of everything this repository ships. Whether the listing should
> be renamed, given a second tagline mentioning renders, or left as-is
> (metadata is still the subcommand a new customer meets first) is an open
> positioning question, not a technical one. This submission is blocked on
> the checklist below regardless.

---

## Listing name

**LGTY Production Impact metadata**

This is the `name:` field in [`action.yml`](../action.yml) and is what appears on
the Marketplace. Per GitHub's rules the name must be unique — it cannot match an
existing Marketplace action name, an existing category name, or another user/org
name. "LGTY Production Impact metadata" satisfies all three; if GitHub reports a
collision at submission time, prefer a minimal disambiguation
("LGTY Production Impact DB metadata") over renaming the action, since the name
is public API once listed.

## Short description (tagline)

> Uploads read-only Postgres metadata (row estimates, sizes, FK edges) over OIDC to power LGTY Production Impact. Never row data.

Keep it to one line and well under GitHub's tagline length limit. This mirrors the
`description:` field in `action.yml` — keep the two in sync.

## Categories

- **Primary category:** Continuous integration
- **Secondary category:** Code review

Rationale: the action *runs in CI* (primary), and the signal it feeds —
Production Impact — surfaces during *code review* (secondary). The exact labels
in the Marketplace dropdown are chosen by the human submitting; if these two
aren't present verbatim, pick the closest available (e.g. "Monitoring" is a
reasonable alternate secondary).

## Branding

Set in `action.yml` and rendered on the listing:

- Icon: `database`
- Color: `blue`

## Listing description body

GitHub renders this repo's [`README.md`](../README.md) as the body of the
Marketplace listing, so the README **is** the listing copy — there is no separate
long-description field to maintain. Before submitting, confirm the README opens
by answering three questions in its first screen:

1. **What is this?** A tier-2, metadata-only CI uploader for LGTY Production Impact.
2. **What does it send / never send?** Identifiers, estimates, sizes, counts, and
   FK edges — never rows, values, column names, PII, secrets, or DDL.
3. **How do I trust it?** The repo is public on purpose; read the three queries
   and the guard, or run `dry-run: true` and read the payload.

If you want a standalone paragraph to paste into any surface that needs prose
rather than the full README, use this:

> **LGTY Production Impact metadata** runs in your CI, authenticates with a
> short-lived OIDC token — no long-lived secret — and sends LGTY the read-only
> database metadata that powers Production Impact: table names, row-count
> estimates, table sizes, column counts, and foreign-key dependency edges. It
> never reads or transmits row data, column values, PII, secrets, or schema
> definitions, and the metadata-only boundary is enforced in code in this public
> repository. A missing upload is treated as *not connected*, never as an
> all-clear.

## Suggested "usage" snippet for the listing

The Marketplace shows an install snippet. Use the same one the README leads with,
so a customer sees identical copy in both places:

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
          db-dsn: ${{ secrets.LGTY_READONLY_DSN }}
          # dry-run: true   # print the payload instead of sending it
```

---

## Submission checklist

For the human org-admin doing the actual publish:

- [ ] A signed, tagged public release exists (a `vX.Y.Z` tag with a GitHub
      Release). Marketplace requires a release to publish. *(Blocked on the
      release-pipeline work — see the README "Status" section.)*
- [ ] `action.yml` is at the repo root with `name`, `description`, and a
      `branding` block (icon + color). ✅ present.
- [ ] The listing name is unique on Marketplace (checked at submission).
- [ ] Two-factor authentication is enabled on the publishing account.
- [ ] The GitHub Marketplace Developer Agreement has been accepted for the
      `trulayer` org.
- [ ] Primary category selected (Continuous integration); secondary optional
      (Code review).
- [ ] README renders cleanly as the listing body (the three-question check above).
- [ ] "Publish this Action to the GitHub Marketplace" is checked when drafting
      the release, and a release title/notes are provided.
