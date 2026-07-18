module github.com/trulayer/lgty-action

go 1.26

// Phase-0 skeleton: intentionally dependency-free (stdlib only) so it builds
// anywhere and is trivial to audit. The Postgres driver (github.com/jackc/pgx/v5)
// is added when the guarded metadata queries are wired to a real database.
