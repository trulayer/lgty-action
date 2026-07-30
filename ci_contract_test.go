package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

const codecovActionSHA = "0fb7174895f61a3b6b78fc075e0cd60383518dac"

func TestCodecovUploadUsesPinnedOIDCWithLeastPrivilege(t *testing.T) {
	workflow := readContractFile(t, ".github/workflows/ci.yml")

	for _, forbidden := range []string{"CODECOV_TOKEN", "codecov/codecov-action@v"} {
		if strings.Contains(workflow, forbidden) {
			t.Fatalf("CI workflow must not contain %q", forbidden)
		}
	}
	if regexp.MustCompile(`(?m)^\s+token:`).MatchString(workflow) {
		t.Fatal("Codecov configuration must not use a long-lived token")
	}

	pinnedAction := "uses: codecov/codecov-action@" + codecovActionSHA
	if count := strings.Count(workflow, pinnedAction); count != 1 {
		t.Fatalf("CI workflow must use exactly one SHA-pinned Codecov action; found %d", count)
	}
	if count := strings.Count(workflow, "uses: codecov/codecov-action@"); count != 1 {
		t.Fatalf("CI workflow must contain exactly one Codecov action; found %d", count)
	}
	if count := strings.Count(workflow, "use_oidc: true"); count != 1 {
		t.Fatalf("CI workflow must enable Codecov OIDC exactly once; found %d", count)
	}
	if count := strings.Count(workflow, "id-token: write"); count != 1 {
		t.Fatalf("CI workflow must grant id-token: write to exactly one job; found %d", count)
	}

	const wantDefaultPermissions = "permissions:\n  contents: read\n\njobs:"
	if !strings.Contains(workflow, wantDefaultPermissions) {
		t.Fatal("workflow default permissions must remain contents: read only")
	}

	build := jobBlock(t, workflow, "build", "semgrep")
	wantBuildPermissions := "    permissions:\n      contents: read\n      id-token: write"
	if got := permissionBlock(t, build); got != wantBuildPermissions {
		t.Fatalf("build permissions exceed the required minimum:\n%s", got)
	}

	semgrep := jobBlock(t, workflow, "semgrep", "ci")
	if strings.Contains(semgrep, "id-token:") {
		t.Fatal("semgrep job must not receive OIDC permission")
	}

	aggregate := jobBlock(t, workflow, "ci", "")
	if !strings.Contains(aggregate, "    permissions: {}") {
		t.Fatal("aggregate ci job must retain empty permissions")
	}
}

func TestCodecovBadgeDoesNotCarryToken(t *testing.T) {
	readme := readContractFile(t, "README.md")
	const badge = "https://codecov.io/gh/trulayer/lgty-action/graph/badge.svg"

	if !strings.Contains(readme, badge+")") {
		t.Fatal("README must retain the plain public Codecov badge")
	}
	if strings.Contains(readme, badge+"?") {
		t.Fatal("public Codecov badge must not contain query credentials")
	}
}

func readContractFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}

func jobBlock(t *testing.T, workflow, name, next string) string {
	t.Helper()
	startMarker := "  " + name + ":\n"
	start := strings.Index(workflow, startMarker)
	if start < 0 {
		t.Fatalf("job %q not found", name)
	}
	end := len(workflow)
	if next != "" {
		nextMarker := "\n  " + next + ":\n"
		if offset := strings.Index(workflow[start:], nextMarker); offset >= 0 {
			end = start + offset
		} else {
			t.Fatalf("job following %q not found", name)
		}
	}
	return workflow[start:end]
}

func permissionBlock(t *testing.T, job string) string {
	t.Helper()
	lines := strings.Split(job, "\n")
	for i, line := range lines {
		if line != "    permissions:" {
			continue
		}
		end := i + 1
		for end < len(lines) && strings.HasPrefix(lines[end], "      ") {
			end++
		}
		return strings.Join(lines[i:end], "\n")
	}
	t.Fatal("job permissions block not found")
	return ""
}
