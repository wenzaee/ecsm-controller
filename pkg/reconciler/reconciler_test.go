package reconciler

import (
	"testing"

	"github.com/wenzaee/ecsm-controller/pkg/ecsmclient"
	"github.com/wenzaee/ecsm-controller/pkg/registry"
)

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
