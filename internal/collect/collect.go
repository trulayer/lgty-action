// Package collect gathers read-only database METADATA. It never reads row data;
// see guard.go for the enforced allowlist.
package collect

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	// Registers the "pgx" database/sql driver. This is the single direct
	// dependency of this action; the guarded queries below run through it.
	_ "github.com/jackc/pgx/v5/stdlib"
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
SELECT DISTINCT tc.table_schema AS from_schema, tc.table_name AS from_table,
       ccu.table_schema AS to_schema, ccu.table_name AS to_table
FROM information_schema.table_constraints tc
JOIN information_schema.referential_constraints rc
  ON rc.constraint_catalog = tc.constraint_catalog
 AND rc.constraint_schema = tc.constraint_schema
 AND rc.constraint_name = tc.constraint_name
JOIN information_schema.key_column_usage ccu
  ON ccu.constraint_catalog = rc.unique_constraint_catalog
 AND ccu.constraint_schema = rc.unique_constraint_schema
 AND ccu.constraint_name = rc.unique_constraint_name
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

	tables, err := collectTables(ctx, db)
	if err != nil {
		return md, fmt.Errorf("collect tables: %w", err)
	}
	md.Tables = tables

	deps, err := collectDeps(ctx, db)
	if err != nil {
		return md, fmt.Errorf("collect dependencies: %w", err)
	}
	md.Deps = deps

	return md, nil
}

// queryGuarded runs a query only after it passes the metadata-only guard. Run
// already asserts the full query set up front; asserting again here means no
// query can reach the database without clearing the guard, even if the call
// sites change. Callers own closing the returned *sql.Rows.
func queryGuarded(ctx context.Context, db Querier, query string) (*sql.Rows, error) {
	if err := AssertMetadataOnly(query); err != nil {
		return nil, err
	}
	return db.QueryContext(ctx, query)
}

// collectTables runs qRowEstimates and qColumnCounts and joins them by
// (schema, name). Row estimates define the set of tables we report (ordinary
// tables in non-system schemas); column counts are attached where they match.
// Output is sorted by (schema, name) so the payload is deterministic.
func collectTables(ctx context.Context, db Querier) ([]TableMeta, error) {
	byKey := map[string]*TableMeta{}

	rows, err := queryGuarded(ctx, db, qRowEstimates)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var t TableMeta
		if err := rows.Scan(&t.Schema, &t.Name, &t.RowEstimate, &t.TotalBytes); err != nil {
			_ = rows.Close()
			return nil, err
		}
		tc := t
		byKey[t.Schema+"."+t.Name] = &tc
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	crows, err := queryGuarded(ctx, db, qColumnCounts)
	if err != nil {
		return nil, err
	}
	for crows.Next() {
		var schema, name string
		var cols int
		if err := crows.Scan(&schema, &name, &cols); err != nil {
			_ = crows.Close()
			return nil, err
		}
		if t, ok := byKey[schema+"."+name]; ok {
			t.ColumnCount = cols
		}
	}
	if err := crows.Err(); err != nil {
		_ = crows.Close()
		return nil, err
	}
	if err := crows.Close(); err != nil {
		return nil, err
	}

	out := make([]TableMeta, 0, len(byKey))
	for _, t := range byKey {
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Schema != out[j].Schema {
			return out[i].Schema < out[j].Schema
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// collectDeps runs qDependencies and returns the foreign-key edges, sorted for
// deterministic output.
func collectDeps(ctx context.Context, db Querier) ([]DepEdge, error) {
	rows, err := queryGuarded(ctx, db, qDependencies)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []DepEdge{}
	for rows.Next() {
		var d DepEdge
		if err := rows.Scan(&d.FromSchema, &d.FromTable, &d.ToSchema, &d.ToTable); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].FromSchema != out[j].FromSchema {
			return out[i].FromSchema < out[j].FromSchema
		}
		if out[i].FromTable != out[j].FromTable {
			return out[i].FromTable < out[j].FromTable
		}
		if out[i].ToSchema != out[j].ToSchema {
			return out[i].ToSchema < out[j].ToSchema
		}
		return out[i].ToTable < out[j].ToTable
	})
	return out, nil
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
