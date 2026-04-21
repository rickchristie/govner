package clipboard

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestXclipShimValidBash verifies the generated xclip shim is syntactically valid bash.
func TestXclipShimValidBash(t *testing.T) {
	script := XclipShim("/usr/bin/xclip")
	assertValidBash(t, script, "xclip")
}

// TestXclipShimContainsTARGETSInterception checks for TARGETS query interception.
func TestXclipShimContainsTARGETSInterception(t *testing.T) {
	script := XclipShim("/usr/bin/xclip")
	assertContains(t, script, "TARGETS", "xclip shim should intercept TARGETS query")
	assertContains(t, script, "image/png", "xclip shim should respond with image/png in TARGETS")
}

// TestXclipShimContainsImageInterception checks for image/png read interception.
func TestXclipShimContainsImageInterception(t *testing.T) {
	script := XclipShim("/usr/bin/xclip")
	assertContains(t, script, "image/*", "xclip shim should match image/* pattern")
	assertContains(t, script, "_cooper_clip_fetch", "xclip shim should call _cooper_clip_fetch for image reads")
}

// TestXclipShimContainsFallback checks that the shim falls back to real xclip.
func TestXclipShimContainsFallback(t *testing.T) {
	script := XclipShim("/usr/bin/xclip")
	assertContains(t, script, `exec "$REAL_BINARY" "$@"`, "xclip shim should fall back to real binary")
	assertContains(t, script, `REAL_BINARY="/usr/bin/xclip"`, "xclip shim should reference the real binary path")
}

// TestXclipShimUsesBinarySafeFetch verifies the mktemp pattern for binary-safe transfer.
func TestXclipShimUsesBinarySafeFetch(t *testing.T) {
	script := XclipShim("/usr/bin/xclip")
	assertContains(t, script, "mktemp", "xclip shim should use mktemp for binary-safe transfer")
	assertContains(t, script, "curl", "xclip shim should use curl to fetch image")
	assertContains(t, script, `cat "$tmpfile"`, "xclip shim should cat tmpfile to stdout")
	assertContains(t, script, `rm -f "$tmpfile"`, "xclip shim should clean up tmpfile")
}

func TestXclipShimContainsTextWriteForwarding(t *testing.T) {
	script := XclipShim("/usr/bin/xclip")
	assertContains(t, script, "/clipboard/text", "xclip shim should post clipboard writes to /clipboard/text")
	assertContains(t, script, "_cooper_xclip_is_clipboard_write", "xclip shim should detect clipboard writes")
	assertContains(t, script, "_cooper_clip_post_text_file", "xclip shim should use shared text post helper")
	assertContains(t, script, "--data-binary", "xclip shim should post raw clipboard bytes")
}

// TestWlPasteShimValidBash verifies the generated wl-paste shim is syntactically valid bash.
func TestWlPasteShimValidBash(t *testing.T) {
	script := WlPasteShim("/usr/bin/wl-paste")
	assertValidBash(t, script, "wl-paste")
}

// TestWlPasteShimContainsListTypesInterception checks for --list-types interception.
func TestWlPasteShimContainsListTypesInterception(t *testing.T) {
	script := WlPasteShim("/usr/bin/wl-paste")
	assertContains(t, script, "--list-types", "wl-paste shim should intercept --list-types")
	assertContains(t, script, "image/png", "wl-paste shim should respond with image/png")
}

// TestWlPasteShimContainsTypeImageInterception checks for --type image/* interception.
func TestWlPasteShimContainsTypeImageInterception(t *testing.T) {
	script := WlPasteShim("/usr/bin/wl-paste")
	assertContains(t, script, "--type*image/*", "wl-paste shim should match --type image/* pattern")
	assertContains(t, script, "_cooper_clip_fetch", "wl-paste shim should call _cooper_clip_fetch for image reads")
}

// TestWlPasteShimContainsFallback checks that the shim falls back to real wl-paste.
func TestWlPasteShimContainsFallback(t *testing.T) {
	script := WlPasteShim("/usr/bin/wl-paste")
	assertContains(t, script, `exec "$REAL_BINARY" "$@"`, "wl-paste shim should fall back to real binary")
	assertContains(t, script, `REAL_BINARY="/usr/bin/wl-paste"`, "wl-paste shim should reference the real binary path")
}

// TestXselShimValidBash verifies the generated xsel shim is syntactically valid bash.
func TestXselShimValidBash(t *testing.T) {
	script := XselShim("/usr/bin/xsel")
	assertValidBash(t, script, "xsel")
}

// TestXselShimContainsFallback checks that the xsel shim falls back to real xsel.
func TestXselShimContainsFallback(t *testing.T) {
	script := XselShim("/usr/bin/xsel")
	assertContains(t, script, `exec "$REAL_BINARY" "$@"`, "xsel shim should fall back to real binary")
	assertContains(t, script, `REAL_BINARY="/usr/bin/xsel"`, "xsel shim should reference the real binary path")
}

func TestXselShimContainsTextWriteForwarding(t *testing.T) {
	script := XselShim("/usr/bin/xsel")
	assertContains(t, script, "/clipboard/text", "xsel shim should post clipboard writes to /clipboard/text")
	assertContains(t, script, "_cooper_xsel_is_clipboard_write", "xsel shim should detect clipboard writes")
	assertContains(t, script, "/usr/bin/xclip -selection clipboard", "xsel shim should mirror text into the local X11 clipboard")
	assertContains(t, script, "--data-binary", "xsel shim should post raw clipboard bytes")
}

// TestAllShimsReferenceTokenFile ensures all shims read the token from the file,
// not from environment variables or command-line arguments.
func TestAllShimsReferenceTokenFile(t *testing.T) {
	shims := map[string]string{
		"xclip":    XclipShim("/usr/bin/xclip"),
		"xsel":     XselShim("/usr/bin/xsel"),
		"wl-paste": WlPasteShim("/usr/bin/wl-paste"),
	}

	for name, script := range shims {
		assertContains(t, script, "COOPER_CLIPBOARD_TOKEN_FILE",
			"%s shim should reference COOPER_CLIPBOARD_TOKEN_FILE", name)
		assertContains(t, script, `cat "${COOPER_CLIPBOARD_TOKEN_FILE}"`,
			"%s shim should read token from file with cat", name)
	}
}

// TestAllShimsReferenceBridgeURL ensures all shims use COOPER_CLIPBOARD_BRIDGE_URL.
func TestAllShimsReferenceBridgeURL(t *testing.T) {
	shims := map[string]string{
		"xclip":    XclipShim("/usr/bin/xclip"),
		"xsel":     XselShim("/usr/bin/xsel"),
		"wl-paste": WlPasteShim("/usr/bin/wl-paste"),
	}

	for name, script := range shims {
		assertContains(t, script, "COOPER_CLIPBOARD_BRIDGE_URL",
			"%s shim should reference COOPER_CLIPBOARD_BRIDGE_URL", name)
	}
}

// TestNoShimContainsTokenInArgs ensures no shim passes the token via command-line
// arguments (which would be visible in /proc and ps output).
func TestNoShimContainsTokenInArgs(t *testing.T) {
	shims := map[string]string{
		"xclip":    XclipShim("/usr/bin/xclip"),
		"xsel":     XselShim("/usr/bin/xsel"),
		"wl-paste": WlPasteShim("/usr/bin/wl-paste"),
	}

	for name, script := range shims {
		// The token should only appear in the Authorization header, read from file.
		// It should never be interpolated as a curl argument outside -H.
		lines := strings.Split(script, "\n")
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			// Skip comments and the cat-from-file line.
			if strings.HasPrefix(trimmed, "#") {
				continue
			}
			// Ensure token is never passed as a bare curl argument (e.g., --token $token).
			if strings.Contains(trimmed, "curl") && strings.Contains(trimmed, "$token") &&
				!strings.Contains(trimmed, "-H") && !strings.Contains(trimmed, "Authorization") {
				t.Errorf("%s shim line %d passes token as bare curl argument: %s", name, i+1, trimmed)
			}
		}
	}
}

// TestAllShimsCheckEnabledGuard ensures all shims check COOPER_CLIPBOARD_ENABLED
// before intercepting anything.
func TestAllShimsCheckEnabledGuard(t *testing.T) {
	shims := map[string]string{
		"xclip":    XclipShim("/usr/bin/xclip"),
		"xsel":     XselShim("/usr/bin/xsel"),
		"wl-paste": WlPasteShim("/usr/bin/wl-paste"),
	}

	for name, script := range shims {
		assertContains(t, script, "COOPER_CLIPBOARD_ENABLED",
			"%s shim should check COOPER_CLIPBOARD_ENABLED", name)
	}
}

// TestShimsPreserveCustomPath verifies the real binary path is correctly embedded.
func TestShimsPreserveCustomPath(t *testing.T) {
	script := XclipShim("/opt/custom/bin/xclip")
	assertContains(t, script, `REAL_BINARY="/opt/custom/bin/xclip"`,
		"xclip shim should embed the provided real binary path")

	script = XselShim("/opt/custom/bin/xsel")
	assertContains(t, script, `REAL_BINARY="/opt/custom/bin/xsel"`,
		"xsel shim should embed the provided real binary path")

	script = WlPasteShim("/opt/custom/bin/wl-paste")
	assertContains(t, script, `REAL_BINARY="/opt/custom/bin/wl-paste"`,
		"wl-paste shim should embed the provided real binary path")
}

// assertValidBash writes the script to a tmpfile and runs bash -n to verify syntax.
func assertValidBash(t *testing.T, script, name string) {
	t.Helper()

	f, err := os.CreateTemp("", "cooper-shim-test-*.sh")
	if err != nil {
		t.Fatalf("failed to create temp file for %s syntax check: %v", name, err)
	}
	defer os.Remove(f.Name())

	if _, err := f.WriteString(script); err != nil {
		f.Close()
		t.Fatalf("failed to write %s script to temp file: %v", name, err)
	}
	f.Close()

	cmd := exec.Command("bash", "-n", f.Name())
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Errorf("%s shim is not valid bash:\n%s\n\nScript:\n%s", name, string(output), script)
	}
}

// assertContains is a test helper that checks if a string contains a substring.
func assertContains(t *testing.T, s, substr string, msgAndArgs ...interface{}) {
	t.Helper()
	if !strings.Contains(s, substr) {
		msg := "expected string to contain substring"
		if len(msgAndArgs) > 0 {
			msg = msgAndArgs[0].(string)
			if len(msgAndArgs) > 1 {
				msg = strings.ReplaceAll(msg, "%s", msgAndArgs[1].(string))
			}
		}
		t.Errorf("%s\n  substring: %q\n  not found in script", msg, substr)
	}
}
