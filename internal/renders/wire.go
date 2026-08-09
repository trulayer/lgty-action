// Package renders implements the renders subcommand: it reads a manifest of
// already-captured PNG screenshots the customer's own CI produced and uploads
// them to LGTY's POST /v1/renders and POST /v1/renders/complete.
//
// It renders nothing itself. Naming no browser, test runner, or component
// framework is the point — Playwright, Cypress, and Storybook's own runner
// all satisfy the manifest contract identically (see docs/inputs-outputs.md).
package renders

import "time"

// CaptureKey is the wire shape of one capture's environment fingerprint.
// Field names and types are a binding contract with the LGTY ingest API's
// capture-key schema — do not rename without a coordinated backend change and
// a major version bump.
type CaptureKey struct {
	ViewportWidth     int     `json:"viewport_width"`
	ViewportHeight    int     `json:"viewport_height"`
	DeviceScaleFactor float64 `json:"device_scale_factor"`
	ColorScheme       string  `json:"color_scheme"` // "light" | "dark"
	BrowserEngine     string  `json:"browser_engine"`
	BrowserVersion    string  `json:"browser_version"`
	ImageFormat       string  `json:"image_format"` // always "png" — the only format the backend accepts in v1
	RunnerImage       string  `json:"runner_image,omitempty"`
}

// CaptureMetadata is the wire shape of the "capture" part of the
// multipart/form-data body POSTed to /v1/renders. Field names and types are a
// binding contract with the LGTY ingest API's capture-metadata schema.
type CaptureMetadata struct {
	CommitSHA    string     `json:"commit_sha"`
	StateID      string     `json:"state_id"`
	CaptureKey   CaptureKey `json:"capture_key"`
	CaptureIndex int        `json:"capture_index"`
	CaptureCount int        `json:"capture_count"`
}

// CaptureAck is the wire shape of a successful /v1/renders response.
type CaptureAck struct {
	CaptureID    string    `json:"capture_id"`
	CommitSHA    string    `json:"commit_sha"`
	StateID      string    `json:"state_id"`
	CaptureIndex int       `json:"capture_index"`
	CaptureCount int       `json:"capture_count"`
	Stored       bool      `json:"stored"` // false = identical identity already stored (still a success)
	ReceivedAt   time.Time `json:"received_at"`
}

// CompletionRequest is the wire shape of the /v1/renders/complete body.
type CompletionRequest struct {
	CommitSHA string `json:"commit_sha"`
}

// CompletionAck is the wire shape of a successful /v1/renders/complete response.
type CompletionAck struct {
	CommitSHA         string `json:"commit_sha"`
	PullRequestNumber *int   `json:"pull_request_number,omitempty"`
	BriefUpdateQueued bool   `json:"brief_update_queued"`
}

// wireError mirrors the ingest API's shared error body, used to surface the
// backend's own code/message on a non-2xx response instead of a bare status.
type wireError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
