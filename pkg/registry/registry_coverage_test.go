package registry

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rosedblabs/rosedb/v2"
)

func TestOpenRoseDBFailsWithInvalidDir(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(filePath, []byte("block"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenRoseDB(RoseDBOptions{DirPath: filePath}); err == nil {
		t.Fatal("OpenRoseDB() unexpectedly succeeded with invalid dir")
	}
}

func TestNewRoseDBStoreWrapsExistingDB(t *testing.T) {
	opts := rosedb.DefaultOptions
	opts.DirPath = t.TempDir()
	db, err := rosedb.Open(opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	store := NewRoseDBStore(db)
	state := DesiredState{ServiceName: "api@1.0.0", Action: DesiredActionStart}
	if err := store.PutDesired(context.Background(), state); err != nil {
		t.Fatalf("PutDesired() error = %v", err)
	}
}

func TestRegistryWriteValidationAndContextErrors(t *testing.T) {
	store := newTestStore(t, 0)
	ctx := context.Background()

	invalid := DesiredState{ServiceName: "invalid", Action: DesiredActionStart}
	if err := store.PutDesired(ctx, invalid); !errors.Is(err, ErrInvalidServiceName) {
		t.Fatalf("PutDesired(invalid) error = %v", err)
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := store.PutStatus(cancelled, ServiceStatus{ServiceName: "api@1.0.0"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("PutStatus() with cancelled ctx error = %v", err)
	}
	if err := store.DeleteDesired(cancelled, "api@1.0.0"); !errors.Is(err, context.Canceled) {
		t.Fatalf("DeleteDesired() with cancelled ctx error = %v", err)
	}
	if err := store.DeleteStatus(cancelled, "api@1.0.0"); !errors.Is(err, context.Canceled) {
		t.Fatalf("DeleteStatus() with cancelled ctx error = %v", err)
	}

	if err := store.PutStatus(ctx, ServiceStatus{ServiceName: "invalid"}); !errors.Is(err, ErrInvalidServiceName) {
		t.Fatalf("PutStatus(invalid) error = %v", err)
	}
	if err := store.DeleteDesired(ctx, "invalid"); !errors.Is(err, ErrInvalidServiceName) {
		t.Fatalf("DeleteDesired(invalid) error = %v", err)
	}
	if err := store.DeleteStatus(ctx, "invalid"); !errors.Is(err, ErrInvalidServiceName) {
		t.Fatalf("DeleteStatus(invalid) error = %v", err)
	}
}

func TestRegistryReadErrors(t *testing.T) {
	store := newTestStore(t, 0)
	ctx := context.Background()

	if _, err := store.GetDesired(ctx, "invalid"); !errors.Is(err, ErrInvalidServiceName) {
		t.Fatalf("GetDesired(invalid) error = %v", err)
	}
	if _, err := store.GetStatus(ctx, "invalid"); !errors.Is(err, ErrInvalidServiceName) {
		t.Fatalf("GetStatus(invalid) error = %v", err)
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := store.GetDesired(cancelled, "api@1.0.0"); !errors.Is(err, context.Canceled) {
		t.Fatalf("GetDesired() with cancelled ctx error = %v", err)
	}
	if _, err := store.ListStatus(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("ListStatus() with cancelled ctx error = %v", err)
	}

	// rosedb 对不存在 key 的 Delete 返回 nil，不存在的读取才返回 ErrNotFound。
	if err := store.DeleteDesired(ctx, "missing@1.0.0"); err != nil {
		t.Fatalf("DeleteDesired(missing) error = %v, want nil", err)
	}
	if err := store.DeleteStatus(ctx, "missing@1.0.0"); err != nil {
		t.Fatalf("DeleteStatus(missing) error = %v, want nil", err)
	}
	if !errors.Is(mapKeyNotFoundErr(rosedb.ErrKeyNotFound), ErrNotFound) {
		t.Fatal("mapKeyNotFoundErr() did not map ErrKeyNotFound")
	}
	if mapKeyNotFoundErr(nil) != nil {
		t.Fatal("mapKeyNotFoundErr() changed nil error")
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := store.GetDesired(ctx, "api@1.0.0"); err == nil {
		t.Fatal("GetDesired() on closed store unexpectedly succeeded")
	}
	if err := store.DeleteDesired(ctx, "api@1.0.0"); err == nil {
		t.Fatal("DeleteDesired() on closed store unexpectedly succeeded")
	}
}

func TestRegistryListDecodeAndIterationErrors(t *testing.T) {
	store := newTestStore(t, 0)
	ctx := context.Background()

	if err := store.db.Put([]byte(DesiredServiceKey("bad@1.0.0")), []byte("not-json")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListDesired(ctx); err == nil {
		t.Fatal("ListDesired() unexpectedly decoded invalid json")
	}
	if err := store.db.Put([]byte(StatusServiceKey("bad@1.0.0")), []byte("not-json")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListStatus(ctx); err == nil {
		t.Fatal("ListStatus() unexpectedly decoded invalid json")
	}

	// 前缀之外的更大 key 应该在迭代时被跳过。
	if err := store.db.Put([]byte("desiredzzz"), []byte("out-of-prefix")); err != nil {
		t.Fatal(err)
	}
	if err := store.db.Delete([]byte(DesiredServiceKey("bad@1.0.0"))); err != nil {
		t.Fatal(err)
	}
	states, err := store.ListDesired(ctx)
	if err != nil || len(states) != 0 {
		t.Fatalf("ListDesired() = (%+v, %v), want empty list", states, err)
	}

	// 迭代过程中 context 取消应中止遍历。
	for _, name := range []string{"a@1.0.0", "b@1.0.0"} {
		if err := store.PutDesired(ctx, DesiredState{ServiceName: name, Action: DesiredActionStart}); err != nil {
			t.Fatal(err)
		}
	}
	cancelCtx, cancel := context.WithCancel(ctx)
	visited := 0
	iterErr := store.ascendPrefix(cancelCtx, DesiredServicePrefix, func(_ string, _ []byte) error {
		visited++
		cancel()
		return nil
	})
	if !errors.Is(iterErr, context.Canceled) || visited == 0 {
		t.Fatalf("ascendPrefix() = (%v, visited=%d), want context cancellation", iterErr, visited)
	}
}

func TestRegistryPutJSONMarshalError(t *testing.T) {
	store := newTestStore(t, 0)
	if err := store.putJSON("any-key", make(chan int)); err == nil {
		t.Fatal("putJSON() unexpectedly marshalled a channel")
	}
}

func TestMapWatchError(t *testing.T) {
	if !errors.Is(mapWatchError(rosedb.ErrWatchDisabled), ErrWatchDisabled) {
		t.Fatal("mapWatchError() did not map ErrWatchDisabled")
	}
	other := errors.New("other")
	if !errors.Is(mapWatchError(other), other) {
		t.Fatal("mapWatchError() changed unrelated error")
	}
}

func TestWatchDesiredStreamsEvents(t *testing.T) {
	store := newTestStore(t, 64)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, err := store.WatchDesired(ctx)
	if err != nil {
		t.Fatalf("WatchDesired() error = %v", err)
	}

	state := DesiredState{ServiceName: "worker@1.0.0", Action: DesiredActionStart}
	if err := store.PutDesired(ctx, state); err != nil {
		t.Fatal(err)
	}
	// 非 desired key 的事件应被过滤。
	if err := store.PutStatus(ctx, ServiceStatus{ServiceName: "worker@1.0.0"}); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteDesired(ctx, state.ServiceName); err != nil {
		t.Fatal(err)
	}

	got := waitDesiredEvents(t, events, 2)
	if got[0].ServiceName != state.ServiceName || got[0].Action != EventActionPut {
		t.Fatalf("first event = %+v", got[0])
	}
	if got[1].ServiceName != state.ServiceName || got[1].Action != EventActionDelete {
		t.Fatalf("second event = %+v", got[1])
	}

	cancel()
	waitChannelClosed(t, events)
}

func TestWatchDesiredStopsWhileSendingWhenCancelled(t *testing.T) {
	store := newTestStore(t, 256)
	ctx, cancel := context.WithCancel(context.Background())

	events, err := store.WatchDesired(ctx)
	if err != nil {
		t.Fatalf("WatchDesired() error = %v", err)
	}

	// 写入超过 out 缓冲的事件且不消费，等 out 写满后发送方会阻塞，再取消 ctx。
	for i := 0; i < 70; i++ {
		state := DesiredState{ServiceName: fmt.Sprintf("svc%d@1.0.0", i), Action: DesiredActionStart}
		if err := store.PutDesired(context.Background(), state); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.After(5 * time.Second)
	for len(events) < cap(events) {
		select {
		case <-deadline:
			t.Fatalf("event channel never filled, len=%d", len(events))
		default:
			time.Sleep(time.Millisecond)
		}
	}
	cancel()
	waitChannelClosed(t, events)
}

func TestWatchDesiredClosesWhenStoreClosed(t *testing.T) {
	store, err := OpenRoseDB(RoseDBOptions{DirPath: t.TempDir(), WatchQueueSize: 64})
	if err != nil {
		t.Fatal(err)
	}
	events, err := store.WatchDesired(context.Background())
	if err != nil {
		t.Fatalf("WatchDesired() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	// 使用未取消的 ctx，确保 watch 通道关闭分支被覆盖。
	waitChannelClosed(t, events)
}

func waitDesiredEvents(t *testing.T, ch <-chan DesiredEvent, count int) []DesiredEvent {
	t.Helper()
	var got []DesiredEvent
	deadline := time.After(5 * time.Second)
	for len(got) < count {
		select {
		case event, ok := <-ch:
			if !ok {
				t.Fatalf("event channel closed after %d events", len(got))
			}
			got = append(got, event)
		case <-deadline:
			t.Fatalf("timed out waiting for events, got %+v", got)
		}
	}
	return got
}

func waitChannelClosed(t *testing.T, ch <-chan DesiredEvent) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for event channel close")
		}
	}
}
