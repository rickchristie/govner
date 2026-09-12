package vmguest

import (
	"reflect"
	"testing"
)

func TestDefaultRouteInterfaces(t *testing.T) {
	t.Parallel()
	ipv4 := []byte("Iface Destination Gateway Flags RefCnt Use Metric Mask MTU Window IRTT\n" +
		"lo 00000000 00000000 0001 0 0 0 00000000 0 0 0\n" +
		"docker0 00001EAC 00000000 0001 0 0 0 0000FFFF 0 0 0\n" +
		"eth0 00000000 0100000A 0003 0 0 0 00000000 0 0 0\n")
	ipv6 := []byte("00000000000000000000000000000000 00 00000000000000000000000000000000 00 00000000000000000000000000000000 00000000 00000000 00000000 00200200 lo\n" +
		"00000000000000000000000000000000 00 00000000000000000000000000000000 00 fe800000000000000000000000000001 00000064 00000000 00000000 00000003 eth1\n")

	got := defaultRouteInterfaces(ipv4, ipv6)
	want := []string{"eth0", "eth1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("defaultRouteInterfaces() = %v, want %v", got, want)
	}
}

func TestDefaultRouteInterfacesWithoutExternalRoute(t *testing.T) {
	t.Parallel()
	ipv4 := []byte("Iface Destination Gateway Flags RefCnt Use Metric Mask MTU Window IRTT\n" +
		"docker0 00001EAC 00000000 0001 0 0 0 0000FFFF 0 0 0\n")

	if got := defaultRouteInterfaces(ipv4, nil); len(got) != 0 {
		t.Fatalf("defaultRouteInterfaces() = %v, want no routes", got)
	}
}
