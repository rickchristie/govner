package pubsub

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBroker_SingleSubscriber(t *testing.T) {
	b := NewBroker[int]()
	defer b.Close()

	ch := b.Subscribe()
	b.Publish(1)
	b.Publish(2)
	b.Publish(3)

	for _, want := range []int{1, 2, 3} {
		select {
		case got := <-ch:
			if got != want {
				t.Errorf("got %d, want %d", got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("timeout waiting for %d", want)
		}
	}
}

func TestBroker_MultipleSubscribers(t *testing.T) {
	b := NewBroker[string]()
	defer b.Close()

	ch1 := b.Subscribe()
	ch2 := b.Subscribe()
	ch3 := b.Subscribe()

	b.Publish("hello")

	for i, ch := range []<-chan string{ch1, ch2, ch3} {
		select {
		case got := <-ch:
			if got != "hello" {
				t.Errorf("subscriber %d: got %q, want %q", i, got, "hello")
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d: timeout", i)
		}
	}
}

func TestBroker_Unsubscribe(t *testing.T) {
	b := NewBroker[int]()
	defer b.Close()

	ch1 := b.Subscribe()
	ch2 := b.Subscribe()

	b.Unsubscribe(ch1)

	b.Publish(42)

	// ch2 should receive it.
	select {
	case got := <-ch2:
		if got != 42 {
			t.Errorf("got %d, want 42", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout on ch2")
	}

	// ch1 should be closed.
	select {
	case _, ok := <-ch1:
		if ok {
			t.Error("ch1 should be closed after unsubscribe")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for ch1 close")
	}

	if b.Len() != 1 {
		t.Errorf("expected 1 subscriber, got %d", b.Len())
	}
}

func TestBroker_Close(t *testing.T) {
	b := NewBroker[int]()

	ch1 := b.Subscribe()
	ch2 := b.Subscribe()

	b.Close()

	// Both channels should be closed after Close.
	for i, ch := range []<-chan int{ch1, ch2} {
		select {
		case _, ok := <-ch:
			if ok {
				t.Errorf("subscriber %d: channel should be closed after Close", i)
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d: timeout waiting for channel close", i)
		}
	}

	// Publish after close is a no-op (should not panic).
	b.Publish(99)
}

func TestBroker_SubscribeAfterClose(t *testing.T) {
	b := NewBroker[int]()
	b.Close()

	ch := b.Subscribe()
	// Should return a closed channel.
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected closed channel from Subscribe after Close")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout — Subscribe after Close should return closed channel")
	}
}

func TestBroker_SlowSubscriberDoesNotBlock(t *testing.T) {
	b := NewBroker[int]()
	defer b.Close()

	// Subscribe but don't consume.
	_ = b.Subscribe()

	// Publish many events — should not block.
	done := make(chan struct{})
	go func() {
		for i := range 1000 {
			b.Publish(i)
		}
		close(done)
	}()

	select {
	case <-done:
		// Good — publishing completed without blocking.
	case <-time.After(5 * time.Second):
		t.Fatal("Publish blocked on slow subscriber")
	}
}

func TestBroker_ConcurrentPublish(t *testing.T) {
	b := NewBroker[int]()
	defer b.Close()

	ch := b.Subscribe()

	const numPublishers = 10
	const eventsPerPublisher = 100

	var wg sync.WaitGroup
	for p := range numPublishers {
		wg.Add(1)
		go func(publisherID int) {
			defer wg.Done()
			for i := range eventsPerPublisher {
				b.Publish(publisherID*1000 + i)
			}
		}(p)
	}

	// Consume in background.
	var count atomic.Int64
	consumeDone := make(chan struct{})
	go func() {
		for range ch {
			count.Add(1)
			if count.Load() == numPublishers*eventsPerPublisher {
				close(consumeDone)
				return
			}
		}
	}()

	wg.Wait() // All publishers done.

	select {
	case <-consumeDone:
		if got := count.Load(); got != numPublishers*eventsPerPublisher {
			t.Errorf("received %d events, want %d", got, numPublishers*eventsPerPublisher)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout — received %d of %d events", count.Load(), numPublishers*eventsPerPublisher)
	}
}

func TestBroker_LateSubscriberMissesOldEvents(t *testing.T) {
	b := NewBroker[int]()
	defer b.Close()

	b.Publish(1)
	b.Publish(2)

	ch := b.Subscribe()

	b.Publish(3)

	select {
	case got := <-ch:
		if got != 3 {
			t.Errorf("late subscriber got %d, want 3 (should miss old events)", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestBroker_UnsubscribeIdempotent(t *testing.T) {
	b := NewBroker[int]()
	defer b.Close()

	ch := b.Subscribe()
	b.Unsubscribe(ch)
	b.Unsubscribe(ch) // Should not panic.
}
