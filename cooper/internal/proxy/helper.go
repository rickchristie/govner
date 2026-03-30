package proxy

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

// DefaultHelperReadTimeout is the default read timeout for the ACL helper
// when waiting for a response from the host-side listener. This must be
// longer than the maximum user-configurable approval timeout (60s) so the
// helper never times out before the user has finished deciding. The 5s
// buffer accounts for IPC and processing overhead.
const DefaultHelperReadTimeout = 65 * time.Second

// RunHelper is the main loop for the ACL helper binary that runs inside
// the proxy container. It is spawned by Squid as an external_acl_type helper.
//
// Protocol:
//   - Reads lines from stdin (one per request): "domain port source_ip"
//   - For each line, connects to the Unix socket at socketPath
//   - Writes the line to the socket
//   - Reads the response: "OK" or "ERR"
//   - Writes the response to stdout for Squid
//
// Fail-closed on every error path:
//   - Socket missing: writes "ERR" to stdout
//   - Connection failure: writes "ERR" to stdout
//   - Write failure: writes "ERR" to stdout
//   - Read timeout: writes "ERR" to stdout
//   - Malformed response: writes "ERR" to stdout
//   - Never hangs indefinitely
//   - Never fails open
func RunHelper(socketPath string, stdin io.Reader, stdout io.Writer) {
	scanner := bufio.NewScanner(stdin)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			// Empty line, write ERR and continue.
			fmt.Fprintln(stdout, "ERR")
			continue
		}

		result := processHelperRequest(socketPath, line)
		fmt.Fprintln(stdout, result)
	}
}

// processHelperRequest handles a single ACL request by connecting to the
// host-side listener, sending the request, and reading the response.
// Returns "OK" or "ERR". Every error path returns "ERR" (fail-closed).
func processHelperRequest(socketPath string, line string) string {
	// Validate input has the expected format: "domain port source_ip"
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return "ERR"
	}

	// Connect to the host-side ACL listener.
	conn, err := net.DialTimeout("unix", socketPath, 5*time.Second)
	if err != nil {
		// Socket missing, connection refused, or timeout -- fail closed.
		return "ERR"
	}
	defer conn.Close()

	// Set a read deadline to prevent hanging indefinitely.
	conn.SetDeadline(time.Now().Add(DefaultHelperReadTimeout))

	// Write the request line to the socket.
	_, err = fmt.Fprintf(conn, "%s\n", line)
	if err != nil {
		// Write failure (broken pipe, etc.) -- fail closed.
		return "ERR"
	}

	// Read the response from the host-side listener.
	reader := bufio.NewReader(conn)
	response, err := reader.ReadString('\n')
	if err != nil {
		// Read failure or timeout -- fail closed.
		return "ERR"
	}

	response = strings.TrimSpace(response)

	// Only accept "OK" as an allow response. Anything else is denied.
	if response == "OK" {
		return "OK"
	}

	return "ERR"
}
