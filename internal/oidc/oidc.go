// Package oidc fetches a short-lived OIDC identity token from the CI provider.
// No long-lived secret is ever used or stored — the token is minted per-run by
// the CI platform and expires quickly.
package oidc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

// FetchToken obtains an OIDC token from GitHub Actions.
//
// A workflow granted `permissions: id-token: write` is given two env vars —
// ACTIONS_ID_TOKEN_REQUEST_URL and ACTIONS_ID_TOKEN_REQUEST_TOKEN. We exchange
// them for a signed JWT scoped to the requested audience, which the LGTY backend
// verifies against GitHub's OIDC issuer.
func FetchToken(ctx context.Context, audience string) (string, error) {
	reqURL := os.Getenv("ACTIONS_ID_TOKEN_REQUEST_URL")
	reqTok := os.Getenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN")
	if reqURL == "" || reqTok == "" {
		return "", fmt.Errorf("no GitHub Actions OIDC environment found (add `permissions: id-token: write` to the job)")
	}

	u, err := url.Parse(reqURL)
	if err != nil {
		return "", fmt.Errorf("parse OIDC request url: %w", err)
	}
	q := u.Query()
	q.Set("audience", audience)
	u.RawQuery = q.Encode()

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+reqTok)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request OIDC token: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("OIDC request failed: %s: %s", resp.Status, string(body))
	}

	var out struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("decode OIDC response: %w", err)
	}
	if out.Value == "" {
		return "", fmt.Errorf("OIDC token was empty")
	}
	return out.Value, nil
}
