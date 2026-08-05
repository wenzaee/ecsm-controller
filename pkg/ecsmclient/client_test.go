package ecsmclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wenzaee/ecsm-controller/pkg/registry"
)

func collectorForServer(t *testing.T, server *httptest.Server) *Collector {
	t.Helper()
	host, portText, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	var port int
	if _, err := fmtSscanf(portText, &port); err != nil {
		t.Fatal(err)
	}
	collector, err := NewCollector(Config{IP: host, Port: port, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return collector
}

// fmtSscanf is kept small to avoid leaking test parsing details into production code.
func fmtSscanf(value string, target *int) (int, error) {
	return fmt.Sscanf(value, "%d", target)
}

func writeAPIResponse(t *testing.T, w http.ResponseWriter, data any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"status": http.StatusOK, "data": data}); err != nil {
		t.Fatal(err)
	}
}

func TestCollectorPaginationAndValidation(t *testing.T) {
	if _, err := NewCollector(Config{}); err == nil {
		t.Fatal("NewCollector() unexpectedly accepted an empty config")
	}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != servicePath || r.URL.Query().Get("pageSize") != "50" {
			t.Errorf("unexpected request: %s", r.URL.String())
		}
		pageNum := r.URL.Query().Get("pageNum")
		if pageNum == "1" {
			list := make([]ServiceInfo, 50)
			for i := range list {
				list[i].Name = "service"
			}
			writeAPIResponse(t, w, ServicePage{List: list, Total: 51})
			return
		}
		writeAPIResponse(t, w, ServicePage{List: []ServiceInfo{{Name: "last"}}, Total: 51})
	}))
	defer server.Close()

	collector := collectorForServer(t, server)
	page, err := collector.CollectServices(context.Background())
	if err != nil || len(page.List) != 51 || page.Total != 51 || requests != 2 {
		t.Fatalf("CollectServices() = (%+v, %v), requests=%d", page, err, requests)
	}
	if !strings.Contains(collector.serviceURL(1, 50), "pageNum=1") {
		t.Fatalf("serviceURL() = %s", collector.serviceURL(1, 50))
	}
}

func TestCollectorRejectsHTTPAndAPIErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("pageNum") == "1" {
			http.Error(w, "unavailable", http.StatusBadGateway)
			return
		}
	}))
	defer server.Close()
	collector := collectorForServer(t, server)
	if _, err := collector.CollectServices(context.Background()); err == nil {
		t.Fatal("CollectServices() unexpectedly accepted HTTP failure")
	}
}

func TestLogClientAppliesCreateStartStopAndScale(t *testing.T) {
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/image/config":
			writeAPIResponse(t, w, map[string]any{"config": map[string]any{}})
		case r.Method == http.MethodPost && r.URL.Path == servicePath:
			writeAPIResponse(t, w, ServiceCreateResponse{ID: "created"})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/start/ids"):
			writeAPIResponse(t, w, []string{"service-id"})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/stop/ids"):
			writeAPIResponse(t, w, []string{"service-id"})
		case r.Method == http.MethodGet && r.URL.Path == servicePath+"/service-id":
			writeAPIResponse(t, w, ServiceDetail{ID: "service-id", Name: "worker@1.0.0", Policy: "dynamic"})
		case r.Method == http.MethodPut && r.URL.Path == servicePath:
			writeAPIResponse(t, w, ServiceCreateResponse{ID: "service-id"})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			writeAPIResponse(t, w, nil)
		}
	}))
	defer server.Close()
	client := NewLogClient(collectorForServer(t, server))
	ctx := context.Background()
	desired := registry.DesiredState{ServiceName: "worker@1.0.0", Action: registry.DesiredActionStart, Replicas: 2}
	if err := client.Apply(ctx, Operation{ServiceName: desired.ServiceName, Action: "need_create", Desired: desired}); err != nil {
		t.Fatalf("create Apply() error = %v", err)
	}
	actual := &ServiceInfo{ID: "service-id", InstanceActive: 1, InstanceOnline: 0, Factor: 1}
	if err := client.Apply(ctx, Operation{ServiceName: desired.ServiceName, Action: "need_scale_out", Desired: desired, Actual: actual}); err != nil {
		t.Fatalf("scale Apply() error = %v", err)
	}
	stopDesired := desired
	stopDesired.Action = registry.DesiredActionStop
	if err := client.Apply(ctx, Operation{ServiceName: desired.ServiceName, Action: "need_stop", Desired: stopDesired, Actual: actual}); err != nil {
		t.Fatalf("stop Apply() error = %v", err)
	}
	startActual := &ServiceInfo{ID: "service-id", InstanceActive: 2, InstanceOnline: 0, Factor: 2}
	if err := client.Apply(ctx, Operation{ServiceName: desired.ServiceName, Action: "need_start", Desired: desired, Actual: startActual}); err != nil {
		t.Fatalf("start Apply() error = %v", err)
	}
	if err := client.Apply(ctx, Operation{Action: "none"}); err != nil {
		t.Fatalf("none Apply() error = %v", err)
	}
	if len(requests) < 6 {
		t.Fatalf("expected ECSM write requests, got %v", requests)
	}
}

func TestClientHelpersAndInvalidOperations(t *testing.T) {
	for _, test := range []struct{ input, want string }{
		{"worker:1.0.0", "worker@1.0.0#sylixos"},
		{"worker@1.0.0#linux", "worker@1.0.0#linux"},
	} {
		got, err := normalizeCreateImageRef(test.input)
		if err != nil || got != test.want {
			t.Errorf("normalizeCreateImageRef(%q) = (%q, %v), want %q", test.input, got, err, test.want)
		}
	}
	if _, err := normalizeCreateImageRef("worker"); err == nil {
		t.Fatal("normalizeCreateImageRef() accepted image without version")
	}
	if !isActualRunning(ServiceInfo{ContainerStatusGroup: []string{"RUNNING"}}) {
		t.Fatal("isActualRunning() did not match container status")
	}
	if !replicasDiffer(Operation{Desired: registry.DesiredState{Replicas: 2}, Actual: &ServiceInfo{Factor: 1}}) {
		t.Fatal("replicasDiffer() returned false for distinct factors")
	}
	client := NewLogClient(nil)
	if err := client.Apply(context.Background(), Operation{Action: "unsupported"}); err == nil {
		t.Fatal("Apply() accepted unsupported action")
	}
	if err := client.Apply(context.Background(), Operation{Action: "need_start"}); err == nil {
		t.Fatal("Apply() accepted start without actual service")
	}
	if err := client.Apply(context.Background(), Operation{Action: "need_scale_out", Actual: &ServiceInfo{ID: "id"}}); err == nil {
		t.Fatal("Apply() accepted scale without positive replicas")
	}
}
