package collect

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"
)

// TestRun_NoDSN_ReturnsEmptyValidPayload proves the dry-run / no-database path
// returns a well-formed, empty payload (non-nil slices) and never errors — the
// pipeline stays exercisable without a DB.
func TestRun_NoDSN_ReturnsEmptyValidPayload(t *testing.T) {
	md, err := Run(context.Background(), "")
	if err != nil {
		t.Fatalf("Run with no DSN must not error, got: %v", err)
	}
	if md.Tables == nil {
		t.Error("Tables must be a non-nil empty slice")
	}
	if md.Deps == nil {
		t.Error("Deps must be a non-nil empty slice")
	}
	if len(md.Tables) != 0 || len(md.Deps) != 0 {
		t.Errorf("expected empty payload, got %d tables and %d deps", len(md.Tables), len(md.Deps))
	}
}

// TestRun_Integration exercises the full collect path against a real Postgres,
// which is the only way to prove the guarded queries return real metadata (and
// only metadata). It is skipped unless LGTY_TEST_DB_DSN points at a reachable
// Postgres — CI provisions one; locally, run against an ephemeral container.
func TestRun_Integration(t *testing.T) {
	dsn := os.Getenv("LGTY_TEST_DB_DSN")
	if dsn == "" {
		t.Skip("set LGTY_TEST_DB_DSN to a Postgres DSN to run the integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	seedDB(ctx, t, dsn)

	md, err := Run(ctx, dsn)
	if err != nil {
		t.Fatalf("Run against real Postgres failed: %v", err)
	}

	// Index the tables in our test schema by name.
	byName := map[string]TableMeta{}
	for _, tb := range md.Tables {
		if tb.Schema == testSchema {
			byName[tb.Name] = tb
		}
	}

	customers, ok := byName["customers"]
	if !ok {
		t.Fatalf("expected table %s.customers in payload; tables=%+v", testSchema, md.Tables)
	}
	if customers.ColumnCount != 3 {
		t.Errorf("customers.ColumnCount = %d, want 3", customers.ColumnCount)
	}
	// reltuples is an ESTIMATE populated by ANALYZE — assert it is present and
	// plausible, never an exact-count assertion (we must never COUNT(*)).
	if customers.RowEstimate < 1 {
		t.Errorf("customers.RowEstimate = %d, want a positive estimate after ANALYZE", customers.RowEstimate)
	}
	if customers.TotalBytes <= 0 {
		t.Errorf("customers.TotalBytes = %d, want > 0", customers.TotalBytes)
	}

	orders, ok := byName["orders"]
	if !ok {
		t.Fatalf("expected table %s.orders in payload", testSchema)
	}
	if orders.ColumnCount != 3 {
		t.Errorf("orders.ColumnCount = %d, want 3", orders.ColumnCount)
	}

	// The foreign key orders.customer_id -> customers.id must surface as an edge.
	foundEdge := false
	for _, d := range md.Deps {
		if d.FromSchema == testSchema && d.FromTable == "orders" &&
			d.ToSchema == testSchema && d.ToTable == "customers" {
			foundEdge = true
		}
	}
	if !foundEdge {
		t.Errorf("expected FK edge %s.orders -> %s.customers in deps; got %+v", testSchema, testSchema, md.Deps)
	}
}

const testSchema = "lgty_action_test"

func seedDB(ctx context.Context, t *testing.T, dsn string) {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open seed connection: %v", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		t.Fatalf("ping Postgres at LGTY_TEST_DB_DSN: %v", err)
	}

	stmts := []string{
		`DROP SCHEMA IF EXISTS ` + testSchema + ` CASCADE`,
		`CREATE SCHEMA ` + testSchema,
		`CREATE TABLE ` + testSchema + `.customers (id bigint PRIMARY KEY, name text, email text)`,
		`CREATE TABLE ` + testSchema + `.orders (
			id bigint PRIMARY KEY,
			customer_id bigint REFERENCES ` + testSchema + `.customers(id),
			total numeric
		)`,
		`INSERT INTO ` + testSchema + `.customers SELECT g, 'name'||g, 'e'||g FROM generate_series(1,5) g`,
		`INSERT INTO ` + testSchema + `.orders SELECT g, ((g % 5) + 1), g * 10 FROM generate_series(1,12) g`,
		`ANALYZE ` + testSchema + `.customers`,
		`ANALYZE ` + testSchema + `.orders`,
	}
	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			t.Fatalf("seed statement failed (%.60q): %v", s, err)
		}
	}
	t.Cleanup(func() {
		cctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = db.ExecContext(cctx, `DROP SCHEMA IF EXISTS `+testSchema+` CASCADE`)
		_ = db.Close()
	})
}
