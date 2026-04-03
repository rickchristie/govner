//go:build integration

// Tests for the cooper-x11-bridge binary. These require Xvfb to be installed
// and are gated behind the "integration" build tag.
//
// Run with:
//
//	go test -tags integration -v -count=1 ./cmd/cooper-x11-bridge/
//
// Prerequisites: Xvfb, xclip (for xclip-based tests), xauth
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// nextDisplay is an atomic counter to ensure each test gets a unique display.
var nextDisplay atomic.Int32

func init() {
	nextDisplay.Store(99)
}

func uniqueDisplay() int {
	return int(nextDisplay.Add(1))
}

// skipIfNoXvfb skips the test if Xvfb is not available on the system.
func skipIfNoXvfb(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("Xvfb"); err != nil {
		t.Skip("Xvfb not available, skipping integration test")
	}
}

// startXvfb starts an Xvfb server on the given display number, creates an
// Xauthority file with a cookie, and registers cleanup. Returns the path
// to the Xauthority file.
func startXvfb(t *testing.T, display int) string {
	t.Helper()

	// Create Xauthority file with a cookie for this display.
	xauthFile := filepath.Join(t.TempDir(), ".Xauthority")

	// Use xauth to generate a cookie. If xauth is missing we still try
	// to run Xvfb without authentication (some CI environments allow it).
	if xauthBin, err := exec.LookPath("xauth"); err == nil {
		cmd := exec.Command(xauthBin, "-f", xauthFile, "add",
			fmt.Sprintf(":%d", display), ".", "deadbeefdeadbeefdeadbeefdeadbeef")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("xauth add failed: %v\n%s", err, out)
		}
	}

	cmd := exec.Command("Xvfb", fmt.Sprintf(":%d", display),
		"-screen", "0", "1024x768x24",
		"-auth", xauthFile,
		"-nolisten", "tcp",
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start Xvfb on display :%d: %v", display, err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
	})

	// Wait for the X server socket to appear.
	socketPath := fmt.Sprintf("/tmp/.X11-unix/X%d", display)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(socketPath); err == nil {
			return xauthFile
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("Xvfb did not create socket %s within timeout", socketPath)
	return ""
}

// startMockBridge starts an HTTP server that mimics the Cooper host bridge
// clipboard endpoints. It serves the given imageData on GET /clipboard/image
// with Bearer token validation.
//
// If imageData is nil, the server returns 204 No Content on image requests.
// The expectedToken is "test-token-secret" unless overridden.
func startMockBridge(t *testing.T, imageData []byte, opts ...mockBridgeOpt) (url string, port int) {
	t.Helper()

	cfg := mockBridgeCfg{
		expectedToken: "test-token-secret",
		statusOnImage: http.StatusOK,
	}
	for _, o := range opts {
		o(&cfg)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/clipboard/image", func(w http.ResponseWriter, r *http.Request) {
		// Validate auth.
		auth := r.Header.Get("Authorization")
		if auth != "Bearer "+cfg.expectedToken {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"invalid token"}`))
			return
		}

		if cfg.statusOnImage != http.StatusOK {
			w.WriteHeader(cfg.statusOnImage)
			return
		}

		if imageData == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		w.Write(imageData)
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	port = ln.Addr().(*net.TCPAddr).Port
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })

	url = fmt.Sprintf("http://127.0.0.1:%d", port)
	return url, port
}

type mockBridgeCfg struct {
	expectedToken string
	statusOnImage int
}

type mockBridgeOpt func(*mockBridgeCfg)

func withRejectToken() mockBridgeOpt {
	return func(cfg *mockBridgeCfg) {
		// Any token will be rejected by returning 401. We do this by
		// setting the expected token to something that won't match.
		cfg.expectedToken = "REJECT-ALL-TOKENS"
	}
}

// writeTokenFile creates a temp file containing the given token and returns
// its path.
func writeTokenFile(t *testing.T, token string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "clipboard-token")
	if err := os.WriteFile(path, []byte(token), 0600); err != nil {
		t.Fatalf("failed to write token file: %v", err)
	}
	return path
}

// buildBridge compiles the cooper-x11-bridge binary and returns the path to
// it. The binary is built once per test run via t.TempDir caching.
func buildBridge(t *testing.T) string {
	t.Helper()
	binPath := filepath.Join(t.TempDir(), "cooper-x11-bridge")
	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/cooper-x11-bridge/")
	cmd.Dir = cooperRoot()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to build cooper-x11-bridge: %v\n%s", err, out)
	}
	return binPath
}

// cooperRoot returns the root directory of the cooper module.
func cooperRoot() string {
	return "/home/ricky/Personal/govner/cooper"
}

// startBridge starts the cooper-x11-bridge binary as a subprocess and returns
// the exec.Cmd. The process is killed on test cleanup.
func startBridge(t *testing.T, binPath string, display int, xauthFile, bridgeURL, tokenFile string) *exec.Cmd {
	t.Helper()

	cmd := exec.Command(binPath,
		"--display", fmt.Sprintf(":%d", display),
		"--xauthority", xauthFile,
		"--bridge-url", bridgeURL,
		"--token-file", tokenFile,
	)
	cmd.Stdout = os.Stderr // Let bridge logs appear in test output.
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start bridge: %v", err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
	})

	// Give the bridge time to connect and claim CLIPBOARD.
	time.Sleep(500 * time.Millisecond)
	return cmd
}

// connectX11 connects to the given X11 display using xgb with the provided
// Xauthority file and returns the connection.
func connectX11(t *testing.T, display int, xauthFile string) *xgb.Conn {
	t.Helper()

	// Set env for xgb's connection.
	prev := os.Getenv("DISPLAY")
	prevAuth := os.Getenv("XAUTHORITY")
	os.Setenv("DISPLAY", fmt.Sprintf(":%d", display))
	os.Setenv("XAUTHORITY", xauthFile)
	t.Cleanup(func() {
		os.Setenv("DISPLAY", prev)
		os.Setenv("XAUTHORITY", prevAuth)
	})

	conn, err := xgb.NewConn()
	if err != nil {
		t.Fatalf("failed to connect to X11 display :%d: %v", display, err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// internAtom interns a single X11 atom by name.
func internAtom(t *testing.T, conn *xgb.Conn, name string) xproto.Atom {
	t.Helper()
	reply, err := xproto.InternAtom(conn, false, uint16(len(name)), name).Reply()
	if err != nil {
		t.Fatalf("failed to intern atom %q: %v", name, err)
	}
	return reply.Atom
}

// generateTestPNG creates a valid PNG image of the specified approximate size
// in bytes (actual size may be slightly larger due to PNG encoding).
func generateTestPNG(t *testing.T, approxSize int) []byte {
	t.Helper()

	// Estimate dimensions needed. Each pixel in an uncompressed RGBA row
	// is 4 bytes, but PNG compresses. For a roughly uniform-colored image
	// PNG compresses extremely well, so we use random-ish patterns.
	// A practical approach: generate an image and check size, then adjust.
	// For simplicity, we create a wide image with varied pixel data.

	// Start with a reasonable estimate: PNG compression on varied data
	// usually achieves ~60-80% of raw size. We target slightly larger.
	width := 256
	height := approxSize / (width * 3) // rough estimate for RGB
	if height < 1 {
		height = 1
	}

	for attempts := 0; attempts < 10; attempts++ {
		img := image.NewRGBA(image.Rect(0, 0, width, height))
		// Fill with varied data to defeat compression.
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				img.SetRGBA(x, y, color.RGBA{
					R: uint8((x*7 + y*13 + attempts*37) & 0xFF),
					G: uint8((x*11 + y*17 + attempts*41) & 0xFF),
					B: uint8((x*23 + y*29 + attempts*43) & 0xFF),
					A: 255,
				})
			}
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			t.Fatalf("failed to encode PNG: %v", err)
		}
		if buf.Len() >= approxSize {
			return buf.Bytes()
		}
		// Image was too small due to compression. Increase height.
		height = height * approxSize / buf.Len()
		height = height * 12 / 10 // add 20% margin
	}

	// Last resort: generate a large image directly.
	img := image.NewRGBA(image.Rect(0, 0, width, height*2))
	for y := 0; y < height*2; y++ {
		for x := 0; x < width; x++ {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8((x*7 + y*13) & 0xFF),
				G: uint8((x*11 + y*17) & 0xFF),
				B: uint8((x*23 + y*29) & 0xFF),
				A: 255,
			})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("failed to encode PNG: %v", err)
	}
	return buf.Bytes()
}

// requestSelectionViaXgb sends a ConvertSelection request for the given target
// atom on CLIPBOARD, waits for the SelectionNotify event, and reads the
// property data. Returns the property data, the type atom, and the format.
// Returns nil data if the selection was refused (property=None).
func requestSelectionViaXgb(
	t *testing.T,
	conn *xgb.Conn,
	clipboardAtom, targetAtom xproto.Atom,
	timeout time.Duration,
) (data []byte, propType xproto.Atom, format byte) {
	t.Helper()

	setup := xproto.Setup(conn)
	screen := setup.DefaultScreen(conn)

	// Create a window to receive the selection.
	wid, err := xproto.NewWindowId(conn)
	if err != nil {
		t.Fatalf("failed to allocate window id: %v", err)
	}
	err = xproto.CreateWindowChecked(
		conn,
		screen.RootDepth,
		wid,
		screen.Root,
		0, 0, 1, 1, 0,
		xproto.WindowClassCopyFromParent,
		screen.RootVisual,
		xproto.CwEventMask,
		[]uint32{xproto.EventMaskPropertyChange},
	).Check()
	if err != nil {
		t.Fatalf("failed to create requestor window: %v", err)
	}

	// Use a custom property name for the transfer.
	xselDataAtom := internAtom(t, conn, "XSEL_DATA")

	// Request the selection.
	xproto.ConvertSelection(conn, wid, clipboardAtom, targetAtom, xselDataAtom, xproto.TimeCurrentTime)

	// Wait for SelectionNotify.
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ev, _ := conn.PollForEvent()
		if ev == nil {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		notify, ok := ev.(xproto.SelectionNotifyEvent)
		if !ok {
			continue
		}
		if notify.Property == xproto.AtomNone {
			// Selection was refused.
			return nil, 0, 0
		}

		// Read the property.
		return readPropertyFull(t, conn, wid, notify.Property)
	}
	t.Fatalf("timed out waiting for SelectionNotify")
	return nil, 0, 0
}

// readPropertyFull reads an entire X11 property, handling both direct and INCR
// transfers. For INCR transfers, it reads chunks until a zero-length property
// is written.
func readPropertyFull(t *testing.T, conn *xgb.Conn, wid xproto.Window, prop xproto.Atom) (data []byte, propType xproto.Atom, format byte) {
	t.Helper()

	incrAtom := internAtom(t, conn, "INCR")

	// Read initial property.
	reply, err := xproto.GetProperty(conn, true, // delete after reading
		wid, prop, xproto.GetPropertyTypeAny, 0, 1<<20, // up to ~4MB
	).Reply()
	if err != nil {
		t.Fatalf("failed to read property: %v", err)
	}

	// Check if this is an INCR transfer.
	if reply.Type == incrAtom {
		// INCR protocol: the initial property contains the total size as
		// a 32-bit integer. Then we wait for PropertyNotify(NewValue)
		// events, read each chunk (deleting the property), until we get
		// a zero-length chunk.
		totalSize := uint32(0)
		if len(reply.Value) >= 4 {
			totalSize = binary.LittleEndian.Uint32(reply.Value[:4])
		}
		_ = totalSize

		var allData []byte
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			ev, _ := conn.PollForEvent()
			if ev == nil {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			pn, ok := ev.(xproto.PropertyNotifyEvent)
			if !ok {
				continue
			}
			if pn.Window != wid || pn.Atom != prop || pn.State != xproto.PropertyNewValue {
				continue
			}

			// Read and delete the chunk.
			chunkReply, err := xproto.GetProperty(conn, true,
				wid, prop, xproto.GetPropertyTypeAny, 0, 1<<20,
			).Reply()
			if err != nil {
				t.Fatalf("failed to read INCR chunk: %v", err)
			}
			if len(chunkReply.Value) == 0 {
				// End of INCR transfer. Return the type/format from
				// the data chunks, not the zero-length terminator.
				return allData, propType, format
			}
			allData = append(allData, chunkReply.Value...)
			propType = chunkReply.Type
			format = byte(chunkReply.Format)
		}
		t.Fatalf("timed out waiting for INCR transfer to complete")
		return nil, 0, 0
	}

	// Direct transfer.
	return reply.Value, reply.Type, byte(reply.Format)
}

// --------------------------------------------------------------------------
// Tests
// --------------------------------------------------------------------------

// TestX11Bridge_ClaimOwnership verifies that the bridge claims CLIPBOARD
// selection ownership on the X server.
func TestX11Bridge_ClaimOwnership(t *testing.T) {
	skipIfNoXvfb(t)

	display := uniqueDisplay()
	xauthFile := startXvfb(t, display)

	bridgeURL, _ := startMockBridge(t, nil) // no image needed
	tokenFile := writeTokenFile(t, "test-token-secret")
	binPath := buildBridge(t)

	startBridge(t, binPath, display, xauthFile, bridgeURL, tokenFile)

	// Connect as a client and verify CLIPBOARD owner.
	conn := connectX11(t, display, xauthFile)
	clipboardAtom := internAtom(t, conn, "CLIPBOARD")

	reply, err := xproto.GetSelectionOwner(conn, clipboardAtom).Reply()
	if err != nil {
		t.Fatalf("GetSelectionOwner failed: %v", err)
	}
	if reply.Owner == xproto.WindowNone {
		t.Fatal("CLIPBOARD has no owner, expected bridge to own it")
	}
	t.Logf("CLIPBOARD owner window: %d", reply.Owner)
}

// TestX11Bridge_TargetsResponse verifies that requesting TARGETS from the
// bridge returns the expected list of supported atoms.
func TestX11Bridge_TargetsResponse(t *testing.T) {
	skipIfNoXvfb(t)

	display := uniqueDisplay()
	xauthFile := startXvfb(t, display)

	bridgeURL, _ := startMockBridge(t, nil)
	tokenFile := writeTokenFile(t, "test-token-secret")
	binPath := buildBridge(t)

	startBridge(t, binPath, display, xauthFile, bridgeURL, tokenFile)

	conn := connectX11(t, display, xauthFile)
	clipboardAtom := internAtom(t, conn, "CLIPBOARD")
	targetsAtom := internAtom(t, conn, "TARGETS")

	data, propType, format := requestSelectionViaXgb(t, conn, clipboardAtom, targetsAtom, 5*time.Second)
	if data == nil {
		t.Fatal("TARGETS request was refused")
	}

	// The response should be a list of atoms (32-bit values).
	if format != 32 {
		t.Fatalf("expected format 32, got %d", format)
	}
	if propType != xproto.AtomAtom {
		t.Fatalf("expected type ATOM, got %d", propType)
	}

	// Parse atoms from the response.
	if len(data)%4 != 0 {
		t.Fatalf("unexpected data length %d (not multiple of 4)", len(data))
	}
	atomNames := make(map[string]bool)
	for i := 0; i < len(data); i += 4 {
		atomID := xproto.Atom(binary.LittleEndian.Uint32(data[i : i+4]))
		nameReply, err := xproto.GetAtomName(conn, atomID).Reply()
		if err != nil {
			t.Logf("could not get name for atom %d: %v", atomID, err)
			continue
		}
		atomNames[nameReply.Name] = true
	}

	t.Logf("TARGETS returned: %v", atomNames)

	// Verify expected atoms are present.
	for _, expected := range []string{"TARGETS", "TIMESTAMP", "image/png"} {
		if !atomNames[expected] {
			t.Errorf("expected %q in TARGETS response, not found", expected)
		}
	}
}

// TestX11Bridge_SmallImageTransfer verifies that a small PNG image (below the
// INCR threshold) can be read from the clipboard via the bridge.
func TestX11Bridge_SmallImageTransfer(t *testing.T) {
	skipIfNoXvfb(t)

	display := uniqueDisplay()
	xauthFile := startXvfb(t, display)

	// Generate a small test PNG (<256KB).
	testPNG := generateTestPNG(t, 50*1024) // ~50KB
	t.Logf("test PNG size: %d bytes", len(testPNG))

	bridgeURL, _ := startMockBridge(t, testPNG)
	tokenFile := writeTokenFile(t, "test-token-secret")
	binPath := buildBridge(t)

	startBridge(t, binPath, display, xauthFile, bridgeURL, tokenFile)

	conn := connectX11(t, display, xauthFile)
	clipboardAtom := internAtom(t, conn, "CLIPBOARD")
	imagePNGAtom := internAtom(t, conn, "image/png")

	data, _, _ := requestSelectionViaXgb(t, conn, clipboardAtom, imagePNGAtom, 10*time.Second)
	if data == nil {
		t.Fatal("image/png request was refused")
	}

	if !bytes.Equal(data, testPNG) {
		t.Errorf("received image data does not match: got %d bytes, want %d bytes", len(data), len(testPNG))
		// Check prefix to aid debugging.
		if len(data) > 0 && len(testPNG) > 0 {
			maxCmp := 16
			if len(data) < maxCmp {
				maxCmp = len(data)
			}
			t.Logf("first %d bytes received: %x", maxCmp, data[:maxCmp])
			maxCmp2 := 16
			if len(testPNG) < maxCmp2 {
				maxCmp2 = len(testPNG)
			}
			t.Logf("first %d bytes expected: %x", maxCmp2, testPNG[:maxCmp2])
		}
	}
}

// TestX11Bridge_LargeImageINCR verifies that a large PNG image (above the
// 256KB INCR threshold) is transferred correctly using the INCR protocol.
func TestX11Bridge_LargeImageINCR(t *testing.T) {
	skipIfNoXvfb(t)

	display := uniqueDisplay()
	xauthFile := startXvfb(t, display)

	// Generate a PNG larger than maxDirectSize (256KB).
	testPNG := generateTestPNG(t, 400*1024) // ~400KB
	if len(testPNG) <= maxDirectSize {
		t.Fatalf("test PNG is %d bytes, need >%d to trigger INCR", len(testPNG), maxDirectSize)
	}
	t.Logf("test PNG size: %d bytes (should trigger INCR)", len(testPNG))

	bridgeURL, _ := startMockBridge(t, testPNG)
	tokenFile := writeTokenFile(t, "test-token-secret")
	binPath := buildBridge(t)

	startBridge(t, binPath, display, xauthFile, bridgeURL, tokenFile)

	conn := connectX11(t, display, xauthFile)
	clipboardAtom := internAtom(t, conn, "CLIPBOARD")
	imagePNGAtom := internAtom(t, conn, "image/png")

	data, _, _ := requestSelectionViaXgb(t, conn, clipboardAtom, imagePNGAtom, 15*time.Second)
	if data == nil {
		t.Fatal("image/png request was refused (expected INCR transfer)")
	}

	if !bytes.Equal(data, testPNG) {
		t.Errorf("INCR transfer data mismatch: got %d bytes, want %d bytes", len(data), len(testPNG))
	}
}

// TestX11Bridge_ServiceDown verifies that when the bridge HTTP service is
// unreachable, clipboard image requests are refused (not hung).
func TestX11Bridge_ServiceDown(t *testing.T) {
	skipIfNoXvfb(t)

	display := uniqueDisplay()
	xauthFile := startXvfb(t, display)

	// Point to a port that nothing listens on.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}
	deadPort := ln.Addr().(*net.TCPAddr).Port
	ln.Close() // Close immediately so nothing is listening.

	tokenFile := writeTokenFile(t, "test-token-secret")
	binPath := buildBridge(t)

	startBridge(t, binPath, display, xauthFile, fmt.Sprintf("http://127.0.0.1:%d", deadPort), tokenFile)

	conn := connectX11(t, display, xauthFile)
	clipboardAtom := internAtom(t, conn, "CLIPBOARD")
	imagePNGAtom := internAtom(t, conn, "image/png")

	// The bridge should refuse the request (return property=None) rather
	// than hanging. We use a generous timeout but expect a quick refusal.
	start := time.Now()
	data, _, _ := requestSelectionViaXgb(t, conn, clipboardAtom, imagePNGAtom, 10*time.Second)
	elapsed := time.Since(start)

	if data != nil {
		t.Fatal("expected request to be refused when bridge service is down")
	}
	// The bridge has a 5s HTTP timeout. Allow some margin but ensure we
	// didn't hang forever.
	if elapsed > 8*time.Second {
		t.Errorf("request took %v, expected it to fail within HTTP timeout", elapsed)
	}
	t.Logf("request correctly refused after %v", elapsed)
}

// TestX11Bridge_InvalidToken verifies that when the bridge service returns 401
// (invalid token), the clipboard image request is refused.
func TestX11Bridge_InvalidToken(t *testing.T) {
	skipIfNoXvfb(t)

	display := uniqueDisplay()
	xauthFile := startXvfb(t, display)

	// The mock bridge will reject all tokens.
	bridgeURL, _ := startMockBridge(t, []byte("should-not-be-served"), withRejectToken())
	tokenFile := writeTokenFile(t, "test-token-secret")
	binPath := buildBridge(t)

	startBridge(t, binPath, display, xauthFile, bridgeURL, tokenFile)

	conn := connectX11(t, display, xauthFile)
	clipboardAtom := internAtom(t, conn, "CLIPBOARD")
	imagePNGAtom := internAtom(t, conn, "image/png")

	data, _, _ := requestSelectionViaXgb(t, conn, clipboardAtom, imagePNGAtom, 5*time.Second)
	if data != nil {
		t.Fatal("expected request to be refused with invalid token, but got data")
	}
	t.Log("request correctly refused due to invalid token")
}
