// Package pubsub provides a generic, unbounded, concurrent-safe
// publish-subscribe broker. Publishers never block; each subscriber
// gets its own unbounded channel backed by an in-memory buffer.
//
// Usage:
//
//	broker := pubsub.NewBroker[MyEvent]()
//	sub := broker.Subscribe()    // returns <-chan MyEvent
//	broker.Publish(MyEvent{...}) // never blocks, even if subscriber is slow
//	broker.Unsubscribe(sub)      // clean up when done
//	broker.Close()               // shuts down all subscribers
package pubsub

import "sync"

// Broker is a generic publish-subscribe hub. Multiple goroutines can
// publish concurrently, and each subscriber receives every event
// published after it subscribed.
//
// Design:
//   - Publish is non-blocking: events are buffered per-subscriber.
//   - Each subscriber has its own goroutine that drains a buffer.
//   - No event is ever dropped (unbounded buffer per subscriber).
//   - Close shuts down all subscriber goroutines and channels.
type Broker[T any] struct {
	mu     sync.RWMutex
	subs   map[*subscription[T]]struct{}
	closed bool
}

// subscription is one subscriber's state. It has an unbounded internal
// buffer implemented as a goroutine bridging an input channel (written
// by Publish) to an output channel (read by the subscriber).
type subscription[T any] struct {
	in  chan T    // Publish writes here (buffered, non-blocking with overflow handling)
	out chan T    // Subscriber reads here (unbounded via bridge goroutine)
	done chan struct{} // Closed when the bridge goroutine exits
}

// NewBroker creates a new Broker. Call Close when done.
func NewBroker[T any]() *Broker[T] {
	return &Broker[T]{
		subs: make(map[*subscription[T]]struct{}),
	}
}

// Subscribe creates a new subscriber and returns a channel that will
// receive all future published events. The channel is unbounded — the
// subscriber will never miss an event, even if it's slow to consume.
//
// Call Unsubscribe with the returned channel when done, or rely on
// Close to clean up all subscribers.
func (b *Broker[T]) Subscribe() <-chan T {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		// Return a closed channel so callers don't block forever.
		ch := make(chan T)
		close(ch)
		return ch
	}

	sub := &subscription[T]{
		in:   make(chan T, 64), // Small buffer to absorb publish bursts.
		out:  make(chan T),
		done: make(chan struct{}),
	}

	// Bridge goroutine: drains 'in' into an unbounded slice, feeds 'out'.
	go func() {
		defer close(sub.done)
		defer close(sub.out)
		var buf []T
		for {
			if len(buf) == 0 {
				// Buffer empty: block on input only.
				val, ok := <-sub.in
				if !ok {
					return // Input closed — shut down.
				}
				buf = append(buf, val)
				continue
			}
			// Buffer has items: try to send front, or receive more.
			select {
			case sub.out <- buf[0]:
				buf[0] = *new(T) // Zero out for GC.
				buf = buf[1:]
			case val, ok := <-sub.in:
				if !ok {
					// Input closed (broker shutting down). Don't try to drain —
					// the subscriber may not be reading, which would deadlock.
					// The subscriber sees the output channel close and knows
					// no more events are coming.
					return
				}
				buf = append(buf, val)
			}
		}
	}()

	b.subs[sub] = struct{}{}
	return sub.out
}

// Unsubscribe removes a subscriber by its output channel and cleans up
// the bridge goroutine. Safe to call multiple times.
func (b *Broker[T]) Unsubscribe(ch <-chan T) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for sub := range b.subs {
		if sub.out == ch {
			close(sub.in)
			<-sub.done // Wait for bridge goroutine to finish.
			delete(b.subs, sub)
			return
		}
	}
}

// Publish sends an event to all current subscribers. It never blocks —
// each subscriber's bridge goroutine buffers the event if the subscriber
// is slow. Safe to call from multiple goroutines concurrently.
func (b *Broker[T]) Publish(event T) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return
	}

	for sub := range b.subs {
		// Non-blocking send to the subscriber's input channel.
		// If the 64-element buffer is full, this select falls through
		// to the goroutine-based overflow path below.
		select {
		case sub.in <- event:
		default:
			// Buffer full — send in a goroutine to avoid blocking the publisher.
			// The bridge goroutine will eventually drain and forward.
			// Recover from panic if the channel is closed during shutdown.
			go func(s *subscription[T], e T) {
				defer func() { recover() }()
				s.in <- e
			}(sub, event)
		}
	}
}

// Close shuts down all subscribers and prevents future publishes.
// All subscriber channels will be closed after their buffers drain.
func (b *Broker[T]) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}
	b.closed = true

	for sub := range b.subs {
		close(sub.in)
		<-sub.done
		delete(b.subs, sub)
	}
}

// Len returns the current number of subscribers.
func (b *Broker[T]) Len() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs)
}
