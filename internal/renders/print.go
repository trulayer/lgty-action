package renders

import (
	"encoding/json"
	"io"
)

// PlannedCapture is what --dry-run prints for one capture: everything the
// upload would carry, decoded and hashed locally, before anything transfers.
// Deliberately does NOT include the raw image bytes — a dry-run log is meant
// to be read and pasted, not to reproduce a multi-megabyte PNG in a CI log.
type PlannedCapture struct {
	File         string     `json:"file"`
	StateID      string     `json:"state_id"`
	CaptureKey   CaptureKey `json:"capture_key"`
	CaptureIndex int        `json:"capture_index"`
	CaptureCount int        `json:"capture_count"`
	WidthPx      int        `json:"width_px"`
	HeightPx     int        `json:"height_px"`
	ByteSize     int        `json:"byte_size"`
	SHA256       string     `json:"sha256"`
}

// PlannedManifest is the full --dry-run payload.
type PlannedManifest struct {
	CommitSHA string           `json:"commit_sha"`
	Captures  []PlannedCapture `json:"captures"`
}

// PrintManifest writes what a real run would upload — every file, its
// resolved dimensions, byte size, and content digest — without making any
// network call. This is the renders subcommand's equivalent of the metadata
// subcommand's --dry-run payload print (see ingest.Print), and closes the
// same commitment: nothing about this run is a surprise to whoever reads the
// job log before it ships anywhere.
func PrintManifest(w io.Writer, commitSHA string, planned []PlannedCapture) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(PlannedManifest{CommitSHA: commitSHA, Captures: planned})
}
