// Package config loads the action's runtime configuration from the environment.
package config

import (
	"errors"
	"os"
	"strconv"
	"strings"
)

// Config is the resolved runtime configuration.
type Config struct {
	BackendURL   string // LGTY ingest base URL
	DBKind       string // database engine; only "postgres" is supported for now
	DBDSN        string // read-only Postgres DSN (use a scoped read-only role)
	Repo         string // owner/name of the repo being onboarded
	Workspace    string // LGTY workspace identifier
	DryRun       bool   // if true, print the payload instead of sending it
	OIDCAudience string // audience claim requested for the OIDC token
}

// Load reads configuration from LGTY_* environment variables (which the GitHub
// Action maps from its inputs) and validates it.
func Load() (Config, error) {
	c := Config{
		BackendURL:   env("LGTY_BACKEND_URL", "https://api.lgty.ai"),
		DBKind:       env("LGTY_DB_KIND", "postgres"),
		DBDSN:        os.Getenv("LGTY_DB_DSN"),
		Repo:         env("LGTY_REPO", os.Getenv("GITHUB_REPOSITORY")),
		Workspace:    os.Getenv("LGTY_WORKSPACE"),
		DryRun:       boolEnv("LGTY_DRY_RUN", false),
		OIDCAudience: env("LGTY_OIDC_AUDIENCE", "lgty"),
	}
	if c.DBKind != "postgres" {
		return c, errors.New("only postgres is supported in this iteration")
	}
	if c.DBDSN == "" && !c.DryRun {
		return c, errors.New("LGTY_DB_DSN is required (or set LGTY_DRY_RUN=true)")
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
