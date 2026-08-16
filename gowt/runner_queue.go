package main

import "sync"

// streamQueue separates pipe reads from delivery on a bounded public channel.
// enqueue only appends under a mutex, so a slow TUI can never apply
// backpressure to the child process's stdout or stderr pipe.
//
// finalize closes the public channel after filling any capacity immediately
// available and returns the undelivered suffix. The runner carries that suffix
// in TestResult, which keeps completion bounded without dropping output.
type streamQueue[T any] struct {
	output    chan T
	wake      chan struct{}
	finalized chan []T

	mu      sync.Mutex
	pending []T
	closed  bool
}

func newStreamQueue[T any](capacity int) *streamQueue[T] {
	queue := &streamQueue[T]{
		output:    make(chan T, capacity),
		wake:      make(chan struct{}, 1),
		finalized: make(chan []T, 1),
	}
	go queue.run()
	return queue
}

func (q *streamQueue[T]) Output() <-chan T {
	return q.output
}

func (q *streamQueue[T]) Enqueue(value T) bool {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return false
	}
	q.pending = append(q.pending, value)
	q.mu.Unlock()
	q.notify()
	return true
}

// Finalize may be called once, after the producer has stopped enqueueing.
func (q *streamQueue[T]) Finalize() []T {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	q.notify()
	return <-q.finalized
}

func (q *streamQueue[T]) notify() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *streamQueue[T]) run() {
	for {
		value, hasValue, closed := q.head()
		if !hasValue {
			if closed {
				q.complete(nil)
				return
			}
			<-q.wake
			continue
		}

		if closed {
			select {
			case q.output <- value:
				q.pop()
			default:
				q.complete(q.takePending())
				return
			}
			continue
		}

		select {
		case q.output <- value:
			q.pop()
		case <-q.wake:
			// Re-read closed and pending state. In particular, Finalize must
			// interrupt a send blocked by a full public channel.
		}
	}
}

func (q *streamQueue[T]) head() (value T, hasValue, closed bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.pending) > 0 {
		value = q.pending[0]
		hasValue = true
	}
	return value, hasValue, q.closed
}

func (q *streamQueue[T]) pop() {
	q.mu.Lock()
	var zero T
	q.pending[0] = zero
	q.pending = q.pending[1:]
	q.mu.Unlock()
}

func (q *streamQueue[T]) takePending() []T {
	q.mu.Lock()
	pending := q.pending
	q.pending = nil
	q.mu.Unlock()
	return pending
}

func (q *streamQueue[T]) complete(pending []T) {
	// Closing output before publishing the suffix preserves EventStream's
	// ordering guarantee: everything received from the channel precedes the
	// pending values returned with Done.
	close(q.output)
	q.finalized <- pending
	close(q.finalized)
}
