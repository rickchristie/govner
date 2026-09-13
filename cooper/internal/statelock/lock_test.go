package statelock

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestConcurrentStartupAndExclusiveReplacement(t *testing.T) {
	first, err := Acquire(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := Acquire(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	if lock, err := Acquire(ctx, true); !errors.Is(err, context.DeadlineExceeded) {
		if lock != nil {
			lock.Close()
		}
		t.Fatalf("replacement did not wait for startup: %v", err)
	}
	first.Close()
	second.Close()
	exclusive, err := Acquire(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	exclusive.Close()
}
