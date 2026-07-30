package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
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
		t.Skip("set LGTY_TEST_DB_DSN to run the full dry-run integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	seedFullRunDB(ctx, t, dsn)

	t.Setenv("LGTY_DB_DSN", dsn)
	t.Setenv("LGTY_DRY_RUN", "true")
	t.Setenv("LGTY_REPO", "acme/widgets")
	t.Setenv("LGTY_WORKSPACE", "workspace-test")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", "")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "")

	var output bytes.Buffer
	if err := run(ctx, &output); err != nil {
		t.Fatalf("run dry-run against Postgres: %v", err)
	}
	if strings.Contains(output.String(), fullRunSecret) {
		t.Fatal("raw row value leaked into dry-run payload")
	}

	var md collect.Metadata
	if err := json.Unmarshal(output.Bytes(), &md); err != nil {
		t.Fatalf("decode printed metadata: %v\n%s", err, output.String())
	}
	if md.Workspace != "workspace-test" || md.Repo != "acme/widgets" {
		t.Fatalf("identity = %q/%q", md.Workspace, md.Repo)
	}
	for _, table := range md.Tables {
		if table.Schema == fullRunSchema && table.Name == "customers" {
			if table.RowEstimate < 1 || table.TotalBytes < 1 || table.ColumnCount != 2 {
				t.Fatalf("customers metadata = %+v", table)
			}
			return
		}
	}
	t.Fatalf("missing %s.customers metadata: %+v", fullRunSchema, md.Tables)
}

const (
	fullRunSchema = "lgty_action_full_run_test"
	fullRunSecret = "raw-customer-value-must-never-leave"
)

func seedFullRunDB(ctx context.Context, t *testing.T, dsn string) {
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
		`DROP SCHEMA IF EXISTS ` + fullRunSchema + ` CASCADE`,
		`CREATE SCHEMA ` + fullRunSchema,
		`CREATE TABLE ` + fullRunSchema + `.customers (id bigint PRIMARY KEY, private_value text)`,
		`INSERT INTO ` + fullRunSchema + `.customers VALUES (1, '` + fullRunSecret + `')`,
		`ANALYZE ` + fullRunSchema + `.customers`,
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
		_, _ = db.ExecContext(cleanupCtx, `DROP SCHEMA IF EXISTS `+fullRunSchema+` CASCADE`)
		_ = db.Close()
	})
}
