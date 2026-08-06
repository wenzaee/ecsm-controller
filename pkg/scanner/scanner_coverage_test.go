package scanner

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wenzaee/ecsm-controller/pkg/ecsmclient"
	"github.com/wenzaee/ecsm-controller/pkg/queue"
	"github.com/wenzaee/ecsm-controller/pkg/registry"
)

func TestNewAppliesDefaultInterval(t *testing.T) {
	s := New(fakeDesiredLister{}, fakeActualReader{}, queue.NewWorkQueue(1), 0)
	if s.interval != defaultInterval {
		t.Fatalf("New() interval = %s, want default %s", s.interval, defaultInterval)
	}
}

func TestRunValidatesDependencies(t *testing.T) {
	var nilScanner *Scanner
	if err := nilScanner.Run(context.Background()); err != nil {
		t.Fatalf("nil Scanner Run() error = %v", err)
	}
	s := &Scanner{desired: fakeDesiredLister{}, actual: fakeActualReader{}}
	if err := s.Run(context.Background()); err == nil {
		t.Fatal("Run() accepted nil work queue")
	}
	noActual := &Scanner{desired: fakeDesiredLister{}, queue: queue.NewWorkQueue(1)}
	if err := noActual.Run(context.Background()); err == nil {
		t.Fatal("Run() accepted nil actual reader")
	}
}

func TestRunScansPeriodicallyUntilCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := New(
		fakeDesiredLister{{ServiceName: "", Action: registry.DesiredActionStart}, {ServiceName: "missing@1.0.0", Action: registry.DesiredActionStart, Replicas: 1}},
		fakeActualReader{page: &ecsmclient.ServicePage{}},
		queue.NewWorkQueue(4),
		10*time.Millisecond,
	)
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not stop after cancellation")
	}
}

func TestRunLogsScanFailuresAndContinues(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := New(errorDesiredLister{err: errors.New("registry down")}, fakeActualReader{}, queue.NewWorkQueue(1), 10*time.Millisecond)
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not stop after cancellation")
	}
}

func TestScanOnceSkipsEmptyServiceName(t *testing.T) {
	q := queue.NewWorkQueue(1)
	s := New(
		fakeDesiredLister{{ServiceName: "", Action: registry.DesiredActionStart, Replicas: 1}},
		fakeActualReader{page: &ecsmclient.ServicePage{}},
		q,
		time.Second,
	)
	if err := s.ScanOnce(context.Background()); err != nil {
		t.Fatalf("ScanOnce() error = %v", err)
	}
	emptyCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, ok := q.Get(emptyCtx); ok || got != "" {
		t.Fatalf("queue unexpectedly contains (%q, %t)", got, ok)
	}
}

func TestScanOnceFailsWithoutLastActualPage(t *testing.T) {
	reader := &sequenceActualReader{pages: []*ecsmclient.ServicePage{nil}, errs: []error{errors.New("ecsm down")}}
	s := New(fakeDesiredLister{{ServiceName: "api@1.0.0", Action: registry.DesiredActionStart}}, reader, queue.NewWorkQueue(1), time.Second)
	if err := s.ScanOnce(context.Background()); err == nil {
		t.Fatal("ScanOnce() unexpectedly succeeded without any cached actual page")
	}
}
