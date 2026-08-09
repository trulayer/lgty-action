// Package cienv reads facts GitHub Actions itself exposes about the run, so
// the renders subcommand needs no LGTY-specific configuration to get them
// right. It never talks to the network or the GitHub API — everything here
// comes from the runner's own environment and the event payload GitHub
// already wrote to disk.
package cienv

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// ResolveHeadCommitSHA returns the commit this CI run is actually testing.
//
// This exists because of one sharp edge: on a pull_request-triggered run,
// $GITHUB_SHA is the ephemeral merge commit GitHub built to run CI against —
// never the pull request's own head. Uploading captures under that SHA would
// silently mismatch the commit the backend's own OIDC-claim binding checks
// against (lgty-backend internal/renders/handler.go, "NEVER claims.Sha" on
// the head path), and every upload would be refused. lgty-frontend's own
// capture workflow already computes this by hand
// (LGTY_CAPTURE_COMMIT_SHA in visual-review-capture.yml); resolving it here
// means no other customer has to reproduce that by hand.
func ResolveHeadCommitSHA() (string, error) {
	if os.Getenv("GITHUB_EVENT_NAME") != "pull_request" {
		sha := os.Getenv("GITHUB_SHA")
		if sha == "" {
			return "", errors.New("GITHUB_SHA is not set (not running under GitHub Actions?) — pass commit-sha explicitly")
		}
		return sha, nil
	}

	path := os.Getenv("GITHUB_EVENT_PATH")
	if path == "" {
		return "", errors.New("GITHUB_EVENT_PATH is not set; cannot resolve the pull request's head SHA — pass commit-sha explicitly")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read GITHUB_EVENT_PATH: %w", err)
	}
	var event struct {
		PullRequest struct {
			Head struct {
				SHA string `json:"sha"`
			} `json:"head"`
		} `json:"pull_request"`
	}
	if err := json.Unmarshal(raw, &event); err != nil {
		return "", fmt.Errorf("decode pull_request event payload: %w", err)
	}
	if event.PullRequest.Head.SHA == "" {
		return "", errors.New("pull_request event payload carries no pull_request.head.sha")
	}
	return event.PullRequest.Head.SHA, nil
}

// DefaultRunnerImage returns the GitHub-hosted runner image identifier
// (e.g. "ubuntu24-20250801.1.0"), or "" when the CI provider does not expose
// one — most notably self-hosted runners, which set neither variable. "" is
// a legitimate answer, not an error: RenderCaptureKey.runner_image is the
// one optional component of the capture key precisely because a CI system
// that does not expose this cannot be made to (api/openapi.yaml).
func DefaultRunnerImage() string {
	os_, version := os.Getenv("ImageOS"), os.Getenv("ImageVersion")
	switch {
	case os_ == "":
		return ""
	case version == "":
		return os_
	default:
		return os_ + "-" + version
	}
}
