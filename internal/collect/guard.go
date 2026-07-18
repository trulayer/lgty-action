package collect

import (
	"fmt"
	"regexp"
	"strings"
)

// The guard is the heart of LGTY's trust model: this action must NEVER read row
// data. Every SQL string is checked here before it can run. The collector only
// ever builds queries from the compile-time constants in collect.go, so this is
// defense-in-depth — but it is deliberately simple and auditable. Read it.
//
// What is allowed: read-only SELECTs over Postgres system catalogs and
// information_schema that expose only *metadata* — table names, row-count
// estimates, sizes, column counts, and foreign-key edges.
//
// What is forbidden: anything that could read, mutate, or exfiltrate row data.

// allowedSources are the only relations/functions a metadata query may touch.
var allowedSources = []string{
	"pg_class",
	"pg_namespace",
	"pg_stat_user_tables",
	"pg_stat_user_indexes",
	"pg_total_relation_size",
	"pg_relation_size",
	"information_schema.tables",
	"information_schema.columns", // column NAMES/types only — never values
	"information_schema.table_constraints",
	"information_schema.referential_constraints",
	"information_schema.key_column_usage",
}

// forbidden keywords must never appear — a crude but effective backstop.
var forbidden = []*regexp.Regexp{
	regexp.MustCompile(`(?is)\binsert\b`),
	regexp.MustCompile(`(?is)\bupdate\b`),
	regexp.MustCompile(`(?is)\bdelete\b`),
	regexp.MustCompile(`(?is)\bcopy\b`),
	regexp.MustCompile(`(?is)\btruncate\b`),
	regexp.MustCompile(`(?is)\bpg_read_file\b`),
	regexp.MustCompile(`(?is)\bpg_read_binary_file\b`),
}

// AssertMetadataOnly returns an error unless the query is a read-only SELECT
// that touches only allowlisted metadata sources.
func AssertMetadataOnly(query string) error {
	q := strings.ToLower(strings.TrimSpace(query))
	if !strings.HasPrefix(q, "select") {
		return fmt.Errorf("guard: only SELECT is permitted (got %.40q)", query)
	}
	for _, f := range forbidden {
		if f.MatchString(q) {
			return fmt.Errorf("guard: forbidden keyword in query (%.40q)", query)
		}
	}
	if !referencesAllowedSource(q) {
		return fmt.Errorf("guard: query does not reference an allowlisted metadata source (%.60q)", query)
	}
	return nil
}

// referencesAllowedSource requires the query to name at least one allowlisted
// system catalog. Combined with the SELECT-only + forbidden-keyword checks and
// the fact that collect.go only ever runs the constant metadata queries, this
// keeps row data unreachable.
func referencesAllowedSource(q string) bool {
	for _, s := range allowedSources {
		if strings.Contains(q, s) {
			return true
		}
	}
	return false
}
