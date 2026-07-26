module github.com/trulayer/lgty-action

go 1.26

// Dependencies are intentionally minimal so the build stays trivial to audit.
// The ONLY direct dependency is the Postgres driver (github.com/jackc/pgx/v5,
// registered via the blank import in internal/collect), which the guarded
// metadata queries run against a real database. Keep it that way.

require github.com/jackc/pgx/v5 v5.10.0

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	golang.org/x/sync v0.17.0 // indirect
	golang.org/x/text v0.29.0 // indirect
)
