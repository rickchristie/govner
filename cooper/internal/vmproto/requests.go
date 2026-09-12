package vmproto

import "sync"

// ActiveRequests rejects a request ID while another stream with the same ID
// is active. Call Release when the accepted stream closes.
type ActiveRequests struct {
	mu     sync.Mutex
	active map[string]bool
}

// Acquire records an active ID and reports whether it was new.
func (r *ActiveRequests) Acquire(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active == nil {
		r.active = make(map[string]bool)
	}
	if r.active[id] {
		return false
	}
	r.active[id] = true
	return true
}

// Release removes one completed request ID.
func (r *ActiveRequests) Release(id string) {
	r.mu.Lock()
	delete(r.active, id)
	r.mu.Unlock()
}
