package renders

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadImage_ValidPNG(t *testing.T) {
	dir := t.TempDir()
	path := writePNG(t, dir, "dashboard.png", 10, 6)

	img, err := LoadImage(path)
	if err != nil {
		t.Fatalf("LoadImage() error = %v", err)
	}
	if img.Width != 10 || img.Height != 6 {
		t.Errorf("dims = %dx%d, want 10x6", img.Width, img.Height)
	}
	if len(img.SHA256) != 64 {
		t.Errorf("SHA256 = %q, want a 64-char hex digest", img.SHA256)
	}
	if len(img.Bytes) == 0 {
		t.Error("Bytes is empty")
	}
}

func TestLoadImage_NotAnImage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "not-an-image.png")
	if err := os.WriteFile(path, []byte("this is not a png"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadImage(path); err == nil {
		t.Fatal("expected an error for undecodable bytes")
	}
}

func TestLoadImage_MissingFile(t *testing.T) {
	if _, err := LoadImage(filepath.Join(t.TempDir(), "missing.png")); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}

func TestLoadImage_OversizeRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.png")
	if err := os.WriteFile(path, make([]byte, MaxImageBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadImage(path)
	if err == nil {
		t.Fatal("expected an error for an oversize file")
	}
	if !errors.Is(err, ErrImageTooLarge) {
		t.Errorf("error = %v, want wrapping ErrImageTooLarge", err)
	}
}
