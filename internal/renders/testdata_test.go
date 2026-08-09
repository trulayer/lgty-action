package renders

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// writePNG writes a minimal, validly-decodable w x h PNG to dir/name and
// returns its path. Shared by every test in this package that needs a real
// image file rather than a mocked one.
func writePNG(t *testing.T, dir, name string, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 1, G: 2, B: 3, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode test png: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write test png: %v", err)
	}
	return path
}

func validManifestEntry(file string) ManifestEntry {
	return ManifestEntry{
		File:    file,
		StateID: "dashboard",
		CaptureKey: ManifestKey{
			ViewportWidth: 1280, ViewportHeight: 720, DeviceScaleFactor: 1,
			ColorScheme: "light", BrowserEngine: "chromium", BrowserVersion: "128.0.0.0",
		},
	}
}
