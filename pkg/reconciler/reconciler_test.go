package reconciler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/wenzaee/ecsm-controller/pkg/ecsmclient"
	"github.com/wenzaee/ecsm-controller/pkg/queue"
	"github.com/wenzaee/ecsm-controller/pkg/registry"
)

type fakeDesiredReader struct {
	state *registry.DesiredState
	err   error
}

func (f fakeDesiredReader) QueryDesired(context.Context, string) (*registry.DesiredState, error) {
	return f.state, f.err
}

type fakeECSMClient struct {
	mu         sync.Mutex
	page       *ecsmclient.ServicePage
	collectErr error
	applyErr   error
	operations []ecsmclient.Operation
}

func (f *fakeECSMClient) CollectServices(context.Context) (*ecsmclient.ServicePage, error) {
	return f.page, f.collectErr
}
func (f *fakeECSMClient) Apply(_ context.Context, op ecsmclient.Operation) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.operations = append(f.operations, op)
	return f.applyErr
}

func (f *fakeECSMClient) operationCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.operations)
}

func TestCompareDesiredActual(t *testing.T) {
	tests := []struct {
		name    string
		desired *registry.DesiredState
		actual  ecsmclient.ServiceInfo
		found   bool
		want    DiffAction
	}{
		{name: "start creates missing service", desired: &registry.DesiredState{Action: registry.DesiredActionStart, Replicas: 3}, want: DiffNeedCreate},
		{name: "start starts stopped service", desired: &registry.DesiredState{Action: registry.DesiredActionStart}, actual: ecsmclient.ServiceInfo{InstanceActive: 1}, found: true, want: DiffNeedStart},
		{name: "start scales out before starting", desired: &registry.DesiredState{Action: registry.DesiredActionStart, Replicas: 3}, actual: ecsmclient.ServiceInfo{InstanceActive: 1, InstanceOnline: 1}, found: true, want: DiffNeedScaleOut},
		{name: "start scales in", desired: &registry.DesiredState{Action: registry.DesiredActionStart, Replicas: 2}, actual: ecsmclient.ServiceInfo{InstanceActive: 3, InstanceOnline: 3}, found: true, want: DiffNeedScaleIn},
		{name: "stop stops running service", desired: &registry.DesiredState{Action: registry.DesiredActionStop}, actual: ecsmclient.ServiceInfo{InstanceActive: 1, InstanceOnline: 1}, found: true, want: DiffNeedStop},
		{name: "matching running service needs no action", desired: &registry.DesiredState{Action: registry.DesiredActionStart, Replicas: 2}, actual: ecsmclient.ServiceInfo{InstanceActive: 2, InstanceOnline: 2}, found: true, want: DiffNone},
		{name: "nil desired needs no action", want: DiffNone},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CompareDesiredActual(tt.desired, tt.actual, tt.found); got != tt.want {
				t.Fatalf("CompareDesiredActual() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFindActualService(t *testing.T) {
	services := []ecsmclient.ServiceInfo{{Name: "api@1.0.0"}, {Name: "worker@1.0.0"}}
	got, ok := FindActualService(services, "worker@1.0.0")
	if !ok || got.Name != "worker@1.0.0" {
		t.Fatalf("FindActualService() = (%+v, %t), want worker@1.0.0", got, ok)
	}
}

func TestReconcileReadsLatestStateAndAppliesDiff(t *testing.T) {
	desired := &registry.DesiredState{ServiceName: "worker@1.0.0", Action: registry.DesiredActionStart, Replicas: 2}
	client := &fakeECSMClient{page: &ecsmclient.ServicePage{List: []ecsmclient.ServiceInfo{{
		ID: "service-id", Name: desired.ServiceName, InstanceActive: 1, InstanceOnline: 1,
	}}}}
	reconciler := New(fakeDesiredReader{state: desired}, client)
	if err := reconciler.Reconcile(context.Background(), desired.ServiceName); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if len(client.operations) != 1 || client.operations[0].Action != string(DiffNeedScaleOut) || client.operations[0].Actual == nil {
		t.Fatalf("operations = %+v", client.operations)
	}
}

func TestReconcileHandlesMissingAndDependencyFailures(t *testing.T) {
	serviceName := "worker@1.0.0"
	if err := New(fakeDesiredReader{err: registry.ErrNotFound}, &fakeECSMClient{}).Reconcile(context.Background(), serviceName); err != nil {
		t.Fatalf("Reconcile() for deleted desired state error = %v", err)
	}
	queryErr := errors.New("database unavailable")
	if err := New(fakeDesiredReader{err: queryErr}, &fakeECSMClient{}).Reconcile(context.Background(), serviceName); !errors.Is(err, queryErr) {
		t.Fatalf("query failure = %v", err)
	}
	desired := &registry.DesiredState{ServiceName: serviceName, Action: registry.DesiredActionStart}
	collectErr := errors.New("ecsm unavailable")
	if err := New(fakeDesiredReader{state: desired}, &fakeECSMClient{collectErr: collectErr}).Reconcile(context.Background(), serviceName); !errors.Is(err, collectErr) {
		t.Fatalf("collect failure = %v", err)
	}
	applyErr := errors.New("apply failed")
	if err := New(fakeDesiredReader{state: desired}, &fakeECSMClient{page: &ecsmclient.ServicePage{}, applyErr: applyErr}).Reconcile(context.Background(), serviceName); !errors.Is(err, applyErr) {
		t.Fatalf("apply failure = %v", err)
	}
	if err := New(fakeDesiredReader{state: desired}, nil).Reconcile(context.Background(), serviceName); err != nil {
		t.Fatalf("Reconcile() with nil ECSM client error = %v", err)
	}
}

func TestWorkerConsumesQueuedService(t *testing.T) {
	desired := &registry.DesiredState{ServiceName: "worker@1.0.0", Action: registry.DesiredActionStart}
	client := &fakeECSMClient{page: &ecsmclient.ServicePage{}}
	q := queue.NewWorkQueue(1)
	worker := NewWorker(q, New(fakeDesiredReader{state: desired}, client))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		worker.Run(ctx)
		close(done)
	}()
	q.Add(desired.ServiceName)
	deadline := time.After(time.Second)
	for client.operationCount() == 0 {
		select {
		case <-deadline:
			t.Fatal("worker did not process queued service")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop after cancellation")
	}
}
