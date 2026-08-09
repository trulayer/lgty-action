package renders

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testMeta() CaptureMetadata {
	return CaptureMetadata{
		CommitSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		StateID:   "dashboard",
		CaptureKey: CaptureKey{
			ViewportWidth: 1280, ViewportHeight: 720, DeviceScaleFactor: 1,
			ColorScheme: "light", BrowserEngine: "chromium", BrowserVersion: "128.0.0.0",
			ImageFormat: "png",
		},
		CaptureIndex: 0, CaptureCount: 1,
	}
}

func TestUpload_PartOrderAndAuthHeader(t *testing.T) {
	var gotAuth string
	var gotFirstPart, gotSecondPart string
	var gotMeta CaptureMetadata
	var gotImageBytes []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/renders" {
			t.Errorf("request = %s %s, want POST /v1/renders", r.Method, r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")

		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "multipart/form-data" {
			t.Fatalf("Content-Type = %q, want multipart/form-data", r.Header.Get("Content-Type"))
		}
		mr := multipart.NewReader(r.Body, params["boundary"])

		part1, err := mr.NextPart()
		if err != nil {
			t.Fatalf("first part: %v", err)
		}
		gotFirstPart = part1.FormName()
		raw1, _ := io.ReadAll(part1)
		_ = json.Unmarshal(raw1, &gotMeta)

		part2, err := mr.NextPart()
		if err != nil {
			t.Fatalf("second part: %v", err)
		}
		gotSecondPart = part2.FormName()
		gotImageBytes, _ = io.ReadAll(part2)

		if _, err := mr.NextPart(); err != io.EOF {
			t.Errorf("expected exactly two parts, found a third")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(CaptureAck{
			CaptureID: "11111111-1111-1111-1111-111111111111", CommitSHA: gotMeta.CommitSHA,
			StateID: gotMeta.StateID, CaptureIndex: gotMeta.CaptureIndex, CaptureCount: gotMeta.CaptureCount,
			Stored: true, ReceivedAt: time.Unix(0, 0).UTC(),
		})
	}))
	defer srv.Close()

	meta := testMeta()
	img := Image{Bytes: []byte("fake-png-bytes"), Width: 1280, Height: 720, SHA256: "deadbeef"}

	ack, err := Upload(context.Background(), srv.URL, "fake-oidc-token", meta, img)
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	// The order is the wire contract, not an implementation detail — capture
	// (authorization) must arrive strictly before image (untrusted bytes).
	if gotFirstPart != "capture" {
		t.Errorf("first multipart field = %q, want \"capture\"", gotFirstPart)
	}
	if gotSecondPart != "image" {
		t.Errorf("second multipart field = %q, want \"image\"", gotSecondPart)
	}
	if gotAuth != "Bearer fake-oidc-token" {
		t.Errorf("Authorization = %q, want Bearer fake-oidc-token", gotAuth)
	}
	if gotMeta.CommitSHA != meta.CommitSHA || gotMeta.StateID != meta.StateID {
		t.Errorf("decoded capture metadata = %+v, want %+v", gotMeta, meta)
	}
	if string(gotImageBytes) != string(img.Bytes) {
		t.Errorf("image bytes = %q, want %q", gotImageBytes, img.Bytes)
	}
	if !ack.Stored {
		t.Error("ack.Stored = false, want true")
	}
}

func TestUpload_NonCreatedStatusReturnsBackendError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"code":"fork_head_repo","message":"captures from forked pull requests are not accepted"}`))
	}))
	defer srv.Close()

	_, err := Upload(context.Background(), srv.URL, "tok", testMeta(), Image{Bytes: []byte("x")})
	if err == nil {
		t.Fatal("expected an error on a 403 response")
	}
	for _, want := range []string{"403", "fork_head_repo", "forked pull requests"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to contain %q", err.Error(), want)
		}
	}
}

func TestComplete_RequestShapeAndResponse(t *testing.T) {
	var gotBody CompletionRequest
	var gotAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/renders/complete" {
			t.Errorf("request = %s %s, want POST /v1/renders/complete", r.Method, r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)

		pr := 42
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(CompletionAck{CommitSHA: gotBody.CommitSHA, PullRequestNumber: &pr, BriefUpdateQueued: true})
	}))
	defer srv.Close()

	ack, err := Complete(context.Background(), srv.URL, "fake-oidc-token", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if gotAuth != "Bearer fake-oidc-token" {
		t.Errorf("Authorization = %q, want Bearer fake-oidc-token", gotAuth)
	}
	if gotBody.CommitSHA != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Errorf("request commit_sha = %q", gotBody.CommitSHA)
	}
	if !ack.BriefUpdateQueued {
		t.Error("BriefUpdateQueued = false, want true")
	}
	if ack.PullRequestNumber == nil || *ack.PullRequestNumber != 42 {
		t.Errorf("PullRequestNumber = %v, want 42", ack.PullRequestNumber)
	}
}

func TestComplete_StaleCommitIsSuccessWithQueuedFalse(t *testing.T) {
	// A completion for a commit that is no longer the PR head is a SUCCESS
	// that updates nothing, never an error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(CompletionAck{CommitSHA: "stale", BriefUpdateQueued: false})
	}))
	defer srv.Close()

	ack, err := Complete(context.Background(), srv.URL, "tok", "stale")
	if err != nil {
		t.Fatalf("Complete() error = %v, want success even for a stale commit", err)
	}
	if ack.BriefUpdateQueued {
		t.Error("BriefUpdateQueued = true, want false for a stale commit")
	}
}
