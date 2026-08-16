package collect

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strings"
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
	if !customers.Analyzed {
		t.Error("customers.Analyzed = false, want true: this table was explicitly ANALYZEd by the test seed")
	}
	// The second clock: WHEN the database computed that estimate. Without it the
	// consumer can only report when this run read the number, which says nothing
	// about how old the statistic itself is.
	if customers.AnalyzedAt == nil {
		t.Error("customers.AnalyzedAt = nil, want the timestamp of the seed's ANALYZE: a consumer with no computation time cannot date the estimate and will decline to present it")
	} else if age := time.Since(*customers.AnalyzedAt); age < 0 || age > time.Hour {
		t.Errorf("customers.AnalyzedAt = %s (age %s), want the ANALYZE this test just ran", customers.AnalyzedAt, age)
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

// TestRun_Integration_NeverAnalyzedTable is the regression test for the
// reltuples=-1 defect: Postgres reports pg_class.reltuples as -1 — a
// sentinel, not a value — for a table that has never been vacuumed/analyzed
// (the common case for a table a migration just created). That sentinel must
// never reach the emitted payload as a row-count estimate; this asserts the
// actual observable Metadata a caller receives, not an intermediate value.
func TestRun_Integration_NeverAnalyzedTable(t *testing.T) {
	dsn := os.Getenv("LGTY_TEST_DB_DSN")
	if dsn == "" {
		t.Skip("set LGTY_TEST_DB_DSN to a Postgres DSN to run the integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	schema := "lgty_action_never_analyzed_test"
	seedNeverAnalyzedTable(ctx, t, dsn, schema)

	md, err := Run(ctx, dsn)
	if err != nil {
		t.Fatalf("Run against real Postgres failed: %v", err)
	}

	var found *TableMeta
	for i := range md.Tables {
		if md.Tables[i].Schema == schema && md.Tables[i].Name == "orders" {
			found = &md.Tables[i]
		}
	}
	if found == nil {
		t.Fatalf("expected table %s.orders in payload; tables=%+v", schema, md.Tables)
	}

	if found.RowEstimate == -1 {
		t.Fatalf("RowEstimate leaked Postgres's reltuples=-1 'never analyzed' sentinel: %+v", found)
	}
	if found.RowEstimate < 0 {
		t.Errorf("RowEstimate = %d, want a non-negative estimate (n_live_tup fallback): %+v", found.RowEstimate, found)
	}
	if found.Analyzed {
		t.Error("Analyzed = true for a table that was never ANALYZEd; want false so a caller can tell this is a live-tuple fallback, not a planner statistic")
	}
	// The never-analyzed state has to be ABSENT on the wire, not a zero time: a
	// zero time.Time marshals as year 1 and would read as a real, extremely old
	// computation rather than as "nothing ever computed this". Absent is what
	// lets the consumer decline to present a number at all.
	if found.AnalyzedAt != nil {
		t.Errorf("AnalyzedAt = %s for a table nothing has ever analyzed; want nil so the field is omitted from the payload", found.AnalyzedAt)
	}
}

func seedNeverAnalyzedTable(ctx context.Context, t *testing.T, dsn, schema string) {
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
		`DROP SCHEMA IF EXISTS ` + schema + ` CASCADE`,
		`CREATE SCHEMA ` + schema,
		`CREATE TABLE ` + schema + `.orders (id bigint PRIMARY KEY, total numeric)`,
		`INSERT INTO ` + schema + `.orders SELECT g, g * 10 FROM generate_series(1,12) g`,
		// Deliberately NO ANALYZE: pg_class.reltuples stays at Postgres's -1
		// "never analyzed" sentinel for this table, which is the case under test.
	}
	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			_ = db.Close()
			t.Fatalf("seed statement failed (%.60q): %v", s, err)
		}
	}
	t.Cleanup(func() {
		cctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = db.ExecContext(cctx, `DROP SCHEMA IF EXISTS `+schema+` CASCADE`)
		_ = db.Close()
	})
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

// TestTableMetaWireShapeForTheAnalyzeClock pins the JSON contract for the second
// clock, which is the half a database cannot prove.
//
// `omitempty` on a nil pointer is what makes "never analyzed" arrive as an
// ABSENT field. Drop it and the zero time.Time marshals as year 1 — a real-
// looking timestamp for a computation that never happened, on the field a
// consumer uses to decide whether it may show a row count at all.
func TestTableMetaWireShapeForTheAnalyzeClock(t *testing.T) {
	neverAnalyzed, err := json.Marshal(TableMeta{
		Schema: "public", Name: "orders", RowEstimate: 42000, Analyzed: true,
		TotalBytes: 8192, ColumnCount: 9,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(neverAnalyzed), "analyzed_at") {
		t.Fatalf("a never-analyzed table must omit analyzed_at entirely: %s", neverAnalyzed)
	}

	at := time.Date(2026, 6, 26, 11, 0, 0, 0, time.UTC)
	analyzed, err := json.Marshal(TableMeta{
		Schema: "public", Name: "orders", RowEstimate: 42000, Analyzed: true,
		AnalyzedAt: &at, TotalBytes: 8192, ColumnCount: 9,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(analyzed), `"analyzed_at":"2026-06-26T11:00:00Z"`) {
		t.Fatalf("analyzed_at must be sent as an RFC3339 timestamp: %s", analyzed)
	}
}
