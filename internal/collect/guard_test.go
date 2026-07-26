package collect

import "testing"

// The guard is the trust-critical code: it must accept exactly the fixed
// metadata query set and reject anything that could read, mutate, or exfiltrate
// row data. These tests are the executable proof of that claim.

func TestAssertMetadataOnly_AcceptsTheFixedQuerySet(t *testing.T) {
	// Every query the action can ever run must clear the guard.
	for i, q := range metadataQueries {
		if err := AssertMetadataOnly(q); err != nil {
			t.Errorf("metadataQueries[%d] must be allowed, got: %v", i, err)
		}
	}
	if len(metadataQueries) != 3 {
		t.Fatalf("expected the fixed set to be exactly 3 queries, got %d", len(metadataQueries))
	}
}

func TestAssertMetadataOnly(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		wantErr bool
	}{
		// --- accepted: read-only SELECTs over allowlisted metadata sources ---
		{"row estimates", qRowEstimates, false},
		{"column counts", qColumnCounts, false},
		{"dependencies", qDependencies, false},
		{"simple pg_class select", "SELECT relname FROM pg_class", false},
		{"leading whitespace tolerated", "\n  SELECT relname FROM pg_class", false},
		{"case insensitive source + keyword", "select RELNAME from PG_CLASS", false},
		{"information_schema columns metadata", "SELECT table_name FROM information_schema.columns", false},

		// --- rejected: not a SELECT ---
		{"empty", "", true},
		{"insert", "INSERT INTO pg_class VALUES (1)", true},
		{"update", "UPDATE pg_class SET relname='x'", true},
		{"delete", "DELETE FROM pg_class", true},
		{"drop", "DROP TABLE customers", true},
		{"truncate stmt", "TRUNCATE customers", true},
		{"copy stmt", "COPY customers TO STDOUT", true},
		{"with-cte not select-prefixed", "WITH x AS (SELECT 1) SELECT relname FROM pg_class", true},

		// --- rejected: SELECT but touches no allowlisted metadata source ---
		{"select row data", "SELECT * FROM customers", true},
		{"select secrets table", "SELECT rolpassword FROM pg_authid", true},
		{"select constant", "SELECT 1", true},

		// --- rejected: forbidden keyword smuggled into a SELECT ---
		{"select then delete", "SELECT relname FROM pg_class; DELETE FROM customers", true},
		{"select then update", "SELECT relname FROM pg_class; UPDATE customers SET x=1", true},
		{"pg_read_file", "SELECT pg_read_file('/etc/passwd')", true},
		{"pg_read_binary_file", "SELECT pg_read_binary_file('/etc/passwd')", true},
		{"copy keyword inside select", "SELECT relname FROM pg_class WHERE relname='copy me'", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := AssertMetadataOnly(tt.query)
			if tt.wantErr && err == nil {
				t.Fatalf("expected rejection, got nil error for query %q", tt.query)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected acceptance, got error %v for query %q", err, tt.query)
			}
		})
	}
}
