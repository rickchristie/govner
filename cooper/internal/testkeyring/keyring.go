// Package testkeyring provides a private Secret Service with synthetic keys.
// Tests must never query or change the user's login keyring.
package testkeyring

import (
	"bufio"
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"
)

// Start returns a service connection and its private bus address. The caller
// exports only the fixture methods it needs. Cleanup stops the whole bus.
func Start(t *testing.T) (*dbus.Conn, string) {
	t.Helper()
	path, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("dbus-daemon is not installed")
	}
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	daemon := exec.CommandContext(ctx, path, "--session", "--nofork", "--print-address=1")
	output, err := daemon.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = daemon.Process.Kill(); _ = daemon.Wait() })
	address, err := bufio.NewReader(output).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	address = strings.TrimSpace(address)
	server, err := dbus.Connect(address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close() })
	if _, err := server.RequestName("org.freedesktop.secrets", dbus.NameFlagDoNotQueue); err != nil {
		t.Fatal(err)
	}
	return server, address
}
