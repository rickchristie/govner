package clipboard

import (
	"testing"
)

func TestDetectImageFormat_Public(t *testing.T) {
	tests := []struct {
		name   string
		data   []byte
		expect string
	}{
		{"PNG", makePNG(t, 1, 1), "png"},
		{"JPEG", makeJPEG(t, 1, 1), "jpeg"},
		{"GIF", makeGIF(t, 1, 1), "gif"},
		{"BMP", makeBMP(t, 1, 1), "bmp"},
		{"empty", []byte{}, ""},
		{"non-image text", []byte("hello world"), "text/plain"},
		{"random garbage", []byte{0x01, 0x02, 0x03, 0x04, 0x05}, "application/octet-stream"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectImageFormat(tt.data)
			if got != tt.expect {
				t.Errorf("DetectImageFormat() = %q, want %q", got, tt.expect)
			}
		})
	}
}

func TestIsImageData(t *testing.T) {
	tests := []struct {
		name   string
		data   []byte
		expect bool
	}{
		{"valid PNG", makePNG(t, 2, 2), true},
		{"valid JPEG", makeJPEG(t, 2, 2), true},
		{"valid GIF", makeGIF(t, 2, 2), true},
		{"valid BMP", makeBMP(t, 2, 2), true},
		{"text data", []byte("hello world"), false},
		{"empty data", []byte{}, false},
		{"SVG data", []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"></svg>`), false},
		{"random garbage", []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x11, 0x22}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsImageData(tt.data)
			if got != tt.expect {
				t.Errorf("IsImageData() = %v, want %v", got, tt.expect)
			}
		})
	}
}

func TestFormatToMIME(t *testing.T) {
	tests := []struct {
		format string
		expect string
	}{
		{"png", "image/png"},
		{"jpeg", "image/jpeg"},
		{"gif", "image/gif"},
		{"bmp", "image/bmp"},
		{"tiff", "image/tiff"},
		{"webp", "image/webp"},
		{"svg", "image/svg+xml"},
		{"unknown", "application/octet-stream"},
		{"", "application/octet-stream"},
	}
	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			got := FormatToMIME(tt.format)
			if got != tt.expect {
				t.Errorf("FormatToMIME(%q) = %q, want %q", tt.format, got, tt.expect)
			}
		})
	}
}
