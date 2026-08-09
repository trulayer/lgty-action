package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"reflect"
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
	adminDSN := os.Getenv("LGTY_TEST_DB_DSN")
	if adminDSN == "" {
		if os.Getenv("CI") == "true" {
			t.Fatal("LGTY_TEST_DB_DSN must be set in CI; full-run integration coverage may not skip")
		}
		t.Skip("set LGTY_TEST_DB_DSN to run the full dry-run integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	dsn := createFullRunDatabase(ctx, t, adminDSN)
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
	if err := runMetadata(ctx, &output, func() time.Time { return collectedAt }); err != nil {
		t.Fatalf("run dry-run against Postgres: %v", err)
	}
	var repeated bytes.Buffer
	if err := runMetadata(ctx, &repeated, func() time.Time { return collectedAt }); err != nil {
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
	want := collect.Metadata{
		Workspace:   "workspace-test",
		Repo:        "acme/widgets",
		CollectedAt: collectedAt.UTC(),
		Tables: []collect.TableMeta{
			wantTable(ctx, t, dsn, schema, "composite_child", 3),
			wantTable(ctx, t, dsn, schema, "composite_parent", 2),
			wantTable(ctx, t, dsn, schema, "customers", 2),
			wantTable(ctx, t, dsn, schema, "orders", 2),
			wantTable(ctx, t, dsn, peerSchema, "composite_child", 3),
			wantTable(ctx, t, dsn, peerSchema, "composite_parent", 2),
			wantTable(ctx, t, dsn, peerSchema, "customers", 2),
			wantTable(ctx, t, dsn, peerSchema, "orders", 2),
		},
		Deps: []collect.DepEdge{
			{FromSchema: schema, FromTable: "composite_child", ToSchema: schema, ToTable: "composite_parent"},
			{FromSchema: schema, FromTable: "orders", ToSchema: schema, ToTable: "customers"},
			{FromSchema: peerSchema, FromTable: "composite_child", ToSchema: peerSchema, ToTable: "composite_parent"},
			{FromSchema: peerSchema, FromTable: "orders", ToSchema: peerSchema, ToTable: "customers"},
		},
	}
	if !reflect.DeepEqual(md, want) {
		t.Fatalf("payload mismatch\n got: %+v\nwant: %+v", md, want)
	}
	wantJSON, err := json.MarshalIndent(want, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	wantJSON = append(wantJSON, '\n')
	if !bytes.Equal(output.Bytes(), wantJSON) {
		t.Fatalf("serialized JSON mismatch\n got:\n%s\nwant:\n%s", output.Bytes(), wantJSON)
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
		`CREATE TABLE ` + schema + `.composite_parent (a bigint, b bigint, PRIMARY KEY (a, b))`,
		`CREATE TABLE ` + schema + `.composite_child (id bigint PRIMARY KEY, x bigint, y bigint, FOREIGN KEY (x, y) REFERENCES ` + schema + `.composite_parent(a, b))`,
		`INSERT INTO ` + schema + `.customers VALUES (1, '` + fullRunSecret + `')`,
		`INSERT INTO ` + schema + `.orders VALUES (1, 1)`,
		`INSERT INTO ` + schema + `.composite_parent VALUES (1, 2)`,
		`INSERT INTO ` + schema + `.composite_child VALUES (1, 1, 2)`,
		`ANALYZE ` + schema + `.customers`,
		`ANALYZE ` + schema + `.orders`,
		`ANALYZE ` + schema + `.composite_parent`,
		`ANALYZE ` + schema + `.composite_child`,
		`CREATE SCHEMA ` + peerSchema,
		`CREATE TABLE ` + peerSchema + `.customers (id bigint PRIMARY KEY, private_value text)`,
		`CREATE TABLE ` + peerSchema + `.orders (id bigint PRIMARY KEY, customer_id bigint REFERENCES ` + peerSchema + `.customers(id))`,
		`CREATE TABLE ` + peerSchema + `.composite_parent (a bigint, b bigint, PRIMARY KEY (a, b))`,
		`CREATE TABLE ` + peerSchema + `.composite_child (id bigint PRIMARY KEY, x bigint, y bigint, FOREIGN KEY (x, y) REFERENCES ` + peerSchema + `.composite_parent(a, b))`,
		`INSERT INTO ` + peerSchema + `.customers VALUES (1, 'peer-private-value')`,
		`INSERT INTO ` + peerSchema + `.orders VALUES (1, 1)`,
		`INSERT INTO ` + peerSchema + `.composite_parent VALUES (1, 2)`,
		`INSERT INTO ` + peerSchema + `.composite_child VALUES (1, 1, 2)`,
		`ANALYZE ` + peerSchema + `.customers`,
		`ANALYZE ` + peerSchema + `.orders`,
		`ANALYZE ` + peerSchema + `.composite_parent`,
		`ANALYZE ` + peerSchema + `.composite_child`,
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

func createFullRunDatabase(ctx context.Context, t *testing.T, adminDSN string) string {
	t.Helper()
	parsed, err := url.Parse(adminDSN)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		t.Fatalf("LGTY_TEST_DB_DSN must be a postgres URL: %v", err)
	}
	name := fmt.Sprintf("lgty_action_full_run_%d", time.Now().UnixNano())
	admin, err := sql.Open("pgx", adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecContext(ctx, `CREATE DATABASE `+name); err != nil {
		_ = admin.Close()
		t.Fatalf("create isolated database: %v", err)
	}
	parsed.Path = "/" + name
	isolatedDSN := parsed.String()
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = admin.ExecContext(cleanupCtx, `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`)
		_ = admin.Close()
	})
	return isolatedDSN
}

func wantTable(ctx context.Context, t *testing.T, dsn, schema, table string, columns int) collect.TableMeta {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var size int64
	if err := db.QueryRowContext(ctx, `SELECT pg_total_relation_size($1::regclass)`,
		schema+"."+table).Scan(&size); err != nil {
		t.Fatalf("relation size %s.%s: %v", schema, table, err)
	}
	return collect.TableMeta{
		Schema: schema, Name: table, RowEstimate: 1, Analyzed: true, TotalBytes: size, ColumnCount: columns,
	}
}
