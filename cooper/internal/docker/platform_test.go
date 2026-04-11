package docker

import "testing"

func TestBridgeGatewayIPsForOSDarwin(t *testing.T) {
	ips, err := bridgeGatewayIPsForOS("darwin")
	if err != nil {
		t.Fatalf("bridgeGatewayIPsForOS(darwin) error: %v", err)
	}
	if len(ips) != 0 {
		t.Fatalf("bridgeGatewayIPsForOS(darwin) = %v, want no gateway IPs", ips)
	}
}

func TestHostRelayGatewayIPsForOSDarwin(t *testing.T) {
	ips := hostRelayGatewayIPsForOS("darwin", []string{"172.17.0.1", "172.18.0.1"})
	if len(ips) != 0 {
		t.Fatalf("hostRelayGatewayIPsForOS(darwin) = %v, want no relay IPs", ips)
	}
}

func TestUniqueStrings(t *testing.T) {
	got := uniqueStrings([]string{"1.1.1.1", "", "1.1.1.1", " 2.2.2.2 "})
	want := []string{"1.1.1.1", "2.2.2.2"}
	if len(got) != len(want) {
		t.Fatalf("uniqueStrings() len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("uniqueStrings()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
