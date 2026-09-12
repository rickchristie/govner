package vmproto

import "testing"

func TestActiveRequestsRejectsOnlyConcurrentDuplicate(t *testing.T) {
	t.Parallel()
	var requests ActiveRequests
	if !requests.Acquire("request-one") {
		t.Fatal("first request ID was rejected")
	}
	if requests.Acquire("request-one") {
		t.Fatal("concurrent duplicate request ID was accepted")
	}
	if !requests.Acquire("request-two") {
		t.Fatal("different request ID was rejected")
	}
	requests.Release("request-one")
	if !requests.Acquire("request-one") {
		t.Fatal("completed request ID could not be reused")
	}
}
