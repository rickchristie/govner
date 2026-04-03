package clipboard

import (
	"bytes"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/http"
	"strings"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

// detectImageFormat inspects raw bytes to determine the image format.
// It uses magic-byte signatures first, then falls back to
// http.DetectContentType. Returns a short format name like "png",
// "jpeg", "gif", "bmp", "tiff", "webp", "svg", or the raw MIME
// string for anything else.
func detectImageFormat(data []byte) string {
	if len(data) == 0 {
		return ""
	}

	// Magic-byte checks for formats that http.DetectContentType may
	// not recognise or may label generically.
	if len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}) {
		return "png"
	}
	if len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return "jpeg"
	}
	if len(data) >= 6 && (string(data[:6]) == "GIF87a" || string(data[:6]) == "GIF89a") {
		return "gif"
	}
	if len(data) >= 2 && data[0] == 'B' && data[1] == 'M' {
		return "bmp"
	}
	// TIFF: little-endian (II) or big-endian (MM)
	if len(data) >= 4 &&
		((data[0] == 'I' && data[1] == 'I' && data[2] == 42 && data[3] == 0) ||
			(data[0] == 'M' && data[1] == 'M' && data[2] == 0 && data[3] == 42)) {
		return "tiff"
	}
	// WebP: RIFF....WEBP
	if len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return "webp"
	}
	// SVG: look for an opening tag near the start.
	if looksLikeSVG(data) {
		return "svg"
	}

	// Fall back to stdlib content detection.
	ct := http.DetectContentType(data)
	ct = strings.SplitN(ct, ";", 2)[0]
	ct = strings.TrimSpace(ct)

	switch ct {
	case "image/png":
		return "png"
	case "image/jpeg":
		return "jpeg"
	case "image/gif":
		return "gif"
	case "image/bmp":
		return "bmp"
	case "image/tiff":
		return "tiff"
	case "image/webp":
		return "webp"
	case "image/svg+xml":
		return "svg"
	default:
		if strings.HasPrefix(ct, "image/") {
			return ct // pass through as-is for uncommon image MIME
		}
		return ct
	}
}

// looksLikeSVG does a quick heuristic check for SVG content.
func looksLikeSVG(data []byte) bool {
	// Check the first 512 bytes for an <svg tag.
	limit := 512
	if len(data) < limit {
		limit = len(data)
	}
	prefix := strings.ToLower(string(data[:limit]))
	return strings.Contains(prefix, "<svg")
}

// isCommonRasterFormat returns true for formats we can decode
// in-process using stdlib + golang.org/x/image.
func isCommonRasterFormat(format string) bool {
	switch format {
	case "png", "jpeg", "gif", "bmp", "tiff", "webp":
		return true
	default:
		return false
	}
}

// convertToPNG decodes data in the given format and re-encodes it as
// PNG. For GIF images it extracts the first frame. Returns the PNG
// bytes, width, and height.
func convertToPNG(data []byte, format string) ([]byte, int, int, error) {
	var img image.Image
	var err error

	switch format {
	case "gif":
		// Use gif.DecodeAll to get the first frame; gif.Decode also
		// works for single-frame GIFs, but DecodeAll is explicit.
		gifImg, decErr := gif.DecodeAll(bytes.NewReader(data))
		if decErr != nil {
			return nil, 0, 0, fmt.Errorf("decode gif: %w", decErr)
		}
		if len(gifImg.Image) == 0 {
			return nil, 0, 0, fmt.Errorf("decode gif: no frames")
		}
		img = gifImg.Image[0]
	case "jpeg":
		img, err = jpeg.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, 0, 0, fmt.Errorf("decode jpeg: %w", err)
		}
	case "png":
		img, err = png.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, 0, 0, fmt.Errorf("decode png: %w", err)
		}
	default:
		// bmp, tiff, webp are registered via blank imports above,
		// so image.Decode will handle them automatically.
		img, _, err = image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, 0, 0, fmt.Errorf("decode %s: %w", format, err)
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, 0, 0, fmt.Errorf("encode png: %w", err)
	}

	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()
	return buf.Bytes(), w, h, nil
}
