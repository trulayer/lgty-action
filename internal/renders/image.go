package renders

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/png" // registers the PNG decoder used by image.DecodeConfig
	"os"
)

// MaxImageBytes mirrors lgty-backend's artifactstore.MaxArtifactBytes (5 MiB
// per capture, api/openapi.yaml "413" response). Checked locally so an
// oversize capture fails fast in the customer's own CI log instead of after
// a multi-megabyte upload the backend was always going to reject.
const MaxImageBytes = 5 << 20

// ErrImageTooLarge is returned when a capture exceeds MaxImageBytes.
var ErrImageTooLarge = errors.New("capture exceeds the per-image size limit")

// Image is a validated, decoded capture ready to upload.
type Image struct {
	Bytes  []byte
	Width  int
	Height int
	SHA256 string
}

// LoadImage reads path, decodes it as a PNG (magic bytes and full header
// decode — the same validation the backend performs at ingest, run here
// first so a corrupt or mislabeled file never leaves this machine), and
// returns its bytes, dimensions, and content digest.
func LoadImage(path string) (Image, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Image{}, fmt.Errorf("read %s: %w", path, err)
	}
	if len(raw) > MaxImageBytes {
		return Image{}, fmt.Errorf("%s: %d bytes: %w", path, len(raw), ErrImageTooLarge)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return Image{}, fmt.Errorf("%s: not a decodable image: %w", path, err)
	}
	if format != "png" {
		return Image{}, fmt.Errorf("%s: decoded as %q, not png", path, format)
	}
	sum := sha256.Sum256(raw)
	return Image{
		Bytes:  raw,
		Width:  cfg.Width,
		Height: cfg.Height,
		SHA256: hex.EncodeToString(sum[:]),
	}, nil
}
