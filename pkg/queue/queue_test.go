package queue

import (
	"context"
	"testing"
	"time"
)

func TestWorkQueueDeduplicatesAndAllowsRequeueAfterDone(t *testing.T) {
	q := NewWorkQueue(1)
	q.Add("worker@1.0.0")
	q.Add("worker@1.0.0")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	got, ok := q.Get(ctx)
	if !ok || got != "worker@1.0.0" {
		t.Fatalf("Get() = (%q, %t), want (worker@1.0.0, true)", got, ok)
	}

	q.Done(got)
	q.Add(got)
	got, ok = q.Get(ctx)
	if !ok || got != "worker@1.0.0" {
		t.Fatalf("Get() after Done() = (%q, %t), want (worker@1.0.0, true)", got, ok)
	}
}

func TestWorkQueueGetStopsWhenContextIsCancelled(t *testing.T) {
	q := NewWorkQueue(1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if got, ok := q.Get(ctx); ok || got != "" {
		t.Fatalf("Get() = (%q, %t), want (empty, false)", got, ok)
	}
}
