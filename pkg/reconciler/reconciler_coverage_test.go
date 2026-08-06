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

func TestCompareDesiredActualRemainingBranches(t *testing.T) {
	runningByStatus := ecsmclient.ServiceInfo{ContainerStatusGroup: []string{"running"}}
	tests := []struct {
		name    string
		desired *registry.DesiredState
		actual  ecsmclient.ServiceInfo
		found   bool
		want    DiffAction
	}{
		{name: "start with zero replicas matches running service", desired: &registry.DesiredState{Action: registry.DesiredActionStart}, actual: ecsmclient.ServiceInfo{InstanceOnline: 1}, found: true, want: DiffNone},
		{name: "start with matching replicas starts stopped service", desired: &registry.DesiredState{Action: registry.DesiredActionStart, Replicas: 2}, actual: ecsmclient.ServiceInfo{InstanceActive: 2}, found: true, want: DiffNeedStart},
		{name: "start with zero replicas starts service reported running by status", desired: &registry.DesiredState{Action: registry.DesiredActionStart}, actual: runningByStatus, found: true, want: DiffNone},
		{name: "stop ignores missing service", desired: &registry.DesiredState{Action: registry.DesiredActionStop}, want: DiffNone},
		{name: "stop scales out first when replicas below desired", desired: &registry.DesiredState{Action: registry.DesiredActionStop, Replicas: 3}, actual: ecsmclient.ServiceInfo{InstanceActive: 1}, found: true, want: DiffNeedScaleOut},
		{name: "stop scales in first when replicas above desired", desired: &registry.DesiredState{Action: registry.DesiredActionStop, Replicas: 1}, actual: ecsmclient.ServiceInfo{InstanceActive: 3}, found: true, want: DiffNeedScaleIn},
		{name: "stop on stopped service needs no action", desired: &registry.DesiredState{Action: registry.DesiredActionStop}, actual: ecsmclient.ServiceInfo{InstanceActive: 1}, found: true, want: DiffNone},
		{name: "unknown action needs no action", desired: &registry.DesiredState{Action: "restart"}, found: true, want: DiffNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CompareDesiredActual(tt.desired, tt.actual, tt.found); got != tt.want {
				t.Fatalf("CompareDesiredActual() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsActualRunningUsesContainerStatus(t *testing.T) {
	if isActualRunning(ecsmclient.ServiceInfo{}) {
		t.Fatal("isActualRunning() returned true for empty service")
	}
	if !isActualRunning(ecsmclient.ServiceInfo{ContainerStatusGroup: []string{"Running"}}) {
		t.Fatal("isActualRunning() did not match container status group")
	}
	if got := actualReplicaCount(ecsmclient.ServiceInfo{InstanceActive: 4}); got != 4 {
		t.Fatalf("actualReplicaCount() = %d", got)
	}
}

func TestReconcileSkipsWhenDiffIsNone(t *testing.T) {
	desired := &registry.DesiredState{ServiceName: "worker@1.0.0", Action: registry.DesiredActionStop}
	client := &fakeECSMClient{page: &ecsmclient.ServicePage{}}
	if err := New(fakeDesiredReader{state: desired}, client).Reconcile(context.Background(), desired.ServiceName); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if client.operationCount() != 0 {
		t.Fatalf("Reconcile() applied %d operations, want none", client.operationCount())
	}
}

func TestReconcileCreatesMissingServiceWithoutActual(t *testing.T) {
	desired := &registry.DesiredState{ServiceName: "worker@1.0.0", Action: registry.DesiredActionStart, Replicas: 1}
	client := &fakeECSMClient{page: &ecsmclient.ServicePage{}}
	if err := New(fakeDesiredReader{state: desired}, client).Reconcile(context.Background(), desired.ServiceName); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if client.operationCount() != 1 || client.operations[0].Action != string(DiffNeedCreate) || client.operations[0].Actual != nil {
		t.Fatalf("operations = %+v", client.operations)
	}
}

type failingReader struct {
	mu    sync.Mutex
	calls int
}

func (f *failingReader) QueryDesired(context.Context, string) (*registry.DesiredState, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	return nil, errors.New("query failed")
}

func (f *failingReader) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func TestWorkerLogsAndContinuesAfterReconcileFailure(t *testing.T) {
	q := queue.NewWorkQueue(1)
	reader := &failingReader{}
	worker := NewWorker(q, New(reader, &fakeECSMClient{}))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		worker.Run(ctx)
		close(done)
	}()

	q.Add("worker@1.0.0")
	deadline := time.After(time.Second)
	for reader.callCount() == 0 {
		select {
		case <-deadline:
			t.Fatal("worker did not process failing item")
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
