package renders

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ManifestFile is the fixed name of the manifest a renders-dir must contain.
// One name, not a configurable one — a caller that wants a different name
// does not exist yet (see CLAUDE.md's scope-discipline rule).
const ManifestFile = "manifest.json"

// ManifestEntry is what a customer's renderer writes to describe ONE
// capture. It is deliberately smaller than the wire CaptureMetadata: no
// commit_sha (resolved once per run, never per capture — see
// internal/cienv), no image_format (the action only ever reads .png files
// and sets this itself), and capture_index/capture_count default to 0/1 so a
// renderer that only ever takes one screenshot per state writes nothing
// extra.
type ManifestEntry struct {
	File         string      `json:"file"`
	StateID      string      `json:"state_id"`
	CaptureKey   ManifestKey `json:"capture_key"`
	CaptureIndex *int        `json:"capture_index,omitempty"`
	CaptureCount *int        `json:"capture_count,omitempty"`
}

// ManifestKey is CaptureKey minus the fields the action fills in itself
// (image_format is always "png"; runner_image is auto-detected from the CI
// environment when the manifest does not supply one — see internal/cienv).
type ManifestKey struct {
	ViewportWidth     int     `json:"viewport_width"`
	ViewportHeight    int     `json:"viewport_height"`
	DeviceScaleFactor float64 `json:"device_scale_factor"`
	ColorScheme       string  `json:"color_scheme"`
	BrowserEngine     string  `json:"browser_engine"`
	BrowserVersion    string  `json:"browser_version"`
	RunnerImage       string  `json:"runner_image,omitempty"`
}

// LoadManifest reads and validates dir/manifest.json. This is this
// subcommand's guard, in the same sense internal/collect/guard.go is the
// metadata subcommand's: the ONLY thing ever read or transmitted is a file
// named in this manifest, resolved to stay inside dir, decodable as PNG. A
// manifest that fails any check here uploads nothing — there is no partial
// trust of a malformed manifest.
func LoadManifest(dir string) ([]ManifestEntry, error) {
	path := filepath.Join(dir, ManifestFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w (a renderer must write this file — see docs/inputs-outputs.md \"Renders subcommand\")", path, err)
	}

	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var entries []ManifestEntry
	if err := decoder.Decode(&entries); err != nil {
		return nil, fmt.Errorf("%s: must be a JSON array of captures: %w", path, err)
	}
	if decoder.More() {
		return nil, fmt.Errorf("%s: must contain exactly one JSON array", path)
	}

	for i := range entries {
		if err := validateEntry(dir, &entries[i]); err != nil {
			return nil, fmt.Errorf("%s: capture %d: %w", path, i, err)
		}
	}
	return entries, nil
}

// validateEntry checks one manifest entry and fills in its defaults
// (capture_index=0, capture_count=1 when omitted). It mirrors the bounds
// lgty-backend's own RenderCaptureMetadata/RenderCaptureKey schema enforces
// (api/openapi.yaml) so a malformed manifest fails fast, locally, against
// the exact entry at fault — instead of spending a request to learn the
// backend would have 400'd it anyway.
func validateEntry(dir string, e *ManifestEntry) error {
	if e.File == "" {
		return fmt.Errorf("\"file\" is required")
	}
	if filepath.IsAbs(e.File) || strings.Contains(e.File, "..") {
		return fmt.Errorf("\"file\" %q must be a relative path inside the renders directory, with no \"..\"", e.File)
	}
	resolved := filepath.Join(dir, e.File)
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve renders directory: %w", err)
	}
	absResolved, err := filepath.Abs(resolved)
	if err != nil {
		return fmt.Errorf("resolve %q: %w", e.File, err)
	}
	if absResolved != absDir && !strings.HasPrefix(absResolved, absDir+string(filepath.Separator)) {
		// Unreachable given the ".." check above, kept as defense in depth: the
		// one property this guard exists to hold is "never a file outside dir".
		return fmt.Errorf("\"file\" %q resolves outside the renders directory", e.File)
	}
	if !strings.HasSuffix(strings.ToLower(e.File), ".png") {
		return fmt.Errorf("\"file\" %q must be a .png file — png is the only format the backend accepts in v1", e.File)
	}

	if e.StateID == "" {
		return fmt.Errorf("\"state_id\" is required")
	}
	if len(e.StateID) > 512 {
		return fmt.Errorf("\"state_id\" is %d characters, over the 512 limit", len(e.StateID))
	}

	k := e.CaptureKey
	if k.ViewportWidth < 1 || k.ViewportWidth > 20000 {
		return fmt.Errorf("capture_key.viewport_width %d must be between 1 and 20000", k.ViewportWidth)
	}
	if k.ViewportHeight < 1 || k.ViewportHeight > 20000 {
		return fmt.Errorf("capture_key.viewport_height %d must be between 1 and 20000", k.ViewportHeight)
	}
	if k.DeviceScaleFactor < 0.1 || k.DeviceScaleFactor > 8 {
		return fmt.Errorf("capture_key.device_scale_factor %v must be between 0.1 and 8", k.DeviceScaleFactor)
	}
	if k.ColorScheme != "light" && k.ColorScheme != "dark" {
		return fmt.Errorf("capture_key.color_scheme %q must be \"light\" or \"dark\"", k.ColorScheme)
	}
	if k.BrowserEngine == "" {
		return fmt.Errorf("capture_key.browser_engine is required")
	}
	if k.BrowserVersion == "" {
		return fmt.Errorf("capture_key.browser_version is required")
	}

	index, count := 0, 1
	if e.CaptureIndex != nil {
		index = *e.CaptureIndex
	}
	if e.CaptureCount != nil {
		count = *e.CaptureCount
	}
	if index < 0 {
		return fmt.Errorf("capture_index %d must be >= 0", index)
	}
	if count < 1 || count > 16 {
		return fmt.Errorf("capture_count %d must be between 1 and 16", count)
	}
	if index >= count {
		return fmt.Errorf("capture_index %d must be less than capture_count %d", index, count)
	}
	e.CaptureIndex = &index
	e.CaptureCount = &count
	return nil
}

// Path returns the absolute path to this entry's image file inside dir.
func (e ManifestEntry) Path(dir string) string {
	return filepath.Join(dir, e.File)
}
