package clipboard

import (
	"bytes"
	"fmt"
	"image/png"
)

// Normalize takes a CaptureResult and produces a ClipboardObject with
// a PNG variant. It detects the actual image format from the raw
// bytes (ignoring the MIME hint), converts to PNG if necessary, and
// enforces the maxBytes size limit on both input and output.
func Normalize(result *CaptureResult, maxBytes int) (*ClipboardObject, error) {
	if result == nil {
		return nil, fmt.Errorf("normalize: nil capture result")
	}
	if len(result.Bytes) == 0 {
		return nil, fmt.Errorf("normalize: empty capture data")
	}

	// Enforce input size limit.
	if maxBytes > 0 && len(result.Bytes) > maxBytes {
		return nil, fmt.Errorf(
			"normalize: input size %d bytes exceeds limit of %d bytes",
			len(result.Bytes), maxBytes,
		)
	}

	format := mimeToFormat(detectImageFormat(result.Bytes))
	if !isImageFormat(format) {
		format = mimeToFormat(result.MIME)
	}
	if !isImageFormat(format) {
		return nil, fmt.Errorf("normalize: clipboard payload is not a recognizable image")
	}

	var pngBytes []byte
	var width, height int

	switch {
	case format == "png":
		// Validate the PNG is decodable, but pass through the
		// original bytes to avoid any re-encoding artefacts.
		cfg, err := png.DecodeConfig(bytes.NewReader(result.Bytes))
		if err != nil {
			return nil, fmt.Errorf("normalize: invalid PNG data: %w", err)
		}
		pngBytes = result.Bytes
		width = cfg.Width
		height = cfg.Height

	case isCommonRasterFormat(format):
		// In-process conversion for jpeg, gif, bmp, tiff, webp.
		var err error
		pngBytes, width, height, err = convertToPNG(result.Bytes, format)
		if err != nil {
			return nil, fmt.Errorf("normalize: %w", err)
		}

	default:
		// Uncommon image format -- delegate to external converter.
		var err error
		pngBytes, err = convertExternalToPNG(result.Bytes)
		if err != nil {
			return nil, fmt.Errorf("normalize: %w", err)
		}
		// Parse the resulting PNG to get dimensions.
		cfg, cfgErr := png.DecodeConfig(bytes.NewReader(pngBytes))
		if cfgErr != nil {
			return nil, fmt.Errorf("normalize: external converter produced invalid PNG: %w", cfgErr)
		}
		width = cfg.Width
		height = cfg.Height
	}

	// Enforce output size limit.
	if maxBytes > 0 && len(pngBytes) > maxBytes {
		return nil, fmt.Errorf(
			"normalize: converted PNG size %d bytes exceeds limit of %d bytes",
			len(pngBytes), maxBytes,
		)
	}

	obj := &ClipboardObject{
		Kind:            ClipboardKindImage,
		MIME:            result.MIME,
		Filename:        result.Filename,
		Extension:       result.Extension,
		Raw:             result.Bytes,
		RawSize:         int64(len(result.Bytes)),
		OriginalTargets: result.OriginalTargets,
		Variants: map[string]ClipboardVariant{
			"image/png": {
				MIME:   "image/png",
				Bytes:  pngBytes,
				Size:   int64(len(pngBytes)),
				Width:  width,
				Height: height,
			},
		},
	}
	return obj, nil
}
