// Package config loads the action's runtime configuration from the environment.
//
// The binary has two subcommands — metadata (the original, default behavior)
// and renders (LGT-404) — and they are configured, and validated, separately.
// A DSN typo must never block a renders run, and a missing renders-dir must
// never block a metadata run: sharing one Config/Load would couple two
// independently-auditable promises (see action/README.md) at the one place a
// customer is least likely to read closely.
package config

import (
	"errors"
	"os"
	"strconv"
	"strings"
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
	OIDCAudience string // audience requested for the OIDC token; MUST match the backend's expected `aud` (LGT-36 §1)
}

// LoadMetadata reads the metadata subcommand's configuration from LGTY_*
// environment variables (which the GitHub Action maps from its inputs) and
// validates it.
func LoadMetadata() (MetadataConfig, error) {
	c := MetadataConfig{
		BackendURL:   env("LGTY_BACKEND_URL", "https://api.lgty.ai"),
		DBKind:       env("LGTY_DB_KIND", "postgres"),
		DBDSN:        os.Getenv("LGTY_DB_DSN"),
		Repo:         env("LGTY_REPO", os.Getenv("GITHUB_REPOSITORY")),
		Workspace:    os.Getenv("LGTY_WORKSPACE"),
		DryRun:       boolEnv("LGTY_DRY_RUN", false),
		OIDCAudience: env("LGTY_OIDC_AUDIENCE", "https://api.lgty.ai/ingest/metadata"),
	}
	if c.DBKind != "postgres" {
		return c, errors.New("only postgres is supported in this iteration")
	}
	if c.DBDSN == "" && !c.DryRun {
		return c, errors.New("LGTY_DB_DSN is required (or set LGTY_DRY_RUN=true)")
	}
	return c, nil
}

// RendersConfig is the resolved runtime configuration for the renders
// subcommand (the CI-uploaded Visual Review capture uploader, LGT-404).
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
