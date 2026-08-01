package main

// These tests close TDD §3.7 requirement 2 for the ingest path: a degrade path
// needs a test that inspects the artifact an operator actually reads (the CLI's
// exit code and its printed message), not an intermediate Go error value. The
// existing internal/ingest unit tests only assert on Send()'s returned error —
// that closes requirement 1 (the failure is detected) but not requirement 2
// (the failure is OBSERVABLE the way a customer's CI log shows it). These tests
// run the actual compiled binary as a subprocess, the same way a GitHub Actions
// step invokes it, and assert on its real exit code and stderr.
//
// Context: action PR #15 added `tables[].analyzed` to the payload before the
// backend's OpenAPI contract had the field, so every upload from the released
// action got rejected with 400 by the backend's DisallowUnknownFields decoder
// (fixed in backend PR #194). ingest.Send already wrapped that as an error and
// main() already mapped it to a non-zero exit via log.Fatal — this incident
// never silently passed. These tests are what should have already existed to
// prove that observable outcome, not a behavior change.
//
// Requires a real Postgres (LGTY_TEST_DB_DSN), same as main_integration_test.go
// — the action's non-dry-run config path requires a real DSN before it will
// even attempt to reach the ingest step, so there is no way to exercise the
// ingest failure through the actual binary without one. Every table/dependency
// query still runs against a real (empty) database; only the ingest endpoint is
// faked, so this is exercising the true collect -> ingest handoff, not a stub.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	actionBinaryOnce sync.Once
	actionBinaryPath string
	actionBinaryErr  error
)

// buildActionBinary compiles the real lgty-action binary once and reuses it
// across tests in this file, so the subprocess under test is exactly what
// ships, not a test harness standing in for it.
func buildActionBinary(t *testing.T) string {
	t.Helper()
	actionBinaryOnce.Do(func() {
		dir, err := os.MkdirTemp("", "lgty-action-bin-")
		if err != nil {
			actionBinaryErr = err
			return
		}
		out := filepath.Join(dir, "lgty-action")
		cmd := exec.Command("go", "build", "-o", out, ".")
		cmd.Dir = mustGetwd()
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			actionBinaryErr = fmt.Errorf("build lgty-action for subprocess test: %w: %s", err, stderr.String())
			return
		}
		actionBinaryPath = out
	})
	if actionBinaryErr != nil {
		t.Fatalf("%v", actionBinaryErr)
	}
	return actionBinaryPath
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return wd
}

// requireTestDB returns an admin DSN for a scratch Postgres, or skips/fails
// per the same convention main_integration_test.go already established:
// skip locally without LGTY_TEST_DB_DSN, but a hard failure in CI, since full
// orchestration coverage must never silently skip there.
func requireTestDB(t *testing.T) string {
	t.Helper()
	adminDSN := os.Getenv("LGTY_TEST_DB_DSN")
	if adminDSN == "" {
		if os.Getenv("CI") == "true" {
			t.Fatal("LGTY_TEST_DB_DSN must be set in CI; ingest-failure observable-outcome coverage may not skip")
		}
		t.Skip("set LGTY_TEST_DB_DSN to run the ingest-failure observable-outcome tests")
	}
	return adminDSN
}

// runActionAgainstFakeIngest runs the real binary in non-dry-run mode against
// a real (empty, freshly created) Postgres database and a fake ingest server,
// returning the process's actual exit error and captured stderr — the two
// things a CI operator reading their job log actually sees.
func runActionAgainstFakeIngest(t *testing.T, backendURL string) (exitErr error, stderr string) {
	t.Helper()
	adminDSN := requireTestDB(t)
	bin := buildActionBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// Reuses main_integration_test.go's DSN-rewrite helper (URL-based, not a
	// string Replace) rather than duplicating database provisioning here.
	dsn := createFullRunDatabase(ctx, t, adminDSN)

	oidc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"value":"fake-oidc-token"}`))
	}))
	defer oidc.Close()

	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(),
		"LGTY_DB_DSN="+dsn,
		"LGTY_DB_KIND=postgres",
		"LGTY_BACKEND_URL="+backendURL,
		"LGTY_DRY_RUN=false",
		"LGTY_REPO=acme/widgets",
		"LGTY_WORKSPACE=workspace-test",
		"ACTIONS_ID_TOKEN_REQUEST_URL="+oidc.URL,
		"ACTIONS_ID_TOKEN_REQUEST_TOKEN=fake-request-token",
	)
	var errBuf bytes.Buffer
	cmd.Stdout = &bytes.Buffer{}
	cmd.Stderr = &errBuf
	err := cmd.Run()
	return err, errBuf.String()
}

func TestRun_BackendRejects400_ExitsNonZeroAndNamesStatusAndCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"bad_request","message":"unknown field \"analyzed\""}`))
	}))
	defer srv.Close()

	exitErr, stderr := runActionAgainstFakeIngest(t, srv.URL)

	// The observable outcome: a non-zero process exit, the way GitHub Actions
	// fails the step. Not Send()'s returned error value — the actual exit code
	// of the actual binary.
	if exitErr == nil {
		t.Fatalf("expected the process to exit non-zero on a rejected upload; stderr:\n%s", stderr)
	}
	var ee *exec.ExitError
	if !errors.As(exitErr, &ee) {
		t.Fatalf("expected *exec.ExitError, got %T: %v", exitErr, exitErr)
	}
	if ee.ExitCode() != 1 {
		t.Fatalf("exit code = %d, want 1; stderr:\n%s", ee.ExitCode(), stderr)
	}

	// The emitted message: an engineer reading the CI log must see the status
	// and the backend's code/message, not a bare "exit status 1".
	for _, want := range []string{"400", "Bad Request", `"code":"bad_request"`, `unknown field \"analyzed\"`} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr does not contain %q; stderr:\n%s", want, stderr)
		}
	}
}

func TestRun_ConnectionRefused_ExitsNonZeroAndNamesFailure(t *testing.T) {
	// A backend-url with nothing listening forces a connection-refused error
	// at the transport layer, distinct from the 4xx case above.
	deadURL := "http://" + reserveClosedPort(t)

	exitErr, stderr := runActionAgainstFakeIngest(t, deadURL)

	if exitErr == nil {
		t.Fatalf("expected the process to exit non-zero when the ingest endpoint is unreachable; stderr:\n%s", stderr)
	}
	var ee *exec.ExitError
	if !errors.As(exitErr, &ee) {
		t.Fatalf("expected *exec.ExitError, got %T: %v", exitErr, exitErr)
	}
	if ee.ExitCode() != 1 {
		t.Fatalf("exit code = %d, want 1; stderr:\n%s", ee.ExitCode(), stderr)
	}

	for _, want := range []string{"post metadata", "connect"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr does not contain %q (connection failure not named); stderr:\n%s", want, stderr)
		}
	}
}

// reserveClosedPort opens then immediately closes a TCP listener so the
// returned address is (with overwhelming likelihood) refusing connections for
// the life of the test, without depending on any external unreachable host.
func reserveClosedPort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a port: %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("close reserved port: %v", err)
	}
	return addr
}
