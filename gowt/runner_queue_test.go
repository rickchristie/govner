package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamQueueFinalizePreservesOverflowInOrder(t *testing.T) {
	queue := newStreamQueue[int](8)
	want := make([]int, 10_000)
	for i := range want {
		want[i] = i
		require.True(t, queue.Enqueue(i))
	}

	finalized := make(chan []int, 1)
	go func() { finalized <- queue.Finalize() }()

	var pending []int
	select {
	case pending = <-finalized:
	case <-time.After(time.Second):
		t.Fatal("Finalize waited for a consumer of the bounded output channel")
	}

	var got []int
	for value := range queue.Output() {
		got = append(got, value)
	}
	got = append(got, pending...)
	assert.Equal(t, want, got)
	assert.False(t, queue.Enqueue(len(want)), "a finalized queue must reject new records")
}

func TestStreamQueueStreamsBeforeFinalization(t *testing.T) {
	queue := newStreamQueue[string](1)
	require.True(t, queue.Enqueue("first"))
	require.True(t, queue.Enqueue("second"))
	assert.Equal(t, "first", receiveWithTimeout(t, queue.Output()))

	pending := queue.Finalize()
	got := []string{"first"}
	for value := range queue.Output() {
		got = append(got, value)
	}
	got = append(got, pending...)
	assert.Equal(t, []string{"first", "second"}, got)
}
