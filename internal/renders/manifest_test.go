package renders

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeManifest(t *testing.T, dir string, entries []ManifestEntry) {
	t.Helper()
	raw, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ManifestFile), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadManifest_ValidSingleCapture(t *testing.T) {
	dir := t.TempDir()
	writePNG(t, dir, "dashboard.png", 4, 4)
	writeManifest(t, dir, []ManifestEntry{validManifestEntry("dashboard.png")})

	entries, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	e := entries[0]
	// Defaults fill in when the manifest omits them.
	if e.CaptureIndex == nil || *e.CaptureIndex != 0 {
		t.Errorf("CaptureIndex = %v, want 0", e.CaptureIndex)
	}
	if e.CaptureCount == nil || *e.CaptureCount != 1 {
		t.Errorf("CaptureCount = %v, want 1", e.CaptureCount)
	}
}

func TestLoadManifest_MissingFileErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadManifest(dir); err == nil {
		t.Fatal("expected an error when manifest.json does not exist")
	}
}

func TestLoadManifest_RejectsUnknownFields(t *testing.T) {
	dir := t.TempDir()
	writePNG(t, dir, "dashboard.png", 4, 4)
	if err := os.WriteFile(filepath.Join(dir, ManifestFile),
		[]byte(`[{"file":"dashboard.png","state_id":"dashboard","typo_field":true,"capture_key":{"viewport_width":1280,"viewport_height":720,"device_scale_factor":1,"color_scheme":"light","browser_engine":"chromium","browser_version":"1"}}]`),
		0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManifest(dir); err == nil {
		t.Fatal("expected an error for an unknown manifest field (strict decode)")
	}
}

func TestLoadManifest_RejectsPathTraversal(t *testing.T) {
	dir := t.TempDir()
	entry := validManifestEntry("../../etc/passwd.png")
	writeManifest(t, dir, []ManifestEntry{entry})

	if _, err := LoadManifest(dir); err == nil {
		t.Fatal("expected an error for a \"file\" that escapes the renders directory")
	}
}

func TestLoadManifest_RejectsAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	entry := validManifestEntry("/etc/passwd.png")
	writeManifest(t, dir, []ManifestEntry{entry})

	if _, err := LoadManifest(dir); err == nil {
		t.Fatal("expected an error for an absolute \"file\" path")
	}
}

func TestLoadManifest_RejectsNonPNGExtension(t *testing.T) {
	dir := t.TempDir()
	entry := validManifestEntry("dashboard.jpg")
	writeManifest(t, dir, []ManifestEntry{entry})

	if _, err := LoadManifest(dir); err == nil {
		t.Fatal("expected an error for a non-.png \"file\"")
	}
}

func TestLoadManifest_RejectsMissingRequiredCaptureKeyFields(t *testing.T) {
	dir := t.TempDir()
	writePNG(t, dir, "dashboard.png", 4, 4)
	entry := validManifestEntry("dashboard.png")
	entry.CaptureKey.ColorScheme = "" // required
	writeManifest(t, dir, []ManifestEntry{entry})

	if _, err := LoadManifest(dir); err == nil {
		t.Fatal("expected an error for a missing capture_key.color_scheme")
	}
}

func TestLoadManifest_RejectsInvalidColorScheme(t *testing.T) {
	dir := t.TempDir()
	writePNG(t, dir, "dashboard.png", 4, 4)
	entry := validManifestEntry("dashboard.png")
	entry.CaptureKey.ColorScheme = "sepia"
	writeManifest(t, dir, []ManifestEntry{entry})

	if _, err := LoadManifest(dir); err == nil {
		t.Fatal("expected an error for an invalid color_scheme")
	}
}

func TestLoadManifest_RejectsIndexNotLessThanCount(t *testing.T) {
	dir := t.TempDir()
	writePNG(t, dir, "dashboard.png", 4, 4)
	entry := validManifestEntry("dashboard.png")
	idx, count := 2, 2
	entry.CaptureIndex, entry.CaptureCount = &idx, &count
	writeManifest(t, dir, []ManifestEntry{entry})

	if _, err := LoadManifest(dir); err == nil {
		t.Fatal("expected an error when capture_index >= capture_count")
	}
}

func TestLoadManifest_RejectsEmptyStateID(t *testing.T) {
	dir := t.TempDir()
	writePNG(t, dir, "dashboard.png", 4, 4)
	entry := validManifestEntry("dashboard.png")
	entry.StateID = ""
	writeManifest(t, dir, []ManifestEntry{entry})

	if _, err := LoadManifest(dir); err == nil {
		t.Fatal("expected an error for an empty state_id")
	}
}

func TestLoadManifest_RejectsNotAnArray(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ManifestFile), []byte(`{"captures":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManifest(dir); err == nil {
		t.Fatal("expected an error when manifest.json is not a bare JSON array")
	}
}

func TestLoadManifest_EmptyArrayIsValid(t *testing.T) {
	// Validation succeeds on an empty manifest — main.go's caller is where the
	// "zero captures is probably a broken renderer" policy decision lives, not
	// this package (LoadManifest only validates shape).
	dir := t.TempDir()
	writeManifest(t, dir, []ManifestEntry{})

	entries, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest() error = %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("len(entries) = %d, want 0", len(entries))
	}
}
