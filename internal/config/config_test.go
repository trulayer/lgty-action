package config

import "testing"

// isolateEnv clears every variable Load reads so each case starts from a known
// blank slate regardless of the ambient CI environment.
func isolateEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"LGTY_BACKEND_URL", "LGTY_DB_KIND", "LGTY_DB_DSN", "LGTY_REPO",
		"LGTY_WORKSPACE", "LGTY_DRY_RUN", "LGTY_OIDC_AUDIENCE", "GITHUB_REPOSITORY",
	} {
		t.Setenv(k, "")
	}
}

func TestLoad_Defaults(t *testing.T) {
	isolateEnv(t)
	t.Setenv("LGTY_DRY_RUN", "true") // avoids the DSN requirement

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if c.BackendURL != "https://api.lgty.ai" {
		t.Errorf("BackendURL = %q, want default", c.BackendURL)
	}
	if c.DBKind != "postgres" {
		t.Errorf("DBKind = %q, want postgres", c.DBKind)
	}
	if c.OIDCAudience != "https://api.lgty.ai/ingest/metadata" {
		t.Errorf("OIDCAudience = %q, want the LGT-36 ingest URI", c.OIDCAudience)
	}
	if !c.DryRun {
		t.Error("DryRun = false, want true")
	}
	if c.Repo != "" {
		t.Errorf("Repo = %q, want empty", c.Repo)
	}
}

func TestLoad_RequiresDSNWhenNotDryRun(t *testing.T) {
	isolateEnv(t)
	// No DSN, no dry-run -> must error rather than send nothing silently.
	if _, err := Load(); err == nil {
		t.Fatal("expected error when LGTY_DB_DSN is unset and not dry-run")
	}
}

func TestLoad_DSNSatisfiesNonDryRun(t *testing.T) {
	isolateEnv(t)
	t.Setenv("LGTY_DB_DSN", "postgres://ro@localhost:5432/app")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if c.DBDSN != "postgres://ro@localhost:5432/app" {
		t.Errorf("DBDSN = %q, want the provided DSN", c.DBDSN)
	}
	if c.DryRun {
		t.Error("DryRun = true, want false")
	}
}

func TestLoad_RejectsNonPostgres(t *testing.T) {
	isolateEnv(t)
	t.Setenv("LGTY_DB_KIND", "mysql")
	t.Setenv("LGTY_DRY_RUN", "true")

	if _, err := Load(); err == nil {
		t.Fatal("expected error for non-postgres DB kind")
	}
}

func TestLoad_RepoFallsBackToGitHubRepository(t *testing.T) {
	isolateEnv(t)
	t.Setenv("LGTY_DRY_RUN", "true")
	t.Setenv("GITHUB_REPOSITORY", "trulayer/kindscan-backend")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if c.Repo != "trulayer/kindscan-backend" {
		t.Errorf("Repo = %q, want GITHUB_REPOSITORY fallback", c.Repo)
	}
}

func TestLoad_ExplicitRepoOverridesGitHubRepository(t *testing.T) {
	isolateEnv(t)
	t.Setenv("LGTY_DRY_RUN", "true")
	t.Setenv("LGTY_REPO", "acme/service")
	t.Setenv("GITHUB_REPOSITORY", "trulayer/kindscan-backend")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if c.Repo != "acme/service" {
		t.Errorf("Repo = %q, want LGTY_REPO to win", c.Repo)
	}
}

func TestLoad_Overrides(t *testing.T) {
	isolateEnv(t)
	t.Setenv("LGTY_DRY_RUN", "true")
	t.Setenv("LGTY_BACKEND_URL", "https://ingest.example.com")
	t.Setenv("LGTY_OIDC_AUDIENCE", "lgty-staging")
	t.Setenv("LGTY_WORKSPACE", "ws_123")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if c.BackendURL != "https://ingest.example.com" {
		t.Errorf("BackendURL = %q, want override", c.BackendURL)
	}
	if c.OIDCAudience != "lgty-staging" {
		t.Errorf("OIDCAudience = %q, want override", c.OIDCAudience)
	}
	if c.Workspace != "ws_123" {
		t.Errorf("Workspace = %q, want ws_123", c.Workspace)
	}
}

func TestLoad_DryRunParsing(t *testing.T) {
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
			c, err := Load()
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if c.DryRun != tt.want {
				t.Errorf("DryRun for %q = %v, want %v", tt.val, c.DryRun, tt.want)
			}
		})
	}
}
