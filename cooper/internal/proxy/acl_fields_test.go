package proxy

import (
	"testing"
	"time"
)

func TestSquidRequestFields(t *testing.T) {
	for _, test := range []struct {
		name, input, port, source string
	}{
		{"current HTTPS", "example.com 443 172.19.0.3 -", "443", "172.19.0.3"},
		{"current HTTP", "example.com 80 172.19.0.3 -", "80", "172.19.0.3"},
		{"IPv6 source", "example.com 443 2001:db8::3 -", "443", "2001:db8::3"},
		{"legacy Squid", "example.com 172.19.0.3 -", "443", "172.19.0.3"},
		{"legacy IPv6", "example.com 2001:db8::3 -", "443", "2001:db8::3"},
		{"legacy without data", "example.com 172.19.0.3", "443", "172.19.0.3"},
		{"explicit without data", "example.com 443 172.19.0.3", "443", "172.19.0.3"},
	} {
		t.Run(test.name, func(t *testing.T) {
			listener := NewACLListener(tempSocketPath(t), time.Second)
			if err := listener.Start(); err != nil {
				t.Fatal(err)
			}
			defer listener.Stop()
			response := make(chan string, 1)
			go func() {
				reply, err := sendRequest(listener.socketPath, test.input)
				if err != nil {
					reply = err.Error()
				}
				response <- reply
			}()
			select {
			case request := <-listener.RequestChan():
				if request.Domain != "example.com" || request.Port != test.port || request.SourceIP != test.source {
					t.Errorf("wrong request metadata: %+v", request)
				}
				listener.Deny(request.ID)
			case <-time.After(3 * time.Second):
				t.Fatal("request was not published")
			}
			if reply := <-response; reply != "ERR" {
				t.Errorf("denied request returned %q", reply)
			}
		})
	}
}

func TestInvalidSquidFieldsAreDeniedBeforeReview(t *testing.T) {
	listener := NewACLListener(tempSocketPath(t), time.Second)
	if err := listener.Start(); err != nil {
		t.Fatal(err)
	}
	defer listener.Stop()
	for _, input := range []string{
		"example.com", "example.com 0 172.19.0.3 -",
		"example.com 65536 172.19.0.3 -", "example.com +443 172.19.0.3 -",
		"example.com 443 unknown -", "example.com 443 172.19.0.3 unexpected",
		"example.com 172.19.0.3 - extra", "example.com 443 172.19.0.3 - -",
	} {
		reply, err := sendRequest(listener.socketPath, input)
		if err != nil || reply != "ERR" {
			t.Errorf("input %q: reply %q, error %v", input, reply, err)
		}
		select {
		case request := <-listener.RequestChan():
			t.Fatalf("invalid request reached review: %+v", request)
		default:
		}
	}
}
