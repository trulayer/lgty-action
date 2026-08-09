package cienv

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveHeadCommitSHA_NonPullRequestUsesGitHubSHA(t *testing.T) {
	t.Setenv("GITHUB_EVENT_NAME", "push")
	t.Setenv("GITHUB_SHA", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	t.Setenv("GITHUB_EVENT_PATH", "")

	sha, err := ResolveHeadCommitSHA()
	if err != nil {
		t.Fatalf("ResolveHeadCommitSHA() error = %v", err)
	}
	if sha != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Errorf("sha = %q, want GITHUB_SHA", sha)
	}
}

func TestResolveHeadCommitSHA_PullRequestUsesEventPayloadHeadNotGitHubSHA(t *testing.T) {
	// The whole point of this function: on pull_request, GITHUB_SHA is the
	// ephemeral merge commit, and the PR's real head lives only in the event
	// payload. Getting this wrong means every upload the backend refuses.
	dir := t.TempDir()
	eventPath := filepath.Join(dir, "event.json")
	if err := os.WriteFile(eventPath, []byte(`{"pull_request":{"head":{"sha":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_EVENT_NAME", "pull_request")
	t.Setenv("GITHUB_SHA", "cccccccccccccccccccccccccccccccccccccccc") // the merge commit — must NOT be returned
	t.Setenv("GITHUB_EVENT_PATH", eventPath)

	sha, err := ResolveHeadCommitSHA()
	if err != nil {
		t.Fatalf("ResolveHeadCommitSHA() error = %v", err)
	}
	if sha != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Errorf("sha = %q, want the pull_request.head.sha from the event payload, not GITHUB_SHA", sha)
	}
}

func TestResolveHeadCommitSHA_PullRequestMissingEventPathErrors(t *testing.T) {
	t.Setenv("GITHUB_EVENT_NAME", "pull_request")
	t.Setenv("GITHUB_EVENT_PATH", "")

	if _, err := ResolveHeadCommitSHA(); err == nil {
		t.Fatal("expected an error when GITHUB_EVENT_PATH is unset on a pull_request event")
	}
}

func TestResolveHeadCommitSHA_NonGitHubActionsErrors(t *testing.T) {
	t.Setenv("GITHUB_EVENT_NAME", "")
	t.Setenv("GITHUB_SHA", "")

	if _, err := ResolveHeadCommitSHA(); err == nil {
		t.Fatal("expected an error when neither GITHUB_EVENT_NAME nor GITHUB_SHA is set")
	}
}

func TestDefaultRunnerImage(t *testing.T) {
	tests := []struct {
		name, os, version, want string
	}{
		{"both set", "ubuntu24", "20250801.1.0", "ubuntu24-20250801.1.0"},
		{"os only", "ubuntu24", "", "ubuntu24"},
		{"neither set (self-hosted)", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ImageOS", tt.os)
			t.Setenv("ImageVersion", tt.version)
			if got := DefaultRunnerImage(); got != tt.want {
				t.Errorf("DefaultRunnerImage() = %q, want %q", got, tt.want)
			}
		})
	}
}
