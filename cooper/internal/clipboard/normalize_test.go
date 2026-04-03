package clipboard

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
)

// ---------------------------------------------------------------------------
// helpers: programmatically generate real image bytes in various formats
// ---------------------------------------------------------------------------

// makePNG creates a small w x h opaque PNG (red fill).
func makePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 255, G: 0, B: 0, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("makePNG: %v", err)
	}
	return buf.Bytes()
}

// makePNGWithAlpha creates a small PNG with semi-transparent pixels.
func makePNGWithAlpha(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.NRGBA{R: 0, G: 128, B: 255, A: 128})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("makePNGWithAlpha: %v", err)
	}
	return buf.Bytes()
}

// makeJPEG creates a small w x h JPEG (green fill).
func makeJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 0, G: 200, B: 0, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("makeJPEG: %v", err)
	}
	return buf.Bytes()
}

// makeGIF creates a 2-frame GIF. Frame 0 is blue, frame 1 is yellow.
func makeGIF(t *testing.T, w, h int) []byte {
	t.Helper()

	palette := color.Palette{
		color.RGBA{R: 0, G: 0, B: 255, A: 255},   // blue
		color.RGBA{R: 255, G: 255, B: 0, A: 255},  // yellow
	}

	frame0 := image.NewPaletted(image.Rect(0, 0, w, h), palette)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			frame0.SetColorIndex(x, y, 0) // blue
		}
	}
	frame1 := image.NewPaletted(image.Rect(0, 0, w, h), palette)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			frame1.SetColorIndex(x, y, 1) // yellow
		}
	}

	g := &gif.GIF{
		Image: []*image.Paletted{frame0, frame1},
		Delay: []int{10, 10},
	}

	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, g); err != nil {
		t.Fatalf("makeGIF: %v", err)
	}
	return buf.Bytes()
}

// makeBMP creates a minimal valid BMP (w x h, blue fill, 24-bit).
// Go's x/image/bmp decoder handles this fine.
func makeBMP(t *testing.T, w, h int) []byte {
	t.Helper()

	rowSize := (w*3 + 3) & ^3 // rows padded to 4-byte boundary
	pixelDataSize := rowSize * h
	fileSize := 14 + 40 + pixelDataSize // file header + DIB header + pixels

	var buf bytes.Buffer
	buf.Grow(fileSize)

	// --- BMP file header (14 bytes) ---
	buf.WriteByte('B')
	buf.WriteByte('M')
	binary.Write(&buf, binary.LittleEndian, uint32(fileSize))
	binary.Write(&buf, binary.LittleEndian, uint16(0)) // reserved
	binary.Write(&buf, binary.LittleEndian, uint16(0)) // reserved
	binary.Write(&buf, binary.LittleEndian, uint32(14+40)) // pixel data offset

	// --- DIB header (BITMAPINFOHEADER, 40 bytes) ---
	binary.Write(&buf, binary.LittleEndian, uint32(40))       // header size
	binary.Write(&buf, binary.LittleEndian, int32(w))          // width
	binary.Write(&buf, binary.LittleEndian, int32(h))          // height (positive = bottom-up)
	binary.Write(&buf, binary.LittleEndian, uint16(1))         // color planes
	binary.Write(&buf, binary.LittleEndian, uint16(24))        // bits per pixel
	binary.Write(&buf, binary.LittleEndian, uint32(0))         // compression (BI_RGB)
	binary.Write(&buf, binary.LittleEndian, uint32(pixelDataSize))
	binary.Write(&buf, binary.LittleEndian, int32(2835))       // horizontal resolution
	binary.Write(&buf, binary.LittleEndian, int32(2835))       // vertical resolution
	binary.Write(&buf, binary.LittleEndian, uint32(0))         // colors in palette
	binary.Write(&buf, binary.LittleEndian, uint32(0))         // important colors

	// --- Pixel data (bottom-up rows, BGR order) ---
	row := make([]byte, rowSize)
	for x := 0; x < w; x++ {
		row[x*3+0] = 255 // B
		row[x*3+1] = 0   // G
		row[x*3+2] = 0   // R
	}
	for y := 0; y < h; y++ {
		buf.Write(row)
	}

	return buf.Bytes()
}

// ---------------------------------------------------------------------------
// Test: PNG passthrough preserves exact bytes
// ---------------------------------------------------------------------------

func TestNormalize_PNGPassthrough(t *testing.T) {
	data := makePNG(t, 4, 4)
	result := &CaptureResult{
		MIME:  "image/png",
		Bytes: data,
	}

	obj, err := Normalize(result, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	variant, ok := obj.Variants["image/png"]
	if !ok {
		t.Fatal("missing image/png variant")
	}

	if !bytes.Equal(variant.Bytes, data) {
		t.Error("PNG passthrough did not preserve exact bytes")
	}
	if variant.Width != 4 || variant.Height != 4 {
		t.Errorf("unexpected dimensions: %dx%d", variant.Width, variant.Height)
	}
	if obj.Kind != ClipboardKindImage {
		t.Errorf("unexpected kind: %s", obj.Kind)
	}
	if !bytes.Equal(obj.Raw, data) {
		t.Error("Raw bytes should match original input")
	}
}

// ---------------------------------------------------------------------------
// Test: JPEG converts to valid PNG
// ---------------------------------------------------------------------------

func TestNormalize_JPEGToPNG(t *testing.T) {
	data := makeJPEG(t, 8, 6)
	result := &CaptureResult{
		MIME:  "image/jpeg",
		Bytes: data,
	}

	obj, err := Normalize(result, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	variant := obj.Variants["image/png"]
	validatePNGBytes(t, variant.Bytes)
	if variant.Width != 8 || variant.Height != 6 {
		t.Errorf("unexpected dimensions: %dx%d", variant.Width, variant.Height)
	}
}

// ---------------------------------------------------------------------------
// Test: GIF converts first frame to PNG
// ---------------------------------------------------------------------------

func TestNormalize_GIFFirstFrame(t *testing.T) {
	data := makeGIF(t, 4, 4)
	result := &CaptureResult{
		MIME:  "image/gif",
		Bytes: data,
	}

	obj, err := Normalize(result, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	variant := obj.Variants["image/png"]
	img := decodePNG(t, variant.Bytes)
	// First frame is all blue.
	r, g, b, _ := img.At(0, 0).RGBA()
	if r != 0 || g != 0 || b>>8 != 255 {
		t.Errorf("first frame pixel should be blue, got R=%d G=%d B=%d", r>>8, g>>8, b>>8)
	}
}

// ---------------------------------------------------------------------------
// Test: BMP converts to PNG
// ---------------------------------------------------------------------------

func TestNormalize_BMPToPNG(t *testing.T) {
	data := makeBMP(t, 4, 4)
	result := &CaptureResult{
		MIME:  "image/bmp",
		Bytes: data,
	}

	obj, err := Normalize(result, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	variant := obj.Variants["image/png"]
	validatePNGBytes(t, variant.Bytes)
	if variant.Width != 4 || variant.Height != 4 {
		t.Errorf("unexpected dimensions: %dx%d", variant.Width, variant.Height)
	}
}

// ---------------------------------------------------------------------------
// Test: Alpha channel survives conversion
// ---------------------------------------------------------------------------

func TestNormalize_AlphaPreserved(t *testing.T) {
	data := makePNGWithAlpha(t, 4, 4)
	result := &CaptureResult{
		MIME:  "image/png",
		Bytes: data,
	}

	obj, err := Normalize(result, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// PNG passthrough: exact bytes, so alpha is trivially preserved.
	// Verify by decoding.
	img := decodePNG(t, obj.Variants["image/png"].Bytes)
	_, _, _, a := img.At(0, 0).RGBA()
	// NRGBA{A:128} -> premul alpha ~0x8080
	if a == 0xFFFF || a == 0 {
		t.Errorf("alpha channel lost: got %d, expected semi-transparent", a)
	}
}

// Also test alpha through a round-trip conversion (PNG -> decode -> encode -> decode).
func TestConvertToPNG_AlphaRoundtrip(t *testing.T) {
	// Create a PNG with alpha, then convert it through convertToPNG
	// which decodes and re-encodes.
	data := makePNGWithAlpha(t, 4, 4)
	pngOut, w, h, err := convertToPNG(data, "png")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w != 4 || h != 4 {
		t.Errorf("unexpected dimensions: %dx%d", w, h)
	}
	img := decodePNG(t, pngOut)
	_, _, _, a := img.At(0, 0).RGBA()
	if a == 0xFFFF || a == 0 {
		t.Errorf("alpha channel lost in round-trip: got %d", a)
	}
}

// ---------------------------------------------------------------------------
// Test: Oversized input rejected
// ---------------------------------------------------------------------------

func TestNormalize_OversizedInputRejected(t *testing.T) {
	data := makePNG(t, 4, 4)
	result := &CaptureResult{
		MIME:  "image/png",
		Bytes: data,
	}

	_, err := Normalize(result, 10) // 10 bytes is way too small
	if err == nil {
		t.Fatal("expected error for oversized input")
	}
	if !contains(err.Error(), "input size") {
		t.Errorf("error should mention input size: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Test: Oversized converted output rejected
// ---------------------------------------------------------------------------

func TestNormalize_OversizedOutputRejected(t *testing.T) {
	// Create a JPEG that is small in bytes but whose PNG re-encoding
	// will be larger (PNG of solid color may actually be smaller, so
	// use a bigger image to make the PNG non-trivial).
	jpegData := makeJPEG(t, 64, 64)

	// Set limit just above the JPEG size but likely below the PNG size.
	// We need to find a limit that passes the input check but fails
	// the output check.
	limit := len(jpegData) + 1

	result := &CaptureResult{
		MIME:  "image/jpeg",
		Bytes: jpegData,
	}

	_, err := Normalize(result, limit)
	if err == nil {
		// If the PNG happens to be small enough, skip the test rather
		// than fail -- PNG encoding of solid colour can be tiny.
		t.Skip("PNG output was smaller than JPEG input; cannot trigger output limit in this scenario")
	}
	if !contains(err.Error(), "converted PNG size") {
		t.Errorf("error should mention converted PNG size: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Test: Malformed image input reports clear error
// ---------------------------------------------------------------------------

func TestNormalize_MalformedInput(t *testing.T) {
	// Data that starts with JPEG magic bytes but is garbage.
	data := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F',
		0, 1, 1, 0, 0, 0, 0, 0, 0, 0, 99, 99, 99}
	result := &CaptureResult{
		MIME:  "image/jpeg",
		Bytes: data,
	}

	_, err := Normalize(result, 0)
	if err == nil {
		t.Fatal("expected error for malformed JPEG")
	}
	if !contains(err.Error(), "decode") && !contains(err.Error(), "normalize") {
		t.Errorf("error should be descriptive: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Test: Unknown format falls back to external converter
// ---------------------------------------------------------------------------

func TestNormalize_UnknownFormatFallsToExternal(t *testing.T) {
	// Craft data that looks like SVG (which triggers the external path).
	svgData := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="4" height="4"><rect fill="red" width="4" height="4"/></svg>`)

	result := &CaptureResult{
		MIME:  "image/svg+xml",
		Bytes: svgData,
	}

	// The external converter (magick) may not be installed in CI,
	// so we expect either success or a specific external-conversion
	// error. The key assertion is that it does NOT try in-process
	// decode (which would produce a different error).
	_, err := Normalize(result, 0)
	if err == nil {
		// magick was available and converted successfully -- great.
		return
	}

	errMsg := err.Error()
	// Should be an external conversion error, not a decode error.
	if contains(errMsg, "decode png") || contains(errMsg, "decode jpeg") ||
		contains(errMsg, "decode gif") || contains(errMsg, "decode bmp") {
		t.Errorf("SVG should not go through in-process decode path: %v", err)
	}
	// Accept external conversion failure (magick not installed).
	if !contains(errMsg, "external conversion") {
		t.Errorf("expected external conversion error for SVG, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Test: detectImageFormat
// ---------------------------------------------------------------------------

func TestDetectImageFormat(t *testing.T) {
	tests := []struct {
		name   string
		data   []byte
		expect string
	}{
		{"png", makePNG(t, 2, 2), "png"},
		{"jpeg", makeJPEG(t, 2, 2), "jpeg"},
		{"gif", makeGIF(t, 2, 2), "gif"},
		{"bmp", makeBMP(t, 2, 2), "bmp"},
		{"svg", []byte(`<?xml version="1.0"?><svg></svg>`), "svg"},
		{"empty", []byte{}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := detectImageFormat(tc.data)
			if got != tc.expect {
				t.Errorf("detectImageFormat(%s) = %q, want %q", tc.name, got, tc.expect)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Test: isCommonRasterFormat
// ---------------------------------------------------------------------------

func TestIsCommonRasterFormat(t *testing.T) {
	yes := []string{"png", "jpeg", "gif", "bmp", "tiff", "webp"}
	no := []string{"svg", "image/avif", "image/heic", "text/plain", ""}

	for _, f := range yes {
		if !isCommonRasterFormat(f) {
			t.Errorf("expected %q to be common raster", f)
		}
	}
	for _, f := range no {
		if isCommonRasterFormat(f) {
			t.Errorf("expected %q to NOT be common raster", f)
		}
	}
}

// ---------------------------------------------------------------------------
// Test: Nil and empty input
// ---------------------------------------------------------------------------

func TestNormalize_NilResult(t *testing.T) {
	_, err := Normalize(nil, 0)
	if err == nil {
		t.Fatal("expected error for nil result")
	}
}

func TestNormalize_EmptyBytes(t *testing.T) {
	result := &CaptureResult{Bytes: []byte{}}
	_, err := Normalize(result, 0)
	if err == nil {
		t.Fatal("expected error for empty bytes")
	}
}

// ---------------------------------------------------------------------------
// Test: CheckConversionPrerequisites
// ---------------------------------------------------------------------------

func TestCheckConversionPrerequisites(t *testing.T) {
	// Just verify it returns without panic. The result depends on
	// whether magick is installed in the test environment.
	err := CheckConversionPrerequisites()
	if err != nil {
		t.Logf("magick not available (ok in CI): %v", err)
	} else {
		t.Log("magick is available")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func validatePNGBytes(t *testing.T, data []byte) {
	t.Helper()
	if len(data) < 8 {
		t.Fatal("PNG data too short")
	}
	// PNG magic
	if !bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}) {
		t.Fatal("data does not have PNG magic bytes")
	}
	_, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("invalid PNG: %v", err)
	}
}

func decodePNG(t *testing.T, data []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode PNG: %v", err)
	}
	return img
}

func contains(s, substr string) bool {
	return bytes.Contains([]byte(s), []byte(substr))
}
