package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/trulayer/lgty-action/internal/collect"
)

func sampleMetadata() collect.Metadata {
	return collect.Metadata{
		Workspace:   "ws_1",
		Repo:        "acme/service",
		CollectedAt: time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC),
		Tables: []collect.TableMeta{
			{Schema: "public", Name: "customers", RowEstimate: 42, TotalBytes: 8192, ColumnCount: 3},
		},
		Deps: []collect.DepEdge{
			{FromSchema: "public", FromTable: "orders", ToSchema: "public", ToTable: "customers"},
		},
	}
}

func TestPrint(t *testing.T) {
	var buf bytes.Buffer
	if err := Print(&buf, sampleMetadata()); err != nil {
		t.Fatalf("Print() error = %v", err)
	}
	out := buf.String()

	// Indented (human-auditable) JSON — the customer must see exactly what leaves.
	if !strings.Contains(out, "\n  ") {
		t.Error("expected indented JSON output")
	}

	// Round-trips back to the same payload.
	var got collect.Metadata
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("Print output is not valid JSON: %v", err)
	}
	if got.Repo != "acme/service" || len(got.Tables) != 1 || got.Tables[0].RowEstimate != 42 {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func TestSend_Success(t *testing.T) {
	var gotBody collect.Metadata
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v1/ingest/metadata" {
			t.Errorf("path = %s, want /v1/ingest/metadata", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok-123" {
			t.Errorf("Authorization = %q, want Bearer tok-123", got)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	if err := Send(context.Background(), srv.URL, "tok-123", sampleMetadata()); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if gotBody.Repo != "acme/service" || len(gotBody.Tables) != 1 {
		t.Errorf("server received unexpected body: %+v", gotBody)
	}
}

func TestSend_NoTokenOmitsAuthHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization header should be absent when token is empty, got %q", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := Send(context.Background(), srv.URL, "", sampleMetadata()); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
}

func TestSend_Non2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "boom")
	}))
	defer srv.Close()

	err := Send(context.Background(), srv.URL, "tok", sampleMetadata())
	if err == nil {
		t.Fatal("expected error on non-2xx response")
	}
	if !strings.Contains(err.Error(), "ingest failed") || !strings.Contains(err.Error(), "boom") {
		t.Errorf("error = %v, want it to include status and body", err)
	}
}

func TestSend_TransportErrorIsWrapped(t *testing.T) {
	// A server that is immediately closed forces a connection error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	err := Send(context.Background(), url, "tok", sampleMetadata())
	if err == nil {
		t.Fatal("expected transport error against closed server")
	}
	if !strings.Contains(err.Error(), "post metadata") {
		t.Errorf("error = %v, want it wrapped with 'post metadata'", err)
	}
}
