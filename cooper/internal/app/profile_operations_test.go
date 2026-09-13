package app

import (
	"context"
	"testing"
	"time"
)

func TestProfileShutdownCancelsAndWaitsForRollback(t *testing.T) {
	var operations profileOperations
	ctx, done, err := operations.begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	stopped := make(chan struct{})
	go func() { operations.stop(); close(stopped) }()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel the operation")
	}
	select {
	case <-stopped:
		t.Fatal("shutdown returned before rollback finished")
	default:
	}
	done()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not finish")
	}
	if _, _, err := operations.begin(t.Context()); err == nil {
		t.Fatal("new operation started during shutdown")
	}
}
