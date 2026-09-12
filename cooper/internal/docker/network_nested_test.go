package docker

import "testing"

func TestContainerNetworkIPFromJSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		data    string
		network string
		want    string
		fail    bool
	}{
		{
			name:    "selected private address",
			data:    `{"cooper-control":{"IPAddress":"172.30.0.2"},"other":{"IPAddress":"172.31.0.3"}}`,
			network: "cooper-control",
			want:    "172.30.0.2",
		},
		{name: "missing network", data: `{"other":{"IPAddress":"172.31.0.3"}}`, network: "cooper-control", fail: true},
		{name: "empty address", data: `{"cooper-control":{"IPAddress":""}}`, network: "cooper-control", fail: true},
		{name: "public address", data: `{"cooper-control":{"IPAddress":"8.8.8.8"}}`, network: "cooper-control", fail: true},
		{name: "IPv6 address", data: `{"cooper-control":{"IPAddress":"fd00::2"}}`, network: "cooper-control", fail: true},
		{name: "invalid JSON", data: `{`, network: "cooper-control", fail: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := containerNetworkIPFromJSON([]byte(test.data), "outer-agent", test.network)
			if test.fail {
				if err == nil {
					t.Fatalf("containerNetworkIPFromJSON returned %q; want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("containerNetworkIPFromJSON = %q; want %q", got, test.want)
			}
		})
	}
}
