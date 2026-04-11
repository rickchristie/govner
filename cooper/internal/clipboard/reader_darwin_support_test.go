package clipboard

import (
	"strings"
	"testing"
)

func TestDarwinTargetsFromInfoPrefersPNG(t *testing.T) {
	info := `{{class PNGf, 42}, {TIFF picture, 84}, {Unicode text, 7}}`
	targets := darwinTargetsFromInfo(info)
	if len(targets) < 2 {
		t.Fatalf("darwinTargetsFromInfo() returned %d targets, want at least 2", len(targets))
	}
	if targets[0].MIME != "image/png" {
		t.Fatalf("first target MIME = %q, want image/png", targets[0].MIME)
	}
	if targets[1].MIME != "image/tiff" {
		t.Fatalf("second target MIME = %q, want image/tiff", targets[1].MIME)
	}
}

func TestDarwinTargetsFromInfoNoImage(t *testing.T) {
	targets := darwinTargetsFromInfo(`{{Unicode text, 12}, {utxt, 12}}`)
	if len(targets) != 0 {
		t.Fatalf("darwinTargetsFromInfo() = %v, want no image targets", targets)
	}
}

func TestDarwinClipboardLooksImageLike(t *testing.T) {
	tests := []struct {
		name string
		info string
		want bool
	}{
		{name: "pdf", info: `{{PDF, 12}, {com.adobe.pdf, 12}}`, want: true},
		{name: "jp2", info: `{{jp2, 12}}`, want: true},
		{name: "photoshop", info: `{{8BPS, 12}}`, want: true},
		{name: "text only", info: `{{Unicode text, 12}}`, want: false},
	}

	for _, tt := range tests {
		if got := darwinClipboardLooksImageLike(tt.info); got != tt.want {
			t.Fatalf("%s: darwinClipboardLooksImageLike() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestDarwinReadTargetsFallsBackForImageLikeClipboard(t *testing.T) {
	targets := darwinReadTargets(`{{PDF, 12}, {com.adobe.pdf, 12}}`)
	if len(targets) == 0 {
		t.Fatal("darwinReadTargets() returned no fallback targets for PDF clipboard data")
	}
	if targets[0].MIME != "image/tiff" {
		t.Fatalf("first fallback target MIME = %q, want image/tiff", targets[0].MIME)
	}
}

func TestDarwinReportedMIMEsIncludesImageLikeMarkers(t *testing.T) {
	mimes := darwinReportedMIMEs(`{{class PNGf, 42}, {PDF, 12}, {8BPS, 77}}`)
	joined := strings.Join(mimes, ",")
	for _, want := range []string{"image/png", "application/pdf", "image/vnd.adobe.photoshop"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("darwinReportedMIMEs() = %v, missing %q", mimes, want)
		}
	}
}
