package scanner

import (
	"context"
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
