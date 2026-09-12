package vmproto

import "testing"

func TestGuestDiagnosticValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		change    func(*GuestDiagnostic)
		wantError bool
	}{
		{name: "depth one"},
		{name: "depth two", change: func(d *GuestDiagnostic) { d.Depth = 2; d.KVMAvailable = false }},
		{name: "Docker network devices", change: func(d *GuestDiagnostic) { d.Interfaces = []string{"br-test", "docker0", "lo", "veth-test"} }},
		{name: "external network device", change: func(d *GuestDiagnostic) { d.ExternalInterfaces = []string{"eth0"} }, wantError: true},
		{name: "default route", change: func(d *GuestDiagnostic) { d.DefaultRoutes = []string{"eth0"} }, wantError: true},
		{name: "missing interface inventory", change: func(d *GuestDiagnostic) { d.Interfaces = nil }, wantError: true},
		{name: "missing KVM at depth one", change: func(d *GuestDiagnostic) { d.KVMAvailable = false }, wantError: true},
		{name: "KVM at depth two", change: func(d *GuestDiagnostic) { d.Depth = 2 }, wantError: true},
		{name: "missing Docker", change: func(d *GuestDiagnostic) { d.DockerVersion = "" }, wantError: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			diagnostic := GuestDiagnostic{
				Schema: GuestDiagnosticSchema, Depth: 1, Interfaces: []string{"lo"},
				DockerVersion: "29.7.2", KVMAvailable: true,
			}
			if test.change != nil {
				test.change(&diagnostic)
			}
			if err := diagnostic.Validate(); (err != nil) != test.wantError {
				t.Fatalf("Validate() error = %v, wantError %v", err, test.wantError)
			}
		})
	}
}
