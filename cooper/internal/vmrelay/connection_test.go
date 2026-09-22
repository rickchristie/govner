package vmrelay

import (
	"bytes"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

func TestRelayKeepsOneWayTrafficActive(t *testing.T) {
	for _, direction := range []string{"download", "upload"} {
		t.Run(direction, func(t *testing.T) {
			t.Parallel()
			const idle = 300 * time.Millisecond
			client, server := startBoundedRelay(t, idle, 5*time.Second)
			sender, receiver := server, client
			if direction == "upload" {
				sender, receiver = client, server
			}
			// The sender has no response to read. Traffic in the other
			// direction must keep this blocked read open too.
			closed := make(chan error, 1)
			go func() {
				var data [1]byte
				_, err := sender.Read(data[:])
				closed <- err
			}()
			started := time.Now()
			for time.Since(started) < 3*idle {
				if err := transferRelayPacket(sender, receiver); err != nil {
					t.Fatal(err)
				}
				select {
				case err := <-closed:
					t.Fatalf("relay closed an active %s after %s: %v", direction, time.Since(started), err)
				case <-time.After(10 * time.Millisecond):
				}
			}
			// Once traffic stops, the same stream must still expire.
			select {
			case err := <-closed:
				if !errors.Is(err, io.EOF) {
					t.Fatalf("idle stream ended with %v, want EOF", err)
				}
			case <-time.After(3 * idle):
				t.Fatal("relay kept an idle stream open")
			}
		})
	}
}

func TestRelayClosesAnIdleConnection(t *testing.T) {
	t.Parallel()
	client, server := startBoundedRelay(t, 100*time.Millisecond, 5*time.Second)
	for _, connection := range []*net.TCPConn{client, server} {
		if err := connection.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		var data [1]byte
		if _, err := connection.Read(data[:]); !errors.Is(err, io.EOF) {
			t.Fatalf("idle connection read = %v, want EOF", err)
		}
	}
}

func TestRelayKeepsAResponseAfterRequestHalfClose(t *testing.T) {
	t.Parallel()
	const idle = 300 * time.Millisecond
	client, server := startBoundedRelay(t, idle, 5*time.Second)
	request := []byte("request")
	if _, err := client.Write(request); err != nil {
		t.Fatal(err)
	}
	if err := client.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(server)
	if err != nil || !bytes.Equal(got, request) {
		t.Fatalf("half-closed request = %q, %v", got, err)
	}
	started := time.Now()
	for time.Since(started) < 3*idle {
		if err := transferRelayPacket(server, client); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := server.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	if got, err := io.ReadAll(client); err != nil || len(got) != 0 {
		t.Fatalf("response after half-close = %q, %v", got, err)
	}
}

func TestRelayLimitsTheLifetimeOfActiveTraffic(t *testing.T) {
	t.Parallel()
	const lifetime = 300 * time.Millisecond
	started := time.Now()
	client, server := startBoundedRelay(t, 150*time.Millisecond, lifetime)
	for time.Since(started) < 4*lifetime {
		if err := transferRelayPacket(client, server); err != nil {
			checkRelayExpiration(t, err, time.Since(started), lifetime)
			return
		}
		if err := transferRelayPacket(server, client); err != nil {
			checkRelayExpiration(t, err, time.Since(started), lifetime)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("active traffic extended the maximum connection lifetime")
}

func checkRelayExpiration(t *testing.T, err error, elapsed, lifetime time.Duration) {
	t.Helper()
	if elapsed < lifetime || elapsed >= 4*lifetime {
		t.Fatalf("relay ended after %s with limit %s: %v", elapsed, lifetime, err)
	}
	var networkError net.Error
	if !errors.Is(err, io.EOF) && !errors.As(err, &networkError) {
		t.Fatalf("relay ended with a non-network error: %v", err)
	}
}

func transferRelayPacket(sender, receiver net.Conn) error {
	packet := []byte("active traffic")
	if _, err := sender.Write(packet); err != nil {
		return err
	}
	got := make([]byte, len(packet))
	if _, err := io.ReadFull(receiver, got); err != nil {
		return err
	}
	if !bytes.Equal(got, packet) {
		return errors.New("relay changed the packet")
	}
	return nil
}

func startBoundedRelay(t *testing.T, idle, lifetime time.Duration) (*net.TCPConn, *net.TCPConn) {
	t.Helper()
	client, guest := relayTCPPair(t)
	proxy, server := relayTCPPair(t)
	done := make(chan struct{})
	expires := time.Now().Add(lifetime)
	go func() {
		defer close(done)
		copyBounded(guest, proxy, idle, expires)
	}()
	t.Cleanup(func() {
		client.Close()
		guest.Close()
		proxy.Close()
		server.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("relay copy did not stop")
		}
	})
	return client, server
}

func relayTCPPair(t *testing.T) (*net.TCPConn, *net.TCPConn) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	client, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	server, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close() })
	for _, connection := range []net.Conn{client, server} {
		if err := connection.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	return client.(*net.TCPConn), server.(*net.TCPConn)
}
