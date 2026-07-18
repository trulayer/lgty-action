// Package ingest ships the collected metadata to the LGTY backend (or prints it
// in dry-run so a customer can see exactly what would leave their perimeter).
package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/trulayer/lgty-action/internal/collect"
)

// Print writes the metadata payload to w as indented JSON. Used by --dry-run.
func Print(w io.Writer, md collect.Metadata) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(md)
}

// Send POSTs the metadata to the LGTY ingest endpoint, authenticated by the
// short-lived OIDC token.
func Send(ctx context.Context, backendURL, oidcToken string, md collect.Metadata) error {
	body, err := json.Marshal(md)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, backendURL+"/v1/ingest/metadata", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if oidcToken != "" {
		req.Header.Set("Authorization", "Bearer "+oidcToken)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("post metadata: %w", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("ingest failed: %s: %s", resp.Status, string(b))
	}
	return nil
}
