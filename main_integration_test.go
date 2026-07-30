package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/trulayer/lgty-action/internal/collect"
)

// TestRun_DryRunWithRealPostgres closes LGT-34's orchestration-level
// integration requirement: config -> OIDC's honest dry-run degradation ->
// guarded pgx collection -> the exact JSON payload printed instead of sent.
func TestRun_DryRunWithRealPostgres(t *testing.T) {
	dsn := os.Getenv("LGTY_TEST_DB_DSN")
	if dsn == "" {
		if os.Getenv("CI") == "true" {
			t.Fatal("LGTY_TEST_DB_DSN must be set in CI; full-run integration coverage may not skip")
		}
		t.Skip("set LGTY_TEST_DB_DSN to run the full dry-run integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	schema := fmt.Sprintf("lgty_action_full_run_test_%d", time.Now().UnixNano())
	peerSchema := schema + "_peer"
	seedFullRunDB(ctx, t, dsn, schema, peerSchema)

	t.Setenv("LGTY_DB_DSN", dsn)
	t.Setenv("LGTY_DRY_RUN", "true")
	t.Setenv("LGTY_REPO", "acme/widgets")
	t.Setenv("LGTY_WORKSPACE", "workspace-test")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", "")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "")

	var output bytes.Buffer
	collectedAt := time.Date(2026, time.July, 30, 12, 34, 56, 0, time.FixedZone("test", -7*60*60))
	if err := run(ctx, &output, func() time.Time { return collectedAt }); err != nil {
		t.Fatalf("run dry-run against Postgres: %v", err)
	}
	var repeated bytes.Buffer
	if err := run(ctx, &repeated, func() time.Time { return collectedAt }); err != nil {
		t.Fatalf("repeat dry-run against Postgres: %v", err)
	}
	if !bytes.Equal(output.Bytes(), repeated.Bytes()) {
		t.Fatalf("same database + clock produced different JSON\nfirst:\n%s\nsecond:\n%s",
			output.String(), repeated.String())
	}
	if strings.Contains(output.String(), fullRunSecret) {
		t.Fatal("raw row value leaked into dry-run payload")
	}

	var shape map[string]json.RawMessage
	if err := json.Unmarshal(output.Bytes(), &shape); err != nil {
		t.Fatalf("decode printed payload shape: %v", err)
	}
	gotKeys := make([]string, 0, len(shape))
	for key := range shape {
		gotKeys = append(gotKeys, key)
	}
	sort.Strings(gotKeys)
	wantKeys := []string{"collected_at", "dependencies", "repo", "tables", "workspace"}
	if strings.Join(gotKeys, ",") != strings.Join(wantKeys, ",") {
		t.Fatalf("payload keys = %v, want %v", gotKeys, wantKeys)
	}

	var md collect.Metadata
	if err := json.Unmarshal(output.Bytes(), &md); err != nil {
		t.Fatalf("decode printed metadata: %v\n%s", err, output.String())
	}
	if md.Workspace != "workspace-test" || md.Repo != "acme/widgets" {
		t.Fatalf("identity = %q/%q", md.Workspace, md.Repo)
	}
	if want := collectedAt.UTC(); !md.CollectedAt.Equal(want) {
		t.Fatalf("collected_at = %s, want %s", md.CollectedAt, want)
	}
	foundTable := false
	for _, table := range md.Tables {
		if table.Schema == schema && table.Name == "customers" {
			if table.RowEstimate < 1 || table.TotalBytes < 1 || table.ColumnCount != 2 {
				t.Fatalf("customers metadata = %+v", table)
			}
			foundTable = true
			break
		}
	}
	if !foundTable {
		t.Fatalf("missing %s.customers metadata: %+v", schema, md.Tables)
	}
	relevantDeps := make([]collect.DepEdge, 0, 2)
	for _, dep := range md.Deps {
		if dep.FromSchema == schema || dep.FromSchema == peerSchema {
			relevantDeps = append(relevantDeps, dep)
		}
	}
	wantDeps := []collect.DepEdge{
		{FromSchema: schema, FromTable: "orders", ToSchema: schema, ToTable: "customers"},
		{FromSchema: peerSchema, FromTable: "orders", ToSchema: peerSchema, ToTable: "customers"},
	}
	if !equalDeps(relevantDeps, wantDeps) {
		t.Fatalf("dependency set = %+v, want exactly %+v; all=%+v", relevantDeps, wantDeps, md.Deps)
	}
}

const fullRunSecret = "raw-customer-value-must-never-leave"

func seedFullRunDB(ctx context.Context, t *testing.T, dsn, schema, peerSchema string) {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		t.Fatalf("ping Postgres: %v", err)
	}
	statements := []string{
		`CREATE SCHEMA ` + schema,
		`CREATE TABLE ` + schema + `.customers (id bigint PRIMARY KEY, private_value text)`,
		`CREATE TABLE ` + schema + `.orders (id bigint PRIMARY KEY, customer_id bigint REFERENCES ` + schema + `.customers(id))`,
		`INSERT INTO ` + schema + `.customers VALUES (1, '` + fullRunSecret + `')`,
		`INSERT INTO ` + schema + `.orders VALUES (1, 1)`,
		`ANALYZE ` + schema + `.customers`,
		`ANALYZE ` + schema + `.orders`,
		`CREATE SCHEMA ` + peerSchema,
		`CREATE TABLE ` + peerSchema + `.customers (id bigint PRIMARY KEY, private_value text)`,
		`CREATE TABLE ` + peerSchema + `.orders (id bigint PRIMARY KEY, customer_id bigint REFERENCES ` + peerSchema + `.customers(id))`,
		`INSERT INTO ` + peerSchema + `.customers VALUES (1, 'peer-private-value')`,
		`INSERT INTO ` + peerSchema + `.orders VALUES (1, 1)`,
		`ANALYZE ` + peerSchema + `.customers`,
		`ANALYZE ` + peerSchema + `.orders`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			_ = db.Close()
			t.Fatalf("seed statement %.60q: %v", statement, err)
		}
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = db.ExecContext(cleanupCtx, `DROP SCHEMA IF EXISTS `+schema+` CASCADE`)
		_, _ = db.ExecContext(cleanupCtx, `DROP SCHEMA IF EXISTS `+peerSchema+` CASCADE`)
		_ = db.Close()
	})
}

func equalDeps(got, want []collect.DepEdge) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
