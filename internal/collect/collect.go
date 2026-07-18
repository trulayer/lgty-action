// Package collect gathers read-only database METADATA. It never reads row data;
// see guard.go for the enforced allowlist.
package collect

import (
	"context"
	"database/sql"
	"time"
)

// Metadata is the ONLY thing this action sends. Table names, row-count
// ESTIMATES (never an exact COUNT(*), never a row scan), sizes, column counts,
// and foreign-key dependency edges. No column values, no rows, no PII.
type Metadata struct {
	Workspace   string      `json:"workspace"`
	Repo        string      `json:"repo"`
	CollectedAt time.Time   `json:"collected_at"`
	Tables      []TableMeta `json:"tables"`
	Deps        []DepEdge   `json:"dependencies"`
}

// TableMeta is per-table metadata.
type TableMeta struct {
	Schema      string `json:"schema"`
	Name        string `json:"name"`
	RowEstimate int64  `json:"row_estimate"` // pg_class.reltuples / n_live_tup — an ESTIMATE
	TotalBytes  int64  `json:"total_bytes"`  // pg_total_relation_size
	ColumnCount int    `json:"column_count"` // count only — never column values
}

// DepEdge is a foreign-key dependency between two tables.
type DepEdge struct {
	FromSchema string `json:"from_schema"`
	FromTable  string `json:"from_table"`
	ToSchema   string `json:"to_schema"`
	ToTable    string `json:"to_table"`
}

// Querier is satisfied by *sql.DB. Kept as an interface so the collector is
// driver-agnostic and trivially testable with a fake.
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// Metadata-only SQL. These are compile-time constants; each is passed through
// AssertMetadataOnly before it may execute.
const (
	qRowEstimates = `
SELECT n.nspname AS schema, c.relname AS name,
       c.reltuples::bigint AS row_estimate,
       pg_total_relation_size(c.oid) AS total_bytes
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE c.relkind = 'r' AND n.nspname NOT IN ('pg_catalog', 'information_schema')`

	qColumnCounts = `
SELECT table_schema AS schema, table_name AS name, count(*) AS column_count
FROM information_schema.columns
GROUP BY table_schema, table_name`

	qDependencies = `
SELECT tc.table_schema AS from_schema, tc.table_name AS from_table,
       ccu.table_schema AS to_schema, ccu.table_name AS to_table
FROM information_schema.table_constraints tc
JOIN information_schema.referential_constraints rc ON rc.constraint_name = tc.constraint_name
JOIN information_schema.key_column_usage ccu ON ccu.constraint_name = rc.unique_constraint_name
WHERE tc.constraint_type = 'FOREIGN KEY'`
)

// metadataQueries is the complete, fixed set this action may ever run.
var metadataQueries = []string{qRowEstimates, qColumnCounts, qDependencies}

// Run collects metadata. Every query is guarded first — even in dry-run — so
// the metadata-only guarantee is proven on every invocation. With no DSN it
// returns an empty, valid payload so the pipeline is exercisable without a DB.
func Run(ctx context.Context, dbDSN string) (Metadata, error) {
	md := Metadata{Tables: []TableMeta{}, Deps: []DepEdge{}}

	for _, q := range metadataQueries {
		if err := AssertMetadataOnly(q); err != nil {
			return md, err
		}
	}

	if dbDSN == "" {
		return md, nil // dry-run / no database
	}

	db, err := open(dbDSN)
	if err != nil {
		return md, err
	}
	defer db.Close()

	// TODO(LGT-): execute the guarded queries via db and populate md. The
	// queries and the guard — the load-bearing, trust-critical parts — are
	// complete above; wiring them to pgx is the remaining Phase-0 step.
	_ = db
	_ = ctx
	return md, nil
}

// open returns a read-only-intended *sql.DB. The pgx stdlib driver is added via
//
//	import _ "github.com/jackc/pgx/v5/stdlib"
//
// and is intentionally omitted from this dependency-free Phase-0 skeleton; until
// it is added, a non-dry-run against a real DSN fails fast with a clear
// "unknown driver" error rather than doing anything unsafe.
func open(dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(2)
	return db, nil
}
