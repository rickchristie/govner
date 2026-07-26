// cooper-x11-bridge is an X11 CLIPBOARD selection owner that runs inside
// Docker containers. It claims CLIPBOARD ownership and serves staged image
// data fetched from the host bridge service when native clipboard consumers
// (e.g., arboard, @teddyzhu/clipboard) request it via X11 selection protocol.
//
// This is a standalone binary using raw X11 protocol via xgb, not a shell
// wrapper around xclip/xsel.
package main

import (
	"context"
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

// incrIdleTimeout bounds how long an abandoned request may retain image data.
// Clipboard consumers can disappear at any point in the X11 handshake, so an
// INCR transfer must not remain globally active forever.
const incrIdleTimeout = 5 * time.Second

// incrSweepInterval controls abandoned-transfer cleanup. Transfer progress is
// event-driven; this ticker is only for bounded resource reclamation.
const incrSweepInterval = 250 * time.Millisecond

// maxActiveINCRTransfers limits retained clipboard copies if a client creates
// requestor windows without completing or closing them.
const maxActiveINCRTransfers = 8

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

type incrTransferKey struct {
	requestor xproto.Window
	property  xproto.Atom
}

// incrTransfer tracks one incremental selection transfer. finalChunkSent is
// intentionally separate from offset: ICCCM requires the owner to wait for the
// requestor to delete the final data chunk before writing the zero-length
// terminator.
type incrTransfer struct {
	data           []byte
	offset         int
	finalChunkSent bool
	lastActivity   time.Time
}

type incrStep struct {
	data       []byte
	terminator bool
}

// incrTransferSet is owned exclusively by the X11 event loop. X11 requestors
// choose both their window and transfer property, so that pair is the transfer
// identity. Tracking a set rather than one global transfer prevents an
// abandoned paste from blocking every later large-image request.
type incrTransferSet struct {
	active      map[incrTransferKey]*incrTransfer
	idleTimeout time.Duration
	maxActive   int
}

func newINCRTransferSet(idleTimeout time.Duration, maxActive int) *incrTransferSet {
	return &incrTransferSet{
		active:      make(map[incrTransferKey]*incrTransfer),
		idleTimeout: idleTimeout,
		maxActive:   maxActive,
	}
}

// start registers a transfer. A request that reuses its own window/property
// replaces the older attempt, while unrelated requests are bounded by
// maxActive. It returns whether an older same-key transfer was replaced and
// whether the new transfer was accepted.
func (s *incrTransferSet) start(key incrTransferKey, data []byte, now time.Time) (bool, bool) {
	_, replaced := s.active[key]
	if !replaced && s.maxActive > 0 && len(s.active) >= s.maxActive {
		return false, false
	}

	s.active[key] = &incrTransfer{
		data:         data,
		lastActivity: now,
	}
	return replaced, true
}

func (s *incrTransferSet) cancel(key incrTransferKey) bool {
	if _, ok := s.active[key]; !ok {
		return false
	}
	delete(s.active, key)
	return true
}

// next acknowledges a requestor property deletion. Data chunks and the
// terminator are separate steps so the final chunk remains readable until the
// requestor explicitly deletes it.
func (s *incrTransferSet) next(key incrTransferKey, now time.Time) (incrStep, bool) {
	transfer, ok := s.active[key]
	if !ok {
		return incrStep{}, false
	}
	transfer.lastActivity = now

	if transfer.finalChunkSent {
		delete(s.active, key)
		return incrStep{terminator: true}, true
	}

	end := transfer.offset + incrChunkSize
	if end > len(transfer.data) {
		end = len(transfer.data)
	}
	data := transfer.data[transfer.offset:end]
	transfer.offset = end
	transfer.finalChunkSent = transfer.offset == len(transfer.data)
	return incrStep{data: data}, true
}

func (s *incrTransferSet) expire(now time.Time) int {
	if s.idleTimeout <= 0 {
		return 0
	}

	expired := 0
	for key, transfer := range s.active {
		if now.Sub(transfer.lastActivity) < s.idleTimeout {
			continue
		}
		delete(s.active, key)
		expired++
	}
	return expired
}

func (s *incrTransferSet) removeWindow(window xproto.Window) int {
	removed := 0
	for key := range s.active {
		if key.requestor != window {
			continue
		}
		delete(s.active, key)
		removed++
	}
	return removed
}

type xEventResult struct {
	event xgb.Event
	err   xgb.Error
}

// pumpXEvents turns xgb's blocking event wait into a channel the main loop can
// select alongside signals and cleanup ticks. xgb already serializes wire
// reads internally, so this removes the old 10 ms polling delay without adding
// another X11 reader.
func pumpXEvents(ctx context.Context, conn *xgb.Conn) <-chan xEventResult {
	results := make(chan xEventResult, 128)
	go func() {
		defer close(results)
		for {
			event, xerr := conn.WaitForEvent()
			if event == nil && xerr == nil {
				return
			}
			select {
			case results <- xEventResult{event: event, err: xerr}:
			case <-ctx.Done():
				return
			}
		}
	}()
	return results
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
	tcpAddr := net.JoinHostPort(displayHost, strconv.Itoa(tcpPort))

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
	eventCtx, stopEventPump := context.WithCancel(context.Background())
	defer func() {
		stopEventPump()
		conn.Close()
	}()

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
		0, // border width
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
	defer signal.Stop(sigCh)

	// HTTP client for bridge requests.
	httpClient := &http.Client{Timeout: httpTimeout}

	transfers := newINCRTransferSet(incrIdleTimeout, maxActiveINCRTransfers)
	xEvents := pumpXEvents(eventCtx, conn)
	sweepTicker := time.NewTicker(incrSweepInterval)
	defer sweepTicker.Stop()

	// Event loop.
	log.Printf("x11-bridge: event loop started")
	for {
		select {
		case sig := <-sigCh:
			log.Printf("x11-bridge: received %v, shutting down", sig)
			return

		case result, ok := <-xEvents:
			if !ok {
				log.Printf("x11-bridge: X11 connection closed, shutting down")
				return
			}
			if result.err != nil {
				// X11 errors can be caused by requestors disappearing between
				// an event and a property write. DestroyNotify and idle
				// expiry reclaim their transfer state.
				log.Printf("x11-bridge: X11 error: %v", result.err)
				continue
			}

			switch e := result.event.(type) {
			case xproto.SelectionRequestEvent:
				handleSelectionRequest(conn, e, a, ownershipTime, httpClient, *bridgeURL, *tokenFile, transfers)

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
					handleINCRPropertyDelete(conn, e, a.imagePNG, transfers)
				}

			case xproto.DestroyNotifyEvent:
				if removed := transfers.removeWindow(e.Window); removed > 0 {
					log.Printf("x11-bridge: discarded %d INCR transfer(s) for closed requestor", removed)
				}
			}

		case now := <-sweepTicker.C:
			if expired := transfers.expire(now); expired > 0 {
				log.Printf("x11-bridge: expired %d abandoned INCR transfer(s)", expired)
			}
		}
	}
}

func handleINCRPropertyDelete(
	conn *xgb.Conn,
	event xproto.PropertyNotifyEvent,
	imagePNGAtom xproto.Atom,
	transfers *incrTransferSet,
) {
	key := incrTransferKey{requestor: event.Window, property: event.Atom}
	step, ok := transfers.next(key, time.Now())
	if !ok {
		return
	}

	if step.terminator {
		xproto.ChangeProperty(
			conn,
			xproto.PropModeReplace,
			key.requestor,
			key.property,
			imagePNGAtom,
			8,
			0,
			nil,
		)
		log.Printf("x11-bridge: INCR transfer complete")
		return
	}

	// ICCCM calls for appending each data chunk after the requestor deletes
	// the previous property. The request is intentionally unchecked: a
	// round-trip here can exceed clipboard consumers' very short per-chunk
	// deadlines. Asynchronous X11 errors still arrive through the event pump.
	xproto.ChangeProperty(
		conn,
		xproto.PropModeAppend,
		key.requestor,
		key.property,
		imagePNGAtom,
		8,
		uint32(len(step.data)),
		step.data,
	)
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
	transfers *incrTransferSet,
) {
	switch ev.Target {
	case a.targets:
		// Respond with list of supported targets.
		targetList := []xproto.Atom{a.targets, a.timestamp, a.imagePNG}
		buf := make([]byte, len(targetList)*4)
		for i, atom := range targetList {
			binary.LittleEndian.PutUint32(buf[i*4:], uint32(atom))
		}

		if err := xproto.ChangePropertyChecked(
			conn,
			xproto.PropModeReplace,
			ev.Requestor,
			ev.Property,
			xproto.AtomAtom,
			32,
			uint32(len(targetList)),
			buf,
		).Check(); err != nil {
			log.Printf("x11-bridge: write TARGETS response: %v", err)
			refuseRequest(conn, ev)
			return
		}
		if err := sendSelectionNotify(conn, ev, ev.Property); err != nil {
			log.Printf("x11-bridge: notify TARGETS response: %v", err)
		}

	case a.timestamp:
		// Respond with ownership timestamp.
		buf := make([]byte, 4)
		binary.LittleEndian.PutUint32(buf, uint32(ownershipTime))

		if err := xproto.ChangePropertyChecked(
			conn,
			xproto.PropModeReplace,
			ev.Requestor,
			ev.Property,
			xproto.AtomInteger,
			32,
			1,
			buf,
		).Check(); err != nil {
			log.Printf("x11-bridge: write TIMESTAMP response: %v", err)
			refuseRequest(conn, ev)
			return
		}
		if err := sendSelectionNotify(conn, ev, ev.Property); err != nil {
			log.Printf("x11-bridge: notify TIMESTAMP response: %v", err)
		}

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

		key := incrTransferKey{requestor: ev.Requestor, property: ev.Property}
		if len(data) <= maxDirectSize {
			transfers.cancel(key)
			// Direct transfer.
			if err := xproto.ChangePropertyChecked(
				conn,
				xproto.PropModeReplace,
				ev.Requestor,
				ev.Property,
				a.imagePNG,
				8,
				uint32(len(data)),
				data,
			).Check(); err != nil {
				log.Printf("x11-bridge: write direct image response: %v", err)
				refuseRequest(conn, ev)
				return
			}
			if err := sendSelectionNotify(conn, ev, ev.Property); err != nil {
				log.Printf("x11-bridge: notify direct image response: %v", err)
			}
		} else {
			// INCR transfer for large images.
			replaced, accepted := transfers.start(key, data, time.Now())
			if !accepted {
				log.Printf("x11-bridge: rejecting image request, too many INCR transfers")
				refuseRequest(conn, ev)
				return
			}
			if replaced {
				log.Printf("x11-bridge: replaced previous INCR transfer for requestor")
			}

			// Subscribe before publishing the INCR property. Intra-connection
			// request ordering then guarantees the requestor cannot acknowledge
			// the property before we are listening for its deletion.
			eventMask := uint32(xproto.EventMaskPropertyChange | xproto.EventMaskStructureNotify)
			if err := xproto.ChangeWindowAttributesChecked(
				conn,
				ev.Requestor,
				xproto.CwEventMask,
				[]uint32{eventMask},
			).Check(); err != nil {
				transfers.cancel(key)
				log.Printf("x11-bridge: subscribe to INCR requestor: %v", err)
				refuseRequest(conn, ev)
				return
			}

			// Write INCR atom with data size to signal incremental transfer.
			sizeBuf := make([]byte, 4)
			binary.LittleEndian.PutUint32(sizeBuf, uint32(len(data)))
			if err := xproto.ChangePropertyChecked(
				conn,
				xproto.PropModeReplace,
				ev.Requestor,
				ev.Property,
				a.incr,
				32,
				1,
				sizeBuf,
			).Check(); err != nil {
				transfers.cancel(key)
				log.Printf("x11-bridge: write INCR response: %v", err)
				refuseRequest(conn, ev)
				return
			}

			if err := sendSelectionNotify(conn, ev, ev.Property); err != nil {
				transfers.cancel(key)
				log.Printf("x11-bridge: notify INCR response: %v", err)
				return
			}
			log.Printf("x11-bridge: started INCR transfer (%d bytes)", len(data))
		}

	default:
		// Unsupported target: refuse.
		refuseRequest(conn, ev)
	}
}

// sendSelectionNotify sends a SelectionNotify event to the requestor,
// indicating the transfer property.
func sendSelectionNotify(conn *xgb.Conn, ev xproto.SelectionRequestEvent, property xproto.Atom) error {
	notify := xproto.SelectionNotifyEvent{
		Time:      ev.Time,
		Requestor: ev.Requestor,
		Selection: ev.Selection,
		Target:    ev.Target,
		Property:  property,
	}
	return xproto.SendEventChecked(
		conn,
		false,
		ev.Requestor,
		xproto.EventMaskNoEvent,
		string(notify.Bytes()),
	).Check()
}

// refuseRequest sends a SelectionNotify with property=None, which tells
// the requestor that the selection conversion failed.
func refuseRequest(conn *xgb.Conn, ev xproto.SelectionRequestEvent) {
	if err := sendSelectionNotify(conn, ev, xproto.AtomNone); err != nil {
		log.Printf("x11-bridge: refuse selection request: %v", err)
	}
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
	} else {
		host = strings.TrimPrefix(host, "[")
		host = strings.TrimSuffix(host, "]")
	}
	num, err = strconv.Atoi(display[idx+1:])
	if err != nil {
		return "", 0, fmt.Errorf("invalid display number in %q: %w", display, err)
	}
	return host, num, nil
}
