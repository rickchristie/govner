package clipboard

import "strings"

type darwinClipboardTarget struct {
	MIME            string
	Extension       string
	AppleScriptType string
	Markers         []string
}

type darwinClipboardMarker struct {
	MIME    string
	Markers []string
}

var darwinClipboardTargets = []darwinClipboardTarget{
	{
		MIME:            "image/png",
		Extension:       ".png",
		AppleScriptType: "\u00abclass PNGf\u00bb",
		Markers:         []string{"pngf", "png picture", "image/png"},
	},
	{
		MIME:            "image/tiff",
		Extension:       ".tiff",
		AppleScriptType: "TIFF picture",
		Markers:         []string{"tiff picture", "image/tiff", " tiff"},
	},
	{
		MIME:            "image/jpeg",
		Extension:       ".jpg",
		AppleScriptType: "JPEG picture",
		Markers:         []string{"jpeg picture", "image/jpeg", " jpeg"},
	},
	{
		MIME:            "image/gif",
		Extension:       ".gif",
		AppleScriptType: "GIF picture",
		Markers:         []string{"gif picture", "image/gif", " gif"},
	},
	{
		MIME:            "image/bmp",
		Extension:       ".bmp",
		AppleScriptType: "BMP picture",
		Markers:         []string{"bmp picture", "image/bmp", " bmp"},
	},
}

var darwinClipboardFallbackTargets = []darwinClipboardTarget{
	darwinClipboardTargets[1], // TIFF picture is the broadest coercion target.
	darwinClipboardTargets[0],
	darwinClipboardTargets[2],
	darwinClipboardTargets[3],
	darwinClipboardTargets[4],
}

var darwinClipboardImageLikeMarkers = []darwinClipboardMarker{
	{MIME: "application/pdf", Markers: []string{"pdf", "com.adobe.pdf", "application/pdf"}},
	{MIME: "image/jp2", Markers: []string{"jp2", "jpeg 2000", "image/jp2"}},
	{MIME: "image/vnd.adobe.photoshop", Markers: []string{"8bps", "photoshop", "image/vnd.adobe.photoshop"}},
	{MIME: "image/pict", Markers: []string{"pict", "quickdraw picture", "image/pict"}},
	{MIME: "image/heic", Markers: []string{"heic", "image/heic"}},
	{MIME: "image/avif", Markers: []string{"avif", "image/avif"}},
	{MIME: "image/svg+xml", Markers: []string{"svg", "image/svg+xml"}},
}

func darwinTargetsFromInfo(info string) []darwinClipboardTarget {
	normalized := strings.ToLower(strings.TrimSpace(info))
	if normalized == "" {
		return nil
	}

	var matches []darwinClipboardTarget
	for _, target := range darwinClipboardTargets {
		for _, marker := range target.Markers {
			if strings.Contains(normalized, marker) {
				matches = append(matches, target)
				break
			}
		}
	}
	return matches
}

func darwinReportedMIMEs(info string) []string {
	normalized := strings.ToLower(strings.TrimSpace(info))
	if normalized == "" {
		return nil
	}

	var mimes []string
	seen := make(map[string]bool)
	appendMIME := func(mime string) {
		if mime == "" || seen[mime] {
			return
		}
		seen[mime] = true
		mimes = append(mimes, mime)
	}

	for _, target := range darwinClipboardTargets {
		for _, marker := range target.Markers {
			if strings.Contains(normalized, marker) {
				appendMIME(target.MIME)
				break
			}
		}
	}
	for _, marker := range darwinClipboardImageLikeMarkers {
		for _, token := range marker.Markers {
			if strings.Contains(normalized, token) {
				appendMIME(marker.MIME)
				break
			}
		}
	}
	return mimes
}

func darwinReadTargets(info string) []darwinClipboardTarget {
	matches := darwinTargetsFromInfo(info)
	if len(matches) > 0 {
		return matches
	}
	if !darwinClipboardLooksImageLike(info) {
		return nil
	}
	return darwinClipboardFallbackTargets
}

func darwinClipboardLooksImageLike(info string) bool {
	normalized := strings.ToLower(strings.TrimSpace(info))
	if normalized == "" {
		return false
	}
	if strings.Contains(normalized, "image/") {
		return true
	}
	for _, marker := range darwinClipboardImageLikeMarkers {
		for _, token := range marker.Markers {
			if strings.Contains(normalized, token) {
				return true
			}
		}
	}
	return false
}

func darwinTargetMIMEs(targets []darwinClipboardTarget) []string {
	out := make([]string, 0, len(targets))
	for _, target := range targets {
		out = append(out, target.MIME)
	}
	return out
}
