package vmguest

import "testing"

func TestDesktopAddressRestrictsGuestDestination(t *testing.T) {
	for _, test := range []struct {
		address, subnet string
		valid           bool
	}{
		{"172.31.0.3", "172.31.0.0/24", true},
		{"192.168.1.1", "172.31.0.0/24", false},
		{"127.0.0.1", "172.31.0.0/24", false},
		{"::1", "::/0", false},
		{"host.test", "172.31.0.0/24", false},
		{"172.31.0.3:22", "172.31.0.0/24", false},
		{"172.31.0.3", "invalid", false},
	} {
		address, err := desktopAddress(test.address, test.subnet)
		if (err == nil) != test.valid {
			t.Fatalf("%q in %q: %v", test.address, test.subnet, err)
		}
		if test.valid && address != "172.31.0.3:6080" {
			t.Fatalf("unexpected address: %s", address)
		}
	}
}
