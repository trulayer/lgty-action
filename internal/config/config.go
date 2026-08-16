// Package config loads the action's runtime configuration from the environment.
//
// The binary has two subcommands — metadata (the original, default behavior)
// and renders — and they are configured, and validated, separately.
// A DSN typo must never block a renders run, and a missing renders-dir must
// never block a metadata run: sharing one Config/Load would couple two
// independently-auditable promises (see action/README.md) at the one place a
// customer is least likely to read closely.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// MetadataConfig is the resolved runtime configuration for the metadata
// subcommand (the tier-2 read-only database uploader).
type MetadataConfig struct {
	BackendURL   string // LGTY ingest base URL
	DBKind       string // database engine; only "postgres" is supported for now
	DBDSN        string // read-only Postgres DSN (use a scoped read-only role)
	Repo         string // owner/name of the repo being onboarded
	Workspace    string // LGTY workspace identifier
	DryRun       bool   // if true, print the payload instead of sending it
	OIDCAudience string // audience requested for the OIDC token; MUST match the backend's expected `aud`
}

// LoadMetadata reads the metadata subcommand's configuration from LGTY_*
// environment variables (which the GitHub Action maps from its inputs) and
// validates it.
func LoadMetadata() (MetadataConfig, error) {
	rawDSN := os.Getenv("LGTY_DB_DSN")
	c := MetadataConfig{
		BackendURL: env("LGTY_BACKEND_URL", "https://api.lgty.ai"),
		DBKind:     env("LGTY_DB_KIND", "postgres"),
		// Trimmed, like every other value read here. A CI secret store
		// routinely hands back a value carrying a leading or trailing
		// newline from however it was pasted in, and leading whitespace in
		// particular defeats the driver's "postgres://" prefix test — the
		// string is then parsed as libpq keyword/value pairs and rejected,
		// far from the whitespace that caused it.
		DBDSN:        strings.TrimSpace(rawDSN),
		Repo:         env("LGTY_REPO", os.Getenv("GITHUB_REPOSITORY")),
		Workspace:    os.Getenv("LGTY_WORKSPACE"),
		DryRun:       boolEnv("LGTY_DRY_RUN", false),
		OIDCAudience: env("LGTY_OIDC_AUDIENCE", "https://api.lgty.ai/ingest/metadata"),
	}
	if c.DBKind != "postgres" {
		return c, errors.New("only postgres is supported in this iteration")
	}
	if c.DBDSN == "" && !c.DryRun {
		if rawDSN != "" {
			// Set, but nothing survived trimming. Saying "is required" here
			// would assert the customer never set it, which is wrong and
			// sends them looking in the wrong place.
			return c, errors.New("LGTY_DB_DSN is set but contains only whitespace — set it to a Postgres connection string (or set LGTY_DRY_RUN=true)")
		}
		return c, errors.New("LGTY_DB_DSN is required (or set LGTY_DRY_RUN=true)")
	}
	// Parse here rather than letting the first query fail: pgx.ParseConfig is
	// exactly what the driver calls when the connection is opened, so this
	// rejects nothing the collector would have accepted — it only moves the
	// failure to the place that can name the input responsible, and replaces
	// a driver message that quotes the connection string back into the log.
	if c.DBDSN != "" {
		if _, err := pgx.ParseConfig(c.DBDSN); err != nil {
			return c, unparseableDSNError(c.DBDSN)
		}
	}
	return c, nil
}

// unparseableDSNError describes a rejected LGTY_DB_DSN without disclosing it.
// The connection string is a customer credential, so neither it, any substring
// of it, nor the driver's own error (which quotes the value) may appear in the
// message. What is reported is its length and whether it carried a URL scheme
// — enough to tell a truncated paste from a malformed URL, and not enough to
// reconstruct any part of the value.
func unparseableDSNError(dsn string) error {
	shape := "no postgres:// or postgresql:// scheme, so it was read as libpq keyword/value pairs"
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		shape = "a postgres:// scheme, so it was read as a URL"
	}
	return fmt.Errorf("LGTY_DB_DSN could not be parsed as a Postgres connection string: the value is %d characters long and has %s. "+
		"Expected either a URL (postgres://USER:PASSWORD@HOST:5432/DBNAME) or libpq keyword/value pairs (host=HOST user=USER dbname=DBNAME). "+
		"The value is a credential and is not echoed here — check the secret for surrounding quotes, an embedded newline, a truncated paste, or an unsupported connection parameter",
		len(dsn), shape)
}

// RendersConfig is the resolved runtime configuration for the renders
// subcommand (the CI-uploaded Visual Review capture uploader).
type RendersConfig struct {
	BackendURL string // LGTY ingest base URL (shared default with metadata; distinct path, distinct OIDC audience)
	RendersDir string // directory holding manifest.json and the PNG captures it names
	// CommitSHA overrides auto-detection. Leave empty in normal CI use: the
	// binary resolves the PR's actual head SHA itself (see internal/cienv),
	// which is NOT $GITHUB_SHA on a pull_request event — that env var is the
	// ephemeral merge commit, not the PR's own head.
	CommitSHA    string
	DryRun       bool   // if true, print the capture manifest instead of uploading; no OIDC token or network call is made
	OIDCAudience string // DISTINCT from the metadata audience by design — see docs/inputs-outputs.md
}

// LoadRenders reads the renders subcommand's configuration from LGTY_*
// environment variables and validates it.
func LoadRenders() (RendersConfig, error) {
	c := RendersConfig{
		BackendURL:   env("LGTY_BACKEND_URL", "https://api.lgty.ai"),
		RendersDir:   os.Getenv("LGTY_RENDERS_DIR"),
		CommitSHA:    os.Getenv("LGTY_COMMIT_SHA"),
		DryRun:       boolEnv("LGTY_DRY_RUN", false),
		OIDCAudience: env("LGTY_RENDERS_OIDC_AUDIENCE", "https://api.lgty.ai/renders"),
	}
	if c.RendersDir == "" {
		return c, errors.New("LGTY_RENDERS_DIR is required (set `renders-dir` in action.yml)")
	}
	return c, nil
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func boolEnv(key string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}
