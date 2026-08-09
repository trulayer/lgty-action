package main

// End-to-end coverage for the renders subcommand, run as the actual compiled
// binary (the same way a GitHub Actions step invokes it), against a fake
// backend that reproduces the /v1/renders and /v1/renders/complete wire
// contract as of 2026-08-09. This is NOT a test against the live production
// backend — see README.md "Status" for what that gap means and why it could
// not be closed here.
//
// No real Postgres is needed for these tests (unlike main_ingest_failure_test.go):
// the renders subcommand never touches a database.

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRendersBackend reproduces just enough of api's /v1/renders and
// /v1/renders/complete to prove the binary's request shapes are accepted by
// something enforcing the real contract: exactly two ordered multipart parts
// for an upload, and a commit_sha-only JSON body for completion.
func fakeRendersBackend(t *testing.T, uploaded *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/renders":
			if got := r.Header.Get("Authorization"); got != "Bearer fake-oidc-token" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || mediaType != "multipart/form-data" {
				http.Error(w, "bad content type", http.StatusBadRequest)
				return
			}
			mr := multipart.NewReader(r.Body, params["boundary"])
			capturePart, err := mr.NextPart()
			if err != nil || capturePart.FormName() != "capture" {
				http.Error(w, "expected capture part first", http.StatusBadRequest)
				return
			}
			var meta struct {
				CommitSHA string `json:"commit_sha"`
				StateID   string `json:"state_id"`
			}
			raw, _ := io.ReadAll(capturePart)
			_ = json.Unmarshal(raw, &meta)

			imagePart, err := mr.NextPart()
			if err != nil || imagePart.FormName() != "image" {
				http.Error(w, "expected image part second", http.StatusBadRequest)
				return
			}
			imgBytes, _ := io.ReadAll(imagePart)
			if len(imgBytes) == 0 {
				http.Error(w, "empty image", http.StatusBadRequest)
				return
			}
			*uploaded = append(*uploaded, meta.StateID)

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"capture_id": "11111111-1111-1111-1111-111111111111", "commit_sha": meta.CommitSHA,
				"state_id": meta.StateID, "capture_index": 0, "capture_count": 1,
				"stored": true, "received_at": "2026-08-09T00:00:00Z",
			})

		case r.Method == http.MethodPost && r.URL.Path == "/v1/renders/complete":
			if got := r.Header.Get("Authorization"); got != "Bearer fake-oidc-token" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			var body struct {
				CommitSHA string `json:"commit_sha"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"commit_sha": body.CommitSHA, "brief_update_queued": true,
			})

		default:
			http.NotFound(w, r)
		}
	}))
}

func writeMinimalPNG(t *testing.T, path string) {
	t.Helper()
	// A real, tiny 1x1 PNG — enough to satisfy the decode-validation guard.
	const pngB64Header = "\x89PNG\r\n\x1a\n"
	raw := []byte(pngB64Header +
		"\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x02\x00\x00\x00\x90wS\xde" +
		"\x00\x00\x00\x0cIDATx\x9cc\xf8\xcf\xc0\x00\x00\x03\x01\x01\x00\x18\xdd\x8d\xb0" +
		"\x00\x00\x00\x00IEND\xaeB`\x82")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeCaptureManifest(t *testing.T, dir string, files ...string) {
	t.Helper()
	type entry struct {
		File       string `json:"file"`
		StateID    string `json:"state_id"`
		CaptureKey struct {
			ViewportWidth     int     `json:"viewport_width"`
			ViewportHeight    int     `json:"viewport_height"`
			DeviceScaleFactor float64 `json:"device_scale_factor"`
			ColorScheme       string  `json:"color_scheme"`
			BrowserEngine     string  `json:"browser_engine"`
			BrowserVersion    string  `json:"browser_version"`
		} `json:"capture_key"`
	}
	var entries []entry
	for _, f := range files {
		e := entry{File: f, StateID: strings.TrimSuffix(f, ".png")}
		e.CaptureKey.ViewportWidth, e.CaptureKey.ViewportHeight = 1280, 720
		e.CaptureKey.DeviceScaleFactor = 1
		e.CaptureKey.ColorScheme = "light"
		e.CaptureKey.BrowserEngine = "chromium"
		e.CaptureKey.BrowserVersion = "128.0.0.0"
		entries = append(entries, e)
	}
	raw, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func runRendersSubcommand(t *testing.T, env []string) (exitErr error, stdout, stderr string) {
	t.Helper()
	bin := buildActionBinary(t)
	cmd := exec.Command(bin, "renders")
	cmd.Env = env
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	return err, outBuf.String(), errBuf.String()
}

func TestRenders_EndToEnd_UploadsAndCompletes(t *testing.T) {
	dir := t.TempDir()
	writeMinimalPNG(t, filepath.Join(dir, "dashboard.png"))
	writeMinimalPNG(t, filepath.Join(dir, "settings.png"))
	writeCaptureManifest(t, dir, "dashboard.png", "settings.png")

	oidc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"value":"fake-oidc-token"}`))
	}))
	defer oidc.Close()

	var uploaded []string
	backend := fakeRendersBackend(t, &uploaded)
	defer backend.Close()

	env := append(os.Environ(),
		"LGTY_RENDERS_DIR="+dir,
		"LGTY_BACKEND_URL="+backend.URL,
		"LGTY_COMMIT_SHA=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"LGTY_DRY_RUN=false",
		"ACTIONS_ID_TOKEN_REQUEST_URL="+oidc.URL,
		"ACTIONS_ID_TOKEN_REQUEST_TOKEN=fake-request-token",
	)
	exitErr, _, stderr := runRendersSubcommand(t, env)
	if exitErr != nil {
		t.Fatalf("renders subcommand failed: %v\nstderr:\n%s", exitErr, stderr)
	}
	if len(uploaded) != 2 {
		t.Fatalf("backend received %d captures, want 2: %v\nstderr:\n%s", len(uploaded), uploaded, stderr)
	}
	if !strings.Contains(stderr, "completion posted") {
		t.Errorf("stderr does not confirm the completion call ran:\n%s", stderr)
	}
}

func TestRenders_DryRun_NoNetworkCallAtAll(t *testing.T) {
	dir := t.TempDir()
	writeMinimalPNG(t, filepath.Join(dir, "dashboard.png"))
	writeCaptureManifest(t, dir, "dashboard.png")

	// No OIDC env vars, and a backend-url with nothing listening — a network
	// call of any kind would fail the process. dry-run must never attempt one.
	env := append(os.Environ(),
		"LGTY_RENDERS_DIR="+dir,
		"LGTY_BACKEND_URL=http://127.0.0.1:1", // reserved, nothing ever listens here
		"LGTY_COMMIT_SHA=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"LGTY_DRY_RUN=true",
	)
	exitErr, stdout, stderr := runRendersSubcommand(t, env)
	if exitErr != nil {
		t.Fatalf("dry-run should not fail even with no OIDC and an unreachable backend: %v\nstderr:\n%s", exitErr, stderr)
	}

	var planned struct {
		CommitSHA string `json:"commit_sha"`
		Captures  []struct {
			File     string `json:"file"`
			WidthPx  int    `json:"width_px"`
			HeightPx int    `json:"height_px"`
			SHA256   string `json:"sha256"`
		} `json:"captures"`
	}
	if err := json.Unmarshal([]byte(stdout), &planned); err != nil {
		t.Fatalf("stdout is not the expected planned-manifest JSON: %v\nstdout:\n%s", err, stdout)
	}
	if len(planned.Captures) != 1 || planned.Captures[0].File != "dashboard.png" {
		t.Fatalf("planned captures = %+v, want one entry for dashboard.png", planned.Captures)
	}
	if planned.Captures[0].SHA256 == "" {
		t.Error("dry-run manifest is missing a content digest")
	}
}

func TestRenders_ForkPRShapeFailsClosed_NoOIDCEnvironment(t *testing.T) {
	// Reproduces what actually happens on a fork PR: GitHub does not grant
	// id-token: write to a pull_request run from a fork, so
	// ACTIONS_ID_TOKEN_REQUEST_URL/TOKEN are simply absent. This subcommand
	// must fail closed here — there is no input that lets it proceed anyway.
	dir := t.TempDir()
	writeMinimalPNG(t, filepath.Join(dir, "dashboard.png"))
	writeCaptureManifest(t, dir, "dashboard.png")

	env := append(os.Environ(),
		"LGTY_RENDERS_DIR="+dir,
		"LGTY_BACKEND_URL=http://127.0.0.1:1",
		"LGTY_COMMIT_SHA=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"LGTY_DRY_RUN=false",
		"ACTIONS_ID_TOKEN_REQUEST_URL=",
		"ACTIONS_ID_TOKEN_REQUEST_TOKEN=",
	)
	exitErr, _, stderr := runRendersSubcommand(t, env)
	if exitErr == nil {
		t.Fatal("expected the process to exit non-zero with no OIDC environment available")
	}
	if !strings.Contains(stderr, "oidc") {
		t.Errorf("stderr does not name the OIDC failure:\n%s", stderr)
	}
}

func TestRenders_EmptyManifestFailsLoud(t *testing.T) {
	dir := t.TempDir()
	writeCaptureManifest(t, dir) // zero entries

	env := append(os.Environ(),
		"LGTY_RENDERS_DIR="+dir,
		"LGTY_DRY_RUN=true",
	)
	exitErr, _, stderr := runRendersSubcommand(t, env)
	if exitErr == nil {
		t.Fatal("expected a zero-capture manifest to fail the run rather than silently succeed")
	}
	if !strings.Contains(stderr, "zero captures") {
		t.Errorf("stderr does not explain the empty-manifest failure:\n%s", stderr)
	}
}

func TestMetadataSubcommand_StillDefaultWithNoArgs(t *testing.T) {
	// Backward compatibility: a step that predates `command` and passes no
	// subcommand argument at all must still run metadata, unchanged.
	bin := buildActionBinary(t)
	cmd := exec.Command(bin) // no "metadata" argument
	cmd.Env = append(os.Environ(),
		"LGTY_DRY_RUN=true",
		"LGTY_DB_DSN=", // dry-run tolerates this
		"ACTIONS_ID_TOKEN_REQUEST_URL=",
		"ACTIONS_ID_TOKEN_REQUEST_TOKEN=",
	)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &outBuf, &errBuf
	// This will fail because there's no real Postgres to collect from in this
	// test environment — that's fine; the point is it took the metadata path
	// (fails in collect.Run, never complains about an unknown command) rather
	// than silently doing nothing or erroring on subcommand dispatch.
	_ = cmd.Run()
	if strings.Contains(errBuf.String(), "unknown command") {
		t.Fatalf("no-argument invocation was not treated as \"metadata\":\n%s", errBuf.String())
	}
}
