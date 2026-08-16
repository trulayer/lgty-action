package config

import (
	"strings"
	"testing"
)

// isolateEnv clears every variable LoadMetadata reads so each case starts
// from a known blank slate regardless of the ambient CI environment.
func isolateEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"LGTY_BACKEND_URL", "LGTY_DB_KIND", "LGTY_DB_DSN", "LGTY_REPO",
		"LGTY_WORKSPACE", "LGTY_DRY_RUN", "LGTY_OIDC_AUDIENCE", "GITHUB_REPOSITORY",
	} {
		t.Setenv(k, "")
	}
}

// isolateRendersEnv clears every variable LoadRenders reads.
func isolateRendersEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"LGTY_BACKEND_URL", "LGTY_RENDERS_DIR", "LGTY_COMMIT_SHA",
		"LGTY_DRY_RUN", "LGTY_RENDERS_OIDC_AUDIENCE",
	} {
		t.Setenv(k, "")
	}
}

func TestLoadRenders_Defaults(t *testing.T) {
	isolateRendersEnv(t)
	t.Setenv("LGTY_RENDERS_DIR", "visual-review-captures")

	c, err := LoadRenders()
	if err != nil {
		t.Fatalf("LoadRenders() error = %v", err)
	}
	if c.BackendURL != "https://api.lgty.ai" {
		t.Errorf("BackendURL = %q, want default", c.BackendURL)
	}
	if c.RendersDir != "visual-review-captures" {
		t.Errorf("RendersDir = %q, want visual-review-captures", c.RendersDir)
	}
	if c.OIDCAudience != "https://api.lgty.ai/renders" {
		t.Errorf("OIDCAudience = %q, want the renders audience, DISTINCT from the metadata one", c.OIDCAudience)
	}
	if c.CommitSHA != "" {
		t.Errorf("CommitSHA = %q, want empty (auto-derive)", c.CommitSHA)
	}
	if c.DryRun {
		t.Error("DryRun = true, want false")
	}
}

func TestLoadRenders_RequiresRendersDir(t *testing.T) {
	isolateRendersEnv(t)
	// No renders-dir set at all — not even dry-run should silently proceed
	// with an implied directory, since guessing one risks uploading the
	// wrong images (or none) with no complaint.
	if _, err := LoadRenders(); err == nil {
		t.Fatal("expected error when LGTY_RENDERS_DIR is unset")
	}
}

func TestLoadRenders_AudienceDistinctFromMetadataDefault(t *testing.T) {
	isolateEnv(t)
	isolateRendersEnv(t)
	t.Setenv("LGTY_DRY_RUN", "true")
	t.Setenv("LGTY_RENDERS_DIR", "captures")

	meta, err := LoadMetadata()
	if err != nil {
		t.Fatalf("LoadMetadata() error = %v", err)
	}
	rend, err := LoadRenders()
	if err != nil {
		t.Fatalf("LoadRenders() error = %v", err)
	}
	if meta.OIDCAudience == rend.OIDCAudience {
		t.Fatalf("metadata and renders default OIDC audiences must be DISTINCT, both got %q", meta.OIDCAudience)
	}
}

func TestLoadRenders_Overrides(t *testing.T) {
	isolateRendersEnv(t)
	t.Setenv("LGTY_RENDERS_DIR", "captures")
	t.Setenv("LGTY_BACKEND_URL", "https://ingest.example.com")
	t.Setenv("LGTY_COMMIT_SHA", "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	t.Setenv("LGTY_RENDERS_OIDC_AUDIENCE", "custom-renders-audience")
	t.Setenv("LGTY_DRY_RUN", "true")

	c, err := LoadRenders()
	if err != nil {
		t.Fatalf("LoadRenders() error = %v", err)
	}
	if c.BackendURL != "https://ingest.example.com" {
		t.Errorf("BackendURL = %q, want override", c.BackendURL)
	}
	if c.CommitSHA != "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef" {
		t.Errorf("CommitSHA = %q, want override", c.CommitSHA)
	}
	if c.OIDCAudience != "custom-renders-audience" {
		t.Errorf("OIDCAudience = %q, want override", c.OIDCAudience)
	}
	if !c.DryRun {
		t.Error("DryRun = false, want true")
	}
}

func TestLoadMetadata_Defaults(t *testing.T) {
	isolateEnv(t)
	t.Setenv("LGTY_DRY_RUN", "true") // avoids the DSN requirement

	c, err := LoadMetadata()
	if err != nil {
		t.Fatalf("LoadMetadata() error = %v", err)
	}
	if c.BackendURL != "https://api.lgty.ai" {
		t.Errorf("BackendURL = %q, want default", c.BackendURL)
	}
	if c.DBKind != "postgres" {
		t.Errorf("DBKind = %q, want postgres", c.DBKind)
	}
	if c.OIDCAudience != "https://api.lgty.ai/ingest/metadata" {
		t.Errorf("OIDCAudience = %q, want the default ingest audience", c.OIDCAudience)
	}
	if !c.DryRun {
		t.Error("DryRun = false, want true")
	}
	if c.Repo != "" {
		t.Errorf("Repo = %q, want empty", c.Repo)
	}
}

func TestLoadMetadata_RequiresDSNWhenNotDryRun(t *testing.T) {
	isolateEnv(t)
	// No DSN, no dry-run -> must error rather than send nothing silently.
	if _, err := LoadMetadata(); err == nil {
		t.Fatal("expected error when LGTY_DB_DSN is unset and not dry-run")
	}
}

func TestLoadMetadata_DSNSatisfiesNonDryRun(t *testing.T) {
	isolateEnv(t)
	t.Setenv("LGTY_DB_DSN", "postgres://ro@localhost:5432/app")

	c, err := LoadMetadata()
	if err != nil {
		t.Fatalf("LoadMetadata() error = %v", err)
	}
	if c.DBDSN != "postgres://ro@localhost:5432/app" {
		t.Errorf("DBDSN = %q, want the provided DSN", c.DBDSN)
	}
	if c.DryRun {
		t.Error("DryRun = true, want false")
	}
}

// A CI secret store commonly returns the value with a newline or space
// attached to one end. Leading whitespace is the damaging case: it defeats the
// driver's "postgres://" prefix test, the string is parsed as libpq
// keyword/value pairs instead of a URL, and the run dies mid-collection.
func TestLoadMetadata_TrimsDSNWhitespace(t *testing.T) {
	const want = "postgres://ro@localhost:5432/app"
	for name, raw := range map[string]string{
		"leading newline":  "\n" + want,
		"trailing newline": want + "\n",
		"leading space":    " " + want,
		"surrounding":      "\n  " + want + "  \n",
	} {
		t.Run(name, func(t *testing.T) {
			isolateEnv(t)
			t.Setenv("LGTY_DB_DSN", raw)

			c, err := LoadMetadata()
			if err != nil {
				t.Fatalf("LoadMetadata() error = %v", err)
			}
			if c.DBDSN != want {
				t.Errorf("DBDSN = %q, want %q", c.DBDSN, want)
			}
		})
	}
}

// A DSN that is whitespace all the way through was set by someone — reporting
// it as missing would point them at the wrong problem.
func TestLoadMetadata_RejectsWhitespaceOnlyDSN(t *testing.T) {
	isolateEnv(t)
	t.Setenv("LGTY_DB_DSN", "  \n ")

	_, err := LoadMetadata()
	if err == nil {
		t.Fatal("expected an error for a whitespace-only DSN outside dry-run")
	}
	if !strings.Contains(err.Error(), "only whitespace") {
		t.Errorf("error = %q, want it to say the value was only whitespace rather than missing", err)
	}
}

// The failure a customer meets on their first run has to name the input, say
// what was expected, and never put the credential in their CI log.
func TestLoadMetadata_RejectsUnparseableDSN(t *testing.T) {
	tests := map[string]struct {
		dsn       string
		wantShape string
	}{
		"not a connection string at all": {dsn: "hunter2", wantShape: "no postgres:// or postgresql:// scheme"},
		"quoted by the shell":            {dsn: `"postgres://ro@localhost:5432/app"`, wantShape: "no postgres:// or postgresql:// scheme"},
		"scheme present, URL malformed":  {dsn: "postgres://ro@local host:5432/app", wantShape: "a postgres:// scheme"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			isolateEnv(t)
			t.Setenv("LGTY_DB_DSN", tc.dsn)

			_, err := LoadMetadata()
			if err == nil {
				t.Fatalf("expected an error for DSN %q", tc.dsn)
			}
			msg := err.Error()
			if !strings.Contains(msg, "LGTY_DB_DSN could not be parsed as a Postgres connection string") {
				t.Errorf("error = %q, want it to name the input and the problem", msg)
			}
			if !strings.Contains(msg, tc.wantShape) {
				t.Errorf("error = %q, want it to report %q", msg, tc.wantShape)
			}
			if !strings.Contains(msg, "postgres://USER:PASSWORD@HOST:5432/DBNAME") {
				t.Errorf("error = %q, want it to state the expected form", msg)
			}
			if strings.Contains(msg, tc.dsn) {
				t.Errorf("error = %q leaks the DSN, which is a credential", msg)
			}
		})
	}
}

// The DSN is a credential and no part of it may reach a CI log. The driver's
// own message redacts the password but still quotes the role, host, port and
// database back — which is why the driver error is discarded here rather than
// wrapped.
func TestLoadMetadata_UnparseableDSNErrorLeaksNoSubstring(t *testing.T) {
	isolateEnv(t)
	const dsn = "postgres://svc_ro:hunter2@db.internal.example:5432/orders\nbogus"
	t.Setenv("LGTY_DB_DSN", dsn)

	_, err := LoadMetadata()
	if err == nil {
		t.Fatal("expected an error for an unparseable DSN")
	}
	msg := err.Error()
	for _, secret := range []string{"svc_ro", "hunter2", "db.internal.example", "orders"} {
		if strings.Contains(msg, secret) {
			t.Errorf("error = %q contains %q from the DSN", msg, secret)
		}
	}
}

func TestLoadMetadata_RejectsNonPostgres(t *testing.T) {
	isolateEnv(t)
	t.Setenv("LGTY_DB_KIND", "mysql")
	t.Setenv("LGTY_DRY_RUN", "true")

	if _, err := LoadMetadata(); err == nil {
		t.Fatal("expected error for non-postgres DB kind")
	}
}

func TestLoadMetadata_RepoFallsBackToGitHubRepository(t *testing.T) {
	isolateEnv(t)
	t.Setenv("LGTY_DRY_RUN", "true")
	t.Setenv("GITHUB_REPOSITORY", "trulayer/kindscan-backend")

	c, err := LoadMetadata()
	if err != nil {
		t.Fatalf("LoadMetadata() error = %v", err)
	}
	if c.Repo != "trulayer/kindscan-backend" {
		t.Errorf("Repo = %q, want GITHUB_REPOSITORY fallback", c.Repo)
	}
}

func TestLoadMetadata_ExplicitRepoOverridesGitHubRepository(t *testing.T) {
	isolateEnv(t)
	t.Setenv("LGTY_DRY_RUN", "true")
	t.Setenv("LGTY_REPO", "acme/service")
	t.Setenv("GITHUB_REPOSITORY", "trulayer/kindscan-backend")

	c, err := LoadMetadata()
	if err != nil {
		t.Fatalf("LoadMetadata() error = %v", err)
	}
	if c.Repo != "acme/service" {
		t.Errorf("Repo = %q, want LGTY_REPO to win", c.Repo)
	}
}

func TestLoadMetadata_Overrides(t *testing.T) {
	isolateEnv(t)
	t.Setenv("LGTY_DRY_RUN", "true")
	t.Setenv("LGTY_BACKEND_URL", "https://ingest.example.com")
	t.Setenv("LGTY_OIDC_AUDIENCE", "custom-audience")
	t.Setenv("LGTY_WORKSPACE", "ws_123")

	c, err := LoadMetadata()
	if err != nil {
		t.Fatalf("LoadMetadata() error = %v", err)
	}
	if c.BackendURL != "https://ingest.example.com" {
		t.Errorf("BackendURL = %q, want override", c.BackendURL)
	}
	if c.OIDCAudience != "custom-audience" {
		t.Errorf("OIDCAudience = %q, want override", c.OIDCAudience)
	}
	if c.Workspace != "ws_123" {
		t.Errorf("Workspace = %q, want ws_123", c.Workspace)
	}
}

func TestLoadMetadata_DryRunParsing(t *testing.T) {
	tests := []struct {
		val  string
		want bool
	}{
		{"true", true},
		{"1", true},
		{"TRUE", true},
		{"false", false},
		{"0", false},
		{"not-a-bool", false}, // invalid -> default (false)
		{"", false},           // unset -> default (false)
	}
	for _, tt := range tests {
		t.Run(tt.val, func(t *testing.T) {
			isolateEnv(t)
			// Provide a DSN so a false parse doesn't trip the DSN requirement.
			t.Setenv("LGTY_DB_DSN", "postgres://ro@localhost/app")
			if tt.val != "" {
				t.Setenv("LGTY_DRY_RUN", tt.val)
			}
			c, err := LoadMetadata()
			if err != nil {
				t.Fatalf("LoadMetadata() error = %v", err)
			}
			if c.DryRun != tt.want {
				t.Errorf("DryRun for %q = %v, want %v", tt.val, c.DryRun, tt.want)
			}
		})
	}
}
