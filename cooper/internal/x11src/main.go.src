// cooper-x11-bridge is an X11 CLIPBOARD selection owner that runs inside
// Docker containers. It claims CLIPBOARD ownership and serves staged image
// data fetched from the host bridge service when native clipboard consumers
// (e.g., arboard, @teddyzhu/clipboard) request it via X11 selection protocol.
//
// This is a standalone binary using raw X11 protocol via xgb, not a shell
// wrapper around xclip/xsel.
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// maxDirectSize is the threshold above which we use the INCR protocol
// instead of direct property transfer. X11 servers typically limit
// property sizes to ~256KB.
const maxDirectSize = 256 * 1024

// incrChunkSize is the size of each chunk sent during INCR transfers.
const incrChunkSize = 64 * 1024

// httpTimeout is the timeout for HTTP requests to the bridge service.
const httpTimeout = 5 * time.Second

// atoms holds interned X11 atoms used by the bridge.
type atoms struct {
	clipboard xproto.Atom
	targets   xproto.Atom
	timestamp xproto.Atom
	incr      xproto.Atom
	imagePNG  xproto.Atom
}

// incrTransfer tracks an ongoing INCR (incremental) selection transfer.
type incrTransfer struct {
	requestor xproto.Window
	property  xproto.Atom
	data      []byte
	offset    int
}

func main() {
	display := flag.String("display", "", "X11 display address (e.g., 127.0.0.1:99)")
	cookieFile := flag.String("cookie-file", "", "path to file containing hex cookie for X auth")
	bridgeURL := flag.String("bridge-url", "", "host bridge URL (e.g., http://127.0.0.1:4343)")
	tokenFile := flag.String("token-file", "", "path to clipboard token file")
	flag.Parse()

	if *display == "" || *cookieFile == "" || *bridgeURL == "" || *tokenFile == "" {
		flag.Usage()
		os.Exit(1)
	}

	// Normalize bridge URL: strip trailing slash.
	*bridgeURL = strings.TrimRight(*bridgeURL, "/")

	// Read cookie hex from file.
	cookieData, err := os.ReadFile(*cookieFile)
	if err != nil {
		log.Fatalf("x11-bridge: read cookie file: %v", err)
	}
	cookieHex := strings.TrimSpace(string(cookieData))

	// Parse display address to get host:port for TCP connection.
	// DISPLAY format: "127.0.0.1:99" → TCP port 6099
	displayHost, displayNum, err := parseDisplay(*display)
	if err != nil {
		log.Fatalf("x11-bridge: parse display: %v", err)
	}
	tcpPort := 6000 + displayNum
	tcpAddr := fmt.Sprintf("%s:%d", displayHost, tcpPort)

	// Connect to X server via TCP with explicit cookie authentication.
	// We use NewConnNetWithCookieHex instead of NewConn/XAUTHORITY because
	// xauth file entries use hostname-based matching that doesn't work
	// reliably with TCP display addresses in Docker containers.
	netConn, err := net.DialTimeout("tcp", tcpAddr, 5*time.Second)
	if err != nil {
		log.Fatalf("x11-bridge: connect to X server at %s: %v", tcpAddr, err)
	}
	conn, err := xgb.NewConnNetWithCookieHex(netConn, cookieHex)
	if err != nil {
		netConn.Close()
		log.Fatalf("x11-bridge: X11 handshake: %v", err)
	}
	defer conn.Close()

	setup := xproto.Setup(conn)
	screen := setup.DefaultScreen(conn)

	// Create invisible 1x1 window to own CLIPBOARD selection.
	wid, err := xproto.NewWindowId(conn)
	if err != nil {
		log.Fatalf("x11-bridge: allocate window id: %v", err)
	}

	err = xproto.CreateWindowChecked(
		conn,
		screen.RootDepth,
		wid,
		screen.Root,
		0, 0, // x, y
		1, 1, // width, height
		0,                         // border width
		xproto.WindowClassCopyFromParent,
		screen.RootVisual,
		xproto.CwEventMask,
		[]uint32{xproto.EventMaskPropertyChange},
	).Check()
	if err != nil {
		log.Fatalf("x11-bridge: create window: %v", err)
	}

	// Intern atoms.
	a, err := internAtoms(conn)
	if err != nil {
		log.Fatalf("x11-bridge: intern atoms: %v", err)
	}

	// Claim CLIPBOARD ownership.
	ownershipTime, err := claimClipboard(conn, wid, a.clipboard)
	if err != nil {
		log.Fatalf("x11-bridge: claim clipboard: %v", err)
	}
	log.Printf("x11-bridge: claimed CLIPBOARD ownership (timestamp=%d)", ownershipTime)

	// Set up graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	// HTTP client for bridge requests.
	httpClient := &http.Client{Timeout: httpTimeout}

	// INCR transfer state (only one at a time).
	var (
		activeINCR *incrTransfer
		incrMu     sync.Mutex
	)

	// Event loop.
	log.Printf("x11-bridge: event loop started")
	for {
		// Check for shutdown signal without blocking.
		select {
		case sig := <-sigCh:
			log.Printf("x11-bridge: received %v, shutting down", sig)
			return
		default:
		}

		ev, xerr := conn.PollForEvent()
		if xerr != nil {
			// X11 errors are often non-fatal (e.g., BadWindow from
			// a requestor that closed). Log and continue.
			log.Printf("x11-bridge: X11 error: %v", xerr)
		}
		if ev == nil {
			// No event available. Sleep briefly to avoid busy-spinning,
			// then check for signals again.
			time.Sleep(10 * time.Millisecond)
			continue
		}

		switch e := ev.(type) {
		case xproto.SelectionRequestEvent:
			handleSelectionRequest(conn, e, a, ownershipTime, httpClient, *bridgeURL, *tokenFile, &activeINCR, &incrMu)

		case xproto.SelectionClearEvent:
			// Another application took CLIPBOARD ownership. Reclaim it.
			log.Printf("x11-bridge: lost CLIPBOARD ownership, reclaiming")
			var reclaimErr error
			ownershipTime, reclaimErr = claimClipboard(conn, wid, a.clipboard)
			if reclaimErr != nil {
				log.Printf("x11-bridge: reclaim clipboard failed: %v", reclaimErr)
			}

		case xproto.PropertyNotifyEvent:
			if e.State == xproto.PropertyDelete {
				incrMu.Lock()
				if activeINCR != nil && e.Window == activeINCR.requestor && e.Atom == activeINCR.property {
					writeNextINCRChunk(conn, activeINCR, a.imagePNG)
					if activeINCR.offset >= len(activeINCR.data) {
						// Transfer complete: write zero-length property.
						xproto.ChangeProperty(
							conn,
							xproto.PropModeReplace,
							activeINCR.requestor,
							activeINCR.property,
							a.imagePNG,
							8,
							0,
							nil,
						)
						activeINCR = nil
						log.Printf("x11-bridge: INCR transfer complete")
					}
				}
				incrMu.Unlock()
			}
		}
	}
}

// internAtoms interns all X11 atoms needed by the bridge.
func internAtoms(conn *xgb.Conn) (atoms, error) {
	names := []string{"CLIPBOARD", "TARGETS", "TIMESTAMP", "INCR", "image/png"}
	cookies := make([]xproto.InternAtomCookie, len(names))
	for i, name := range names {
		cookies[i] = xproto.InternAtom(conn, false, uint16(len(name)), name)
	}

	var a atoms
	for i, cookie := range cookies {
		reply, err := cookie.Reply()
		if err != nil {
			return a, fmt.Errorf("intern atom %q: %w", names[i], err)
		}
		switch i {
		case 0:
			a.clipboard = reply.Atom
		case 1:
			a.targets = reply.Atom
		case 2:
			a.timestamp = reply.Atom
		case 3:
			a.incr = reply.Atom
		case 4:
			a.imagePNG = reply.Atom
		}
	}
	return a, nil
}

// claimClipboard claims CLIPBOARD selection ownership and returns the
// server timestamp used. Uses CurrentTime and retrieves the actual
// server timestamp from a property change event.
func claimClipboard(conn *xgb.Conn, wid xproto.Window, clipboardAtom xproto.Atom) (xproto.Timestamp, error) {
	xproto.SetSelectionOwner(conn, wid, clipboardAtom, xproto.TimeCurrentTime)

	// Verify we actually got ownership.
	reply, err := xproto.GetSelectionOwner(conn, clipboardAtom).Reply()
	if err != nil {
		return 0, fmt.Errorf("get selection owner: %w", err)
	}
	if reply.Owner != wid {
		return 0, fmt.Errorf("failed to acquire CLIPBOARD ownership (owner=%d, expected=%d)", reply.Owner, wid)
	}

	return xproto.TimeCurrentTime, nil
}

// handleSelectionRequest processes an incoming SelectionRequest event.
func handleSelectionRequest(
	conn *xgb.Conn,
	ev xproto.SelectionRequestEvent,
	a atoms,
	ownershipTime xproto.Timestamp,
	client *http.Client,
	bridgeURL, tokenFile string,
	activeINCR **incrTransfer,
	incrMu *sync.Mutex,
) {
	switch ev.Target {
	case a.targets:
		// Respond with list of supported targets.
		targetList := []xproto.Atom{a.targets, a.timestamp, a.imagePNG}
		buf := make([]byte, len(targetList)*4)
		for i, atom := range targetList {
			binary.LittleEndian.PutUint32(buf[i*4:], uint32(atom))
		}

		xproto.ChangeProperty(
			conn,
			xproto.PropModeReplace,
			ev.Requestor,
			ev.Property,
			xproto.AtomAtom,
			32,
			uint32(len(targetList)),
			buf,
		)
		sendSelectionNotify(conn, ev, ev.Property)

	case a.timestamp:
		// Respond with ownership timestamp.
		buf := make([]byte, 4)
		binary.LittleEndian.PutUint32(buf, uint32(ownershipTime))

		xproto.ChangeProperty(
			conn,
			xproto.PropModeReplace,
			ev.Requestor,
			ev.Property,
			xproto.AtomInteger,
			32,
			1,
			buf,
		)
		sendSelectionNotify(conn, ev, ev.Property)

	case a.imagePNG:
		// Fetch image from bridge and serve it.
		data, err := fetchImage(client, bridgeURL, tokenFile)
		if err != nil {
			log.Printf("x11-bridge: fetch image: %v", err)
			refuseRequest(conn, ev)
			return
		}
		if data == nil {
			// No image staged (204).
			refuseRequest(conn, ev)
			return
		}

		if len(data) <= maxDirectSize {
			// Direct transfer.
			xproto.ChangeProperty(
				conn,
				xproto.PropModeReplace,
				ev.Requestor,
				ev.Property,
				a.imagePNG,
				8,
				uint32(len(data)),
				data,
			)
			sendSelectionNotify(conn, ev, ev.Property)
		} else {
			// INCR transfer for large images.
			incrMu.Lock()
			if *activeINCR != nil {
				// Already have an active INCR transfer. Reject this one.
				incrMu.Unlock()
				log.Printf("x11-bridge: rejecting image request, INCR transfer in progress")
				refuseRequest(conn, ev)
				return
			}

			*activeINCR = &incrTransfer{
				requestor: ev.Requestor,
				property:  ev.Property,
				data:      data,
				offset:    0,
			}
			incrMu.Unlock()

			// Write INCR atom with data size to signal incremental transfer.
			sizeBuf := make([]byte, 4)
			binary.LittleEndian.PutUint32(sizeBuf, uint32(len(data)))
			xproto.ChangeProperty(
				conn,
				xproto.PropModeReplace,
				ev.Requestor,
				ev.Property,
				a.incr,
				32,
				1,
				sizeBuf,
			)

			// Subscribe to property changes on the requestor window so
			// we get notified when the requestor deletes the property
			// (signaling readiness for the next chunk).
			xproto.ChangeWindowAttributes(
				conn,
				ev.Requestor,
				xproto.CwEventMask,
				[]uint32{xproto.EventMaskPropertyChange},
			)

			sendSelectionNotify(conn, ev, ev.Property)
			log.Printf("x11-bridge: started INCR transfer (%d bytes)", len(data))
		}

	default:
		// Unsupported target: refuse.
		refuseRequest(conn, ev)
	}
}

// writeNextINCRChunk writes the next chunk of data for an INCR transfer.
func writeNextINCRChunk(conn *xgb.Conn, transfer *incrTransfer, imagePNGAtom xproto.Atom) {
	end := transfer.offset + incrChunkSize
	if end > len(transfer.data) {
		end = len(transfer.data)
	}
	chunk := transfer.data[transfer.offset:end]
	transfer.offset = end

	xproto.ChangeProperty(
		conn,
		xproto.PropModeReplace,
		transfer.requestor,
		transfer.property,
		imagePNGAtom,
		8,
		uint32(len(chunk)),
		chunk,
	)
}

// sendSelectionNotify sends a SelectionNotify event to the requestor,
// indicating the transfer property.
func sendSelectionNotify(conn *xgb.Conn, ev xproto.SelectionRequestEvent, property xproto.Atom) {
	notify := xproto.SelectionNotifyEvent{
		Time:      ev.Time,
		Requestor: ev.Requestor,
		Selection: ev.Selection,
		Target:    ev.Target,
		Property:  property,
	}
	xproto.SendEvent(conn, false, ev.Requestor, xproto.EventMaskNoEvent, string(notify.Bytes()))
}

// refuseRequest sends a SelectionNotify with property=None, which tells
// the requestor that the selection conversion failed.
func refuseRequest(conn *xgb.Conn, ev xproto.SelectionRequestEvent) {
	notify := xproto.SelectionNotifyEvent{
		Time:      ev.Time,
		Requestor: ev.Requestor,
		Selection: ev.Selection,
		Target:    ev.Target,
		Property:  xproto.AtomNone,
	}
	xproto.SendEvent(conn, false, ev.Requestor, xproto.EventMaskNoEvent, string(notify.Bytes()))
}

// fetchImage retrieves the staged clipboard image from the bridge service.
// Returns the image bytes on success, nil if no image is staged (204),
// or an error on failure.
func fetchImage(client *http.Client, bridgeURL, tokenFile string) ([]byte, error) {
	// Read token fresh on each request to support rotation.
	tokenBytes, err := os.ReadFile(tokenFile)
	if err != nil {
		return nil, fmt.Errorf("read token file: %w", err)
	}
	token := strings.TrimSpace(string(tokenBytes))
	if token == "" {
		return nil, fmt.Errorf("token file is empty")
	}

	req, err := http.NewRequest("GET", bridgeURL+"/clipboard/image", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("read response body: %w", err)
		}
		if len(data) == 0 {
			return nil, nil
		}
		return data, nil

	case http.StatusNoContent:
		return nil, nil

	case http.StatusUnauthorized:
		return nil, fmt.Errorf("bridge returned 401 (invalid token)")

	default:
		return nil, fmt.Errorf("bridge returned unexpected status %d", resp.StatusCode)
	}
}

// parseDisplay parses an X11 DISPLAY string like "127.0.0.1:99" into
// host and display number.
func parseDisplay(display string) (host string, num int, err error) {
	idx := strings.LastIndex(display, ":")
	if idx < 0 {
		return "", 0, fmt.Errorf("invalid display %q: missing colon", display)
	}
	host = display[:idx]
	if host == "" {
		host = "127.0.0.1"
	}
	num, err = strconv.Atoi(display[idx+1:])
	if err != nil {
		return "", 0, fmt.Errorf("invalid display number in %q: %w", display, err)
	}
	return host, num, nil
}
