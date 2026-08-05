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

type fakeDesiredLister []registry.DesiredState

func (f fakeDesiredLister) ListDesired(context.Context) ([]registry.DesiredState, error) {
	return f, nil
}

type fakeActualReader struct{ page *ecsmclient.ServicePage }

func (f fakeActualReader) CollectServices(context.Context) (*ecsmclient.ServicePage, error) {
	return f.page, nil
}

type errorDesiredLister struct{ err error }

func (f errorDesiredLister) ListDesired(context.Context) ([]registry.DesiredState, error) {
	return nil, f.err
}

type sequenceActualReader struct {
	pages []*ecsmclient.ServicePage
	errs  []error
	index int
}

func (f *sequenceActualReader) CollectServices(context.Context) (*ecsmclient.ServicePage, error) {
	i := f.index
	f.index++
	return f.pages[i], f.errs[i]
}

func TestScanOnceEnqueuesOnlyServicesThatNeedReconciliation(t *testing.T) {
	q := queue.NewWorkQueue(2)
	s := New(
		fakeDesiredLister{
			{ServiceName: "missing@1.0.0", Action: registry.DesiredActionStart, Replicas: 1},
			{ServiceName: "ready@1.0.0", Action: registry.DesiredActionStart, Replicas: 2},
		},
		fakeActualReader{page: &ecsmclient.ServicePage{List: []ecsmclient.ServiceInfo{{Name: "ready@1.0.0", InstanceActive: 2, InstanceOnline: 2}}}},
		q,
		time.Second,
	)

	if err := s.ScanOnce(context.Background()); err != nil {
		t.Fatalf("ScanOnce() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, ok := q.Get(ctx)
	if !ok || got != "missing@1.0.0" {
		t.Fatalf("queued service = (%q, %t), want missing@1.0.0", got, ok)
	}
	q.Done(got)

	noMoreCtx, noMoreCancel := context.WithCancel(context.Background())
	noMoreCancel()
	if got, ok := q.Get(noMoreCtx); ok || got != "" {
		t.Fatalf("unexpected second queue item = (%q, %t)", got, ok)
	}
}

func TestScannerErrorsValidationAndUsesLastActualState(t *testing.T) {
	q := queue.NewWorkQueue(1)
	listErr := errors.New("registry unavailable")
	if err := New(errorDesiredLister{err: listErr}, fakeActualReader{}, q, time.Second).ScanOnce(context.Background()); !errors.Is(err, listErr) {
		t.Fatalf("ScanOnce() list error = %v", err)
	}
	if err := New(nil, fakeActualReader{}, q, time.Second).ScanOnce(context.Background()); err == nil {
		t.Fatal("ScanOnce() accepted nil desired lister")
	}

	reader := &sequenceActualReader{
		pages: []*ecsmclient.ServicePage{{List: []ecsmclient.ServiceInfo{{Name: "ready@1.0.0", InstanceActive: 1, InstanceOnline: 1}}}, nil},
		errs:  []error{nil, errors.New("temporary ECSM failure")},
	}
	scanner := New(fakeDesiredLister{{ServiceName: "ready@1.0.0", Action: registry.DesiredActionStart, Replicas: 1}}, reader, q, time.Second)
	if err := scanner.ScanOnce(context.Background()); err != nil {
		t.Fatalf("initial ScanOnce() error = %v", err)
	}
	if err := scanner.ScanOnce(context.Background()); err != nil {
		t.Fatalf("ScanOnce() did not reuse last actual page: %v", err)
	}
}

func TestRunStopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	scanner := New(fakeDesiredLister{}, fakeActualReader{page: &ecsmclient.ServicePage{}}, queue.NewWorkQueue(1), time.Second)
	if err := scanner.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}
