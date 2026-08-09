// Command lgty-action is LGTY's tier-2 CI uploader. It runs inside the
// CUSTOMER's CI (not LGTY's infrastructure) and has two subcommands, each a
// separately auditable promise about what it transmits:
//
//   - metadata (the default, unchanged since this binary's first release):
//     authenticates with a short-lived OIDC token and sends the LGTY backend
//     read-only database METADATA — table names, row-count ESTIMATES, sizes,
//     and foreign-key dependency edges. It NEVER reads or transmits row
//     data. Enforced in internal/collect/guard.go.
//
//   - renders (LGT-404): authenticates with a DIFFERENT short-lived OIDC
//     token, on a DIFFERENT audience, and uploads PNG screenshots the
//     customer's own CI already rendered, plus the small set of identifiers
//     named in a manifest the customer's renderer writes. It transmits
//     exactly the files that manifest names, from the directory the
//     customer pointed it at, and nothing else. Enforced in
//     internal/renders/manifest.go.
//
// A workflow that never invokes the renders subcommand never runs any of its
// code — see README.md for why that separation is the whole point.
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/trulayer/lgty-action/internal/cienv"
	"github.com/trulayer/lgty-action/internal/collect"
	"github.com/trulayer/lgty-action/internal/config"
	"github.com/trulayer/lgty-action/internal/ingest"
	"github.com/trulayer/lgty-action/internal/oidc"
	"github.com/trulayer/lgty-action/internal/renders"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("lgty-action: ")
	if err := dispatch(context.Background(), os.Stdout, os.Args[1:], time.Now); err != nil {
		log.Fatal(err)
	}
}

// dispatch selects a subcommand from argv. No argument at all defaults to
// "metadata" — every action.yml and every `uses: trulayer/lgty-action@v1`
// step written before this change passes none, and must keep behaving
// exactly as before.
func dispatch(ctx context.Context, stdout io.Writer, args []string, now func() time.Time) error {
	cmd := "metadata"
	if len(args) > 0 {
		cmd = args[0]
	}
	switch cmd {
	case "metadata":
		return runMetadata(ctx, stdout, now)
	case "renders":
		return runRenders(ctx, stdout, now)
	default:
		return fmt.Errorf(`unknown command %q — expected "metadata" or "renders"`, cmd)
	}
}

// runMetadata is the original, unmodified pipeline: OIDC -> guarded Postgres
// collection -> ingest. See docs/inputs-outputs.md "Metadata subcommand".
func runMetadata(ctx context.Context, stdout io.Writer, now func() time.Time) error {
	cfg, err := config.LoadMetadata()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	log.Printf("metadata: workspace=%s repo=%s db=%s dry_run=%t", cfg.Workspace, cfg.Repo, cfg.DBKind, cfg.DryRun)
	log.Print("metadata: this command emits METADATA ONLY (table names, row-count ESTIMATES, sizes, dependency edges). It never reads or sends row data.")

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
	md.CollectedAt = now().UTC()
	md.Repo = cfg.Repo
	md.Workspace = cfg.Workspace

	// 4. Ship it (or print it, in dry-run, so the customer can see exactly what leaves).
	if cfg.DryRun {
		return ingest.Print(stdout, md)
	}
	return ingest.Send(ctx, cfg.BackendURL, token, md)
}

// runRenders is the LGT-404 pipeline: read the customer's capture manifest,
// resolve the commit it belongs to, and upload every capture it names to
// POST /v1/renders, then signal completion with POST /v1/renders/complete so
// an already-published brief can be upgraded in place. See
// docs/inputs-outputs.md "Renders subcommand".
func runRenders(ctx context.Context, stdout io.Writer, now func() time.Time) error {
	_ = now // reserved for a future timestamped log line; not needed yet
	cfg, err := config.LoadRenders()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	entries, err := renders.LoadManifest(cfg.RendersDir)
	if err != nil {
		return fmt.Errorf("manifest: %w", err)
	}
	if len(entries) == 0 {
		// A manifest that names zero captures is very likely a broken renderer,
		// not an intentional "nothing to capture this run" — failing loud here
		// is the cheapest place to catch that, the same reasoning that makes an
		// empty DSN a hard failure on the metadata path rather than a silent
		// empty upload.
		return fmt.Errorf("manifest: %s/%s names zero captures", cfg.RendersDir, renders.ManifestFile)
	}

	commitSHA := cfg.CommitSHA
	if commitSHA == "" {
		commitSHA, err = cienv.ResolveHeadCommitSHA()
		if err != nil {
			return fmt.Errorf("resolve commit sha: %w (or set commit-sha explicitly)", err)
		}
	}
	log.Printf("renders: repo=%s commit=%s captures=%d dry_run=%t", os.Getenv("GITHUB_REPOSITORY"), commitSHA, len(entries), cfg.DryRun)
	log.Print("renders: this command transmits EXACTLY the PNG files and identifiers your manifest.json names — nothing else, and nothing the metadata command's payload ever includes.")

	if cfg.DryRun {
		planned := make([]renders.PlannedCapture, 0, len(entries))
		for _, e := range entries {
			img, err := renders.LoadImage(e.Path(cfg.RendersDir))
			if err != nil {
				return fmt.Errorf("%s: %w", e.File, err)
			}
			planned = append(planned, renders.PlannedCapture{
				File: e.File, StateID: e.StateID,
				CaptureKey:   captureKey(e),
				CaptureIndex: *e.CaptureIndex, CaptureCount: *e.CaptureCount,
				WidthPx: img.Width, HeightPx: img.Height,
				ByteSize: len(img.Bytes), SHA256: img.SHA256,
			})
		}
		return renders.PrintManifest(stdout, commitSHA, planned)
	}

	token, err := oidc.FetchToken(ctx, cfg.OIDCAudience)
	if err != nil {
		return fmt.Errorf("oidc: %w", err)
	}

	var failures []error
	for _, e := range entries {
		img, err := renders.LoadImage(e.Path(cfg.RendersDir))
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", e.File, err))
			continue
		}
		meta := renders.CaptureMetadata{
			CommitSHA: commitSHA, StateID: e.StateID, CaptureKey: captureKey(e),
			CaptureIndex: *e.CaptureIndex, CaptureCount: *e.CaptureCount,
		}
		ack, err := renders.Upload(ctx, cfg.BackendURL, token, meta, img)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", e.File, err))
			continue
		}
		log.Printf("renders: uploaded %s state=%q index=%d/%d stored=%t", e.File, e.StateID, ack.CaptureIndex, ack.CaptureCount, ack.Stored)
	}

	// The completion call is made regardless of individual capture failures —
	// see the "Some states uploaded, others not" outcome in TDD §4.3.1. A
	// partial batch is still worth telling the backend about: it is what lets
	// the resolver show "N of M states compared" instead of leaving the brief
	// stuck on "not run" because this call never arrived.
	ack, completeErr := renders.Complete(ctx, cfg.BackendURL, token, commitSHA)
	if completeErr == nil {
		log.Printf("renders: completion posted commit=%s brief_update_queued=%t", ack.CommitSHA, ack.BriefUpdateQueued)
	}

	if len(failures) > 0 {
		msg := fmt.Sprintf("%d of %d captures failed to upload:", len(failures), len(entries))
		for _, f := range failures {
			msg += "\n  - " + f.Error()
		}
		return fmt.Errorf("%s", msg)
	}
	if completeErr != nil {
		return fmt.Errorf("complete: %w", completeErr)
	}
	return nil
}

// captureKey builds the wire CaptureKey from a manifest entry, filling in
// image_format (always "png") and runner_image (auto-detected from the CI
// environment when the manifest does not supply one).
func captureKey(e renders.ManifestEntry) renders.CaptureKey {
	runnerImage := e.CaptureKey.RunnerImage
	if runnerImage == "" {
		runnerImage = cienv.DefaultRunnerImage()
	}
	return renders.CaptureKey{
		ViewportWidth: e.CaptureKey.ViewportWidth, ViewportHeight: e.CaptureKey.ViewportHeight,
		DeviceScaleFactor: e.CaptureKey.DeviceScaleFactor, ColorScheme: e.CaptureKey.ColorScheme,
		BrowserEngine: e.CaptureKey.BrowserEngine, BrowserVersion: e.CaptureKey.BrowserVersion,
		ImageFormat: "png", RunnerImage: runnerImage,
	}
}
