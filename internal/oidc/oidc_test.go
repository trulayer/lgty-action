package oidc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchToken_MissingEnv(t *testing.T) {
	// A job without `permissions: id-token: write` has neither var set.
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", "")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "")

	_, err := FetchToken(context.Background(), "lgty")
	if err == nil {
		t.Fatal("expected error when OIDC env is absent")
	}
	if !strings.Contains(err.Error(), "id-token") {
		t.Errorf("error = %v, want guidance mentioning id-token permission", err)
	}
}

func TestFetchToken_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The request token is passed as a bearer credential...
		if got := r.Header.Get("Authorization"); got != "Bearer req-tok" {
			t.Errorf("Authorization = %q, want Bearer req-tok", got)
		}
		// ...and the requested audience is forwarded as a query param.
		if aud := r.URL.Query().Get("audience"); aud != "lgty" {
			t.Errorf("audience = %q, want lgty", aud)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"value":"signed.jwt.token"}`))
	}))
	defer srv.Close()

	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", srv.URL)
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "req-tok")

	tok, err := FetchToken(context.Background(), "lgty")
	if err != nil {
		t.Fatalf("FetchToken() error = %v", err)
	}
	if tok != "signed.jwt.token" {
		t.Errorf("token = %q, want signed.jwt.token", tok)
	}
}

func TestFetchToken_PreservesExistingQueryParams(t *testing.T) {
	var gotAPIVersion string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIVersion = r.URL.Query().Get("api-version")
		_, _ = w.Write([]byte(`{"value":"jwt"}`))
	}))
	defer srv.Close()

	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", srv.URL+"/?api-version=2.0")
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "req-tok")

	if _, err := FetchToken(context.Background(), "lgty"); err != nil {
		t.Fatalf("FetchToken() error = %v", err)
	}
	if gotAPIVersion != "2.0" {
		t.Errorf("api-version = %q, want the existing param preserved", gotAPIVersion)
	}
}

func TestFetchToken_Non200IsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("nope"))
	}))
	defer srv.Close()

	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", srv.URL)
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "req-tok")

	_, err := FetchToken(context.Background(), "lgty")
	if err == nil {
		t.Fatal("expected error on non-200 OIDC response")
	}
	if !strings.Contains(err.Error(), "OIDC request failed") {
		t.Errorf("error = %v, want it to mention the failed request", err)
	}
}

func TestFetchToken_EmptyValueIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"value":""}`))
	}))
	defer srv.Close()

	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_URL", srv.URL)
	t.Setenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN", "req-tok")

	_, err := FetchToken(context.Background(), "lgty")
	if err == nil {
		t.Fatal("expected error when OIDC token value is empty")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error = %v, want it to mention empty token", err)
	}
}
