package registry

import (
	"context"
	"errors"
	"testing"

	"github.com/rosedblabs/rosedb/v2"
)

func newTestStore(t *testing.T, watchQueueSize uint64) *RoseDBStore {
	t.Helper()
	store, err := OpenRoseDB(RoseDBOptions{DirPath: t.TempDir(), Sync: true, WatchQueueSize: watchQueueSize})
	if err != nil {
		t.Fatalf("OpenRoseDB() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestDesiredAndStatusPersistence(t *testing.T) {
	store := newTestStore(t, 0)
	ctx := context.Background()
	desired := DesiredState{ServiceName: "worker@1.0.0", Action: DesiredActionStart, Replicas: 2}
	if err := store.PutDesired(ctx, desired); err != nil {
		t.Fatalf("PutDesired() error = %v", err)
	}
	desired.Replicas = 3
	if err := store.UpdateDesired(ctx, desired); err != nil {
		t.Fatalf("UpdateDesired() error = %v", err)
	}
	gotDesired, err := store.QueryDesired(ctx, desired.ServiceName)
	if err != nil || *gotDesired != desired {
		t.Fatalf("QueryDesired() = (%+v, %v), want (%+v, nil)", gotDesired, err, desired)
	}
	list, err := store.ListDesired(ctx)
	if err != nil || len(list) != 1 || list[0] != desired {
		t.Fatalf("ListDesired() = (%+v, %v)", list, err)
	}

	status := ServiceStatus{ServiceName: desired.ServiceName, Phase: StatusPhaseReady, LastAction: "need_start"}
	if err := store.PutStatus(ctx, status); err != nil {
		t.Fatalf("PutStatus() error = %v", err)
	}
	gotStatus, err := store.GetStatus(ctx, status.ServiceName)
	if err != nil || gotStatus.Phase != StatusPhaseReady || gotStatus.UpdatedAt.IsZero() {
		t.Fatalf("GetStatus() = (%+v, %v), want stored status with timestamp", gotStatus, err)
	}
	statuses, err := store.ListStatus(ctx)
	if err != nil || len(statuses) != 1 || statuses[0].ServiceName != status.ServiceName {
		t.Fatalf("ListStatus() = (%+v, %v)", statuses, err)
	}

	if err := store.DeleteDesired(ctx, desired.ServiceName); err != nil {
		t.Fatalf("DeleteDesired() error = %v", err)
	}
	if _, err := store.GetDesired(ctx, desired.ServiceName); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetDesired() after delete error = %v, want ErrNotFound", err)
	}
	if err := store.DeleteStatus(ctx, status.ServiceName); err != nil {
		t.Fatalf("DeleteStatus() error = %v", err)
	}
	if _, err := store.GetStatus(ctx, status.ServiceName); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetStatus() after delete error = %v, want ErrNotFound", err)
	}
}

func TestRegistryValidationAndKeyParsing(t *testing.T) {
	valid := DesiredState{ServiceName: "api@2.0.0", Action: DesiredActionStop, Replicas: 0}
	if err := ValidateDesiredState(valid); err != nil {
		t.Fatalf("ValidateDesiredState(valid) error = %v", err)
	}
	for _, state := range []DesiredState{
		{ServiceName: "invalid", Action: DesiredActionStart},
		{ServiceName: "api@1", Action: "restart"},
		{ServiceName: "api@1", Action: DesiredActionStart, Replicas: -1},
	} {
		if err := ValidateDesiredState(state); err == nil {
			t.Errorf("ValidateDesiredState(%+v) unexpectedly succeeded", state)
		}
	}
	if name, ok := ServiceNameFromDesiredKey(DesiredServiceKey("api@2.0.0")); !ok || name != "api@2.0.0" {
		t.Fatalf("ServiceNameFromDesiredKey() = (%q, %t)", name, ok)
	}
	if _, ok := ServiceNameFromStatusKey("desired/services/api@2.0.0"); ok {
		t.Fatal("ServiceNameFromStatusKey() accepted desired key")
	}
	if err := ValidateServiceName("api@2.0.0"); err != nil {
		t.Fatalf("ValidateServiceName() error = %v", err)
	}
	if toEventAction(rosedb.WatchActionDelete) != EventActionDelete || toEventAction(rosedb.WatchActionPut) != EventActionPut {
		t.Fatal("toEventAction() returned unexpected values")
	}
}

func TestRegistryRespectsCancelledContext(t *testing.T) {
	store := newTestStore(t, 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	state := DesiredState{ServiceName: "api@1.0.0", Action: DesiredActionStart}
	if err := store.PutDesired(ctx, state); !errors.Is(err, context.Canceled) {
		t.Fatalf("PutDesired() error = %v, want context cancellation", err)
	}
	if _, err := store.ListDesired(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("ListDesired() error = %v, want context cancellation", err)
	}
}

func TestWatchDesiredReturnsDisabledError(t *testing.T) {
	store := newTestStore(t, 0)
	if _, err := store.WatchDesired(context.Background()); !errors.Is(err, ErrWatchDisabled) {
		t.Fatalf("WatchDesired() with watch disabled error = %v, want ErrWatchDisabled", err)
	}
}
