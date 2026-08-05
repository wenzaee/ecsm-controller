package desiredwatcher

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wenzaee/ecsm-controller/pkg/queue"
	"github.com/wenzaee/ecsm-controller/pkg/registry"
)

type fakeDesiredStore struct {
	events <-chan registry.DesiredEvent
	err    error
}

func (f fakeDesiredStore) PutDesired(context.Context, registry.DesiredState) error    { return nil }
func (f fakeDesiredStore) UpdateDesired(context.Context, registry.DesiredState) error { return nil }
func (f fakeDesiredStore) GetDesired(context.Context, string) (*registry.DesiredState, error) {
	return nil, registry.ErrNotFound
}
func (f fakeDesiredStore) QueryDesired(context.Context, string) (*registry.DesiredState, error) {
	return nil, registry.ErrNotFound
}
func (f fakeDesiredStore) DeleteDesired(context.Context, string) error { return nil }
func (f fakeDesiredStore) ListDesired(context.Context) ([]registry.DesiredState, error) {
	return nil, nil
}
func (f fakeDesiredStore) WatchDesired(context.Context) (<-chan registry.DesiredEvent, error) {
	return f.events, f.err
}

func TestWatcherQueuesEventsAndStopsOnClosedChannel(t *testing.T) {
	events := make(chan registry.DesiredEvent, 1)
	events <- registry.DesiredEvent{ServiceName: "worker@1.0.0", Action: registry.EventActionPut}
	close(events)
	q := queue.NewWorkQueue(1)
	watcher := New(fakeDesiredStore{events: events}, q)
	if err := watcher.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	service, ok := q.Get(ctx)
	if !ok || service != "worker@1.0.0" {
		t.Fatalf("queued service = (%q, %t)", service, ok)
	}
}

func TestWatcherReturnsStoreErrorAndCreatesDefaultQueue(t *testing.T) {
	storeErr := errors.New("watch unavailable")
	watcher := New(fakeDesiredStore{err: storeErr}, nil)
	if watcher.queue == nil {
		t.Fatal("New() did not create default queue")
	}
	if err := watcher.Run(context.Background()); !errors.Is(err, storeErr) {
		t.Fatalf("Run() error = %v, want store error", err)
	}
}

func TestWatcherStopsWhenContextIsCancelled(t *testing.T) {
	events := make(chan registry.DesiredEvent)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := New(fakeDesiredStore{events: events}, queue.NewWorkQueue(1)).Run(ctx); err != nil {
		t.Fatalf("Run() after cancellation error = %v", err)
	}
}
