package main

import (
	"bytes"
	"testing"
	"time"

	"github.com/jezek/xgb/xproto"
)

func TestINCRTransferSetWaitsForFinalChunkAcknowledgement(t *testing.T) {
	tests := []struct {
		name string
		size int
	}{
		{name: "partial final chunk", size: incrChunkSize + 17},
		{name: "full final chunk", size: 2 * incrChunkSize},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Unix(100, 0)
			key := incrTransferKey{requestor: 11, property: 22}
			want := bytes.Repeat([]byte{0x5a}, tt.size)
			transfers := newINCRTransferSet(time.Second, 2)

			if replaced, accepted := transfers.start(key, want, now); replaced || !accepted {
				t.Fatalf("start = (replaced=%t, accepted=%t), want (false, true)", replaced, accepted)
			}

			var got []byte
			for len(got) < len(want) {
				step, ok := transfers.next(key, now)
				if !ok {
					t.Fatal("transfer disappeared before all data chunks were acknowledged")
				}
				if step.terminator {
					t.Fatal("terminator emitted before all data chunks")
				}
				got = append(got, step.data...)
			}

			if !bytes.Equal(got, want) {
				t.Fatalf("received %d bytes, want %d", len(got), len(want))
			}
			if len(transfers.active) != 1 {
				t.Fatalf("active transfers = %d, want 1 until final chunk is acknowledged", len(transfers.active))
			}

			step, ok := transfers.next(key, now)
			if !ok || !step.terminator || len(step.data) != 0 {
				t.Fatalf("final acknowledgement step = (%+v, %t), want zero-length terminator", step, ok)
			}
			if len(transfers.active) != 0 {
				t.Fatalf("active transfers = %d after terminator, want 0", len(transfers.active))
			}
		})
	}
}

func TestINCRTransferSetAllowsIndependentRequestsAndBoundsRetention(t *testing.T) {
	now := time.Unix(200, 0)
	transfers := newINCRTransferSet(time.Second, 2)
	first := incrTransferKey{requestor: 1, property: 10}
	second := incrTransferKey{requestor: 2, property: 20}
	third := incrTransferKey{requestor: 3, property: 30}

	if _, accepted := transfers.start(first, []byte("first"), now); !accepted {
		t.Fatal("first transfer was rejected")
	}
	if _, accepted := transfers.start(second, []byte("second"), now); !accepted {
		t.Fatal("independent second transfer was rejected")
	}
	if _, accepted := transfers.start(third, []byte("third"), now); accepted {
		t.Fatal("transfer beyond retention limit was accepted")
	}

	replaced, accepted := transfers.start(first, []byte("replacement"), now.Add(time.Millisecond))
	if !replaced || !accepted {
		t.Fatalf("same-key retry = (replaced=%t, accepted=%t), want (true, true)", replaced, accepted)
	}
	if got := string(transfers.active[first].data); got != "replacement" {
		t.Fatalf("replacement data = %q, want %q", got, "replacement")
	}
}

func TestINCRTransferSetExpiresAbandonedRequests(t *testing.T) {
	timeout := time.Second
	start := time.Unix(300, 0)
	transfers := newINCRTransferSet(timeout, 4)
	oldKey := incrTransferKey{requestor: 1, property: 10}
	recentKey := incrTransferKey{requestor: 2, property: 20}

	transfers.start(oldKey, []byte("old"), start)
	transfers.start(recentKey, []byte("recent"), start.Add(750*time.Millisecond))

	if expired := transfers.expire(start.Add(1500 * time.Millisecond)); expired != 1 {
		t.Fatalf("expired transfers = %d, want 1", expired)
	}
	if _, ok := transfers.active[oldKey]; ok {
		t.Fatal("abandoned transfer remained active")
	}
	if _, ok := transfers.active[recentKey]; !ok {
		t.Fatal("recent transfer expired too early")
	}
}

func TestINCRTransferSetRemovesDestroyedRequestorWindow(t *testing.T) {
	now := time.Unix(400, 0)
	transfers := newINCRTransferSet(time.Second, 4)
	closedWindow := xproto.Window(44)
	otherWindow := xproto.Window(55)

	transfers.start(incrTransferKey{requestor: closedWindow, property: 1}, []byte("a"), now)
	transfers.start(incrTransferKey{requestor: closedWindow, property: 2}, []byte("b"), now)
	otherKey := incrTransferKey{requestor: otherWindow, property: 3}
	transfers.start(otherKey, []byte("c"), now)

	if removed := transfers.removeWindow(closedWindow); removed != 2 {
		t.Fatalf("removed transfers = %d, want 2", removed)
	}
	if len(transfers.active) != 1 {
		t.Fatalf("active transfers = %d, want 1", len(transfers.active))
	}
	if _, ok := transfers.active[otherKey]; !ok {
		t.Fatal("transfer for another requestor was removed")
	}
}
