// Command lgty-action is LGTY's tier-2 CI uploader.
//
// It runs inside the CUSTOMER's CI (not LGTY's infrastructure), authenticates
// with a short-lived OIDC token (no long-lived secret), and sends the LGTY
// backend read-only database METADATA — table names, row-count ESTIMATES,
// sizes, and foreign-key dependency edges. It NEVER reads or transmits row
// data. The metadata-only guarantee is enforced in internal/collect/guard.go;
// read it — that is the whole point of this being a public repo.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/trulayer/lgty-action/internal/collect"
	"github.com/trulayer/lgty-action/internal/config"
	"github.com/trulayer/lgty-action/internal/ingest"
	"github.com/trulayer/lgty-action/internal/oidc"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("lgty-action: ")
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	log.Printf("workspace=%s repo=%s db=%s dry_run=%t", cfg.Workspace, cfg.Repo, cfg.DBKind, cfg.DryRun)
	log.Print("this action emits METADATA ONLY (table names, row-count ESTIMATES, sizes, dependency edges). It never reads or sends row data.")

	// 1. Short-lived OIDC identity from the CI provider — no long-lived credential.
	token, err := oidc.FetchToken(ctx, cfg.OIDCAudience)
	if err != nil {
		if !cfg.DryRun {
			return fmt.Errorf("oidc: %w", err)
		}
		log.Printf("OIDC unavailable (%v) — continuing in dry-run", err)
	}

	// 2 + 3. Collect metadata only. Every query is passed through the guard.
	md, err := collect.Run(ctx, cfg.DBDSN)
	if err != nil {
		return fmt.Errorf("collect: %w", err)
	}
	md.CollectedAt = time.Now().UTC()
	md.Repo = cfg.Repo
	md.Workspace = cfg.Workspace

	// 4. Ship it (or print it, in dry-run, so the customer can see exactly what leaves).
	if cfg.DryRun {
		return ingest.Print(os.Stdout, md)
	}
	return ingest.Send(ctx, cfg.BackendURL, token, md)
}
