package ecsmclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wenzaee/ecsm-controller/pkg/config"
	"github.com/wenzaee/ecsm-controller/pkg/registry"
)

type staticPageCollector struct {
	page *ServicePage
}

func (s staticPageCollector) CollectServices(context.Context) (*ServicePage, error) {
	return s.page, nil
}

func TestLogClientCollectServicesDelegation(t *testing.T) {
	ctx := context.Background()
	var nilClient *LogClient
	if _, err := nilClient.CollectServices(ctx); err == nil {
		t.Fatal("nil LogClient CollectServices() unexpectedly succeeded")
	}
	if _, err := NewLogClient(nil).CollectServices(ctx); err == nil {
		t.Fatal("CollectServices() accepted nil collector")
	}
	want := &ServicePage{Total: 1}
	page, err := NewLogClient(staticPageCollector{page: want}).CollectServices(ctx)
	if err != nil || page != want {
		t.Fatalf("CollectServices() = (%+v, %v)", page, err)
	}
}

func TestWriteOperationsRequireRealCollector(t *testing.T) {
	ctx := context.Background()
	client := NewLogClient(staticPageCollector{})
	if err := client.Apply(ctx, Operation{Action: "need_create", ServiceName: "worker@1.0.0"}); err == nil {
		t.Fatal("create accepted non-collector implementation")
	}
	if err := client.Apply(ctx, Operation{Action: "need_start", Actual: &ServiceInfo{ID: "id"}}); err == nil {
		t.Fatal("start accepted non-collector implementation")
	}
	scale := Operation{Action: "need_scale_out", Desired: registry.DesiredState{Replicas: 2}, Actual: &ServiceInfo{ID: "id"}}
	if err := client.Apply(ctx, scale); err == nil {
		t.Fatal("scale accepted non-collector implementation")
	}
}

func TestCreateServiceValidationBranches(t *testing.T) {
	ctx := context.Background()
	var imageConfigFail, createFail bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/image/config":
			if imageConfigFail {
				writeAPIResponse(t, w, map[string]any{"config": nil})
				return
			}
			writeAPIResponse(t, w, map[string]any{"config": map[string]any{}})
		case r.Method == http.MethodPost && r.URL.Path == servicePath:
			if createFail {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"status":500,"message":"create rejected"}`))
				return
			}
			writeAPIResponse(t, w, ServiceCreateResponse{ID: "created"})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	client := NewLogClient(collectorForServer(t, server))

	// 两个服务名都为空时报错。
	if err := client.Apply(ctx, Operation{Action: "need_create"}); err == nil {
		t.Fatal("create accepted empty service name")
	}
	// 服务名缺少版本号时报错。
	if err := client.Apply(ctx, Operation{Action: "need_create", ServiceName: "worker"}); err == nil {
		t.Fatal("create accepted image without version")
	}
	// 回退到 Desired.ServiceName 后创建成功。
	desired := registry.DesiredState{ServiceName: "worker:1.0.0", Action: registry.DesiredActionStart, Replicas: 2}
	if err := client.Apply(ctx, Operation{Action: "need_create", Desired: desired}); err != nil {
		t.Fatalf("create with fallback service name error = %v", err)
	}
	// 镜像配置接口失败时报错。
	imageConfigFail = true
	if err := client.Apply(ctx, Operation{Action: "need_create", ServiceName: "worker:1.0.0"}); err == nil {
		t.Fatal("create accepted empty image config")
	}
	imageConfigFail = false
	// 创建接口失败时报错。
	createFail = true
	if err := client.Apply(ctx, Operation{Action: "need_create", ServiceName: "worker:1.0.0"}); err == nil {
		t.Fatal("create accepted failing create API")
	}
}

func scriptedWriteServer(t *testing.T, updateStatus int, updateID string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == servicePath+"/service-id":
			writeAPIResponse(t, w, ServiceDetail{ID: "service-id", Name: "worker@1.0.0"})
		case r.Method == http.MethodPut && r.URL.Path == servicePath:
			if updateStatus != http.StatusOK {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"status":500,"message":"update rejected"}`))
				return
			}
			writeAPIResponse(t, w, ServiceCreateResponse{ID: updateID})
		case r.Method == http.MethodPost && (strings.HasSuffix(r.URL.Path, "/start/ids") || strings.HasSuffix(r.URL.Path, "/stop/ids")):
			writeAPIResponse(t, w, []string{"service-id"})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
}

func TestStartUpdatesReplicasBeforeAction(t *testing.T) {
	server := scriptedWriteServer(t, http.StatusOK, "new-id")
	defer server.Close()
	client := NewLogClient(collectorForServer(t, server))
	op := Operation{
		Action:      "need_start",
		ServiceName: "worker@1.0.0",
		Desired:     registry.DesiredState{Action: registry.DesiredActionStart, Replicas: 3},
		Actual:      &ServiceInfo{ID: "service-id", Factor: 1},
	}
	if err := client.Apply(context.Background(), op); err != nil {
		t.Fatalf("start with replica update error = %v", err)
	}
}

func TestStartStopScaleReportUpdateFailures(t *testing.T) {
	server := scriptedWriteServer(t, http.StatusInternalServerError, "")
	defer server.Close()
	client := NewLogClient(collectorForServer(t, server))
	ctx := context.Background()
	actual := &ServiceInfo{ID: "service-id", Factor: 1}
	desired := registry.DesiredState{Replicas: 3}

	start := Operation{Action: "need_start", Desired: desired, Actual: actual}
	if err := client.Apply(ctx, start); err == nil {
		t.Fatal("start accepted failing replica update")
	}
	stop := Operation{Action: "need_stop", Desired: desired, Actual: actual}
	if err := client.Apply(ctx, stop); err == nil {
		t.Fatal("stop accepted failing replica update")
	}
	scale := Operation{Action: "need_scale_out", Desired: desired, Actual: actual}
	if err := client.Apply(ctx, scale); err == nil {
		t.Fatal("scale accepted failing replica update")
	}
	if err := client.Apply(ctx, Operation{Action: "need_stop"}); err == nil {
		t.Fatal("stop accepted missing actual service")
	}
	if err := client.Apply(ctx, Operation{Action: "need_scale_in"}); err == nil {
		t.Fatal("scale accepted missing actual service")
	}
}

func TestActionAndDetailRequestsReportFailures(t *testing.T) {
	mode := "detail-fail"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == servicePath+"/service-id":
			if mode == "detail-fail" {
				_, _ = w.Write([]byte(`{"status":500,"message":"detail rejected"}`))
				return
			}
			_, _ = w.Write([]byte(`{"status":200,"data":{"id":"service-id","name":"worker@1.0.0"}}`))
		default:
			_, _ = w.Write([]byte(`{"status":500,"message":"action rejected"}`))
		}
	}))
	defer server.Close()
	client := NewLogClient(collectorForServer(t, server))
	ctx := context.Background()
	op := Operation{
		Action:  "need_start",
		Desired: registry.DesiredState{Action: registry.DesiredActionStart, Replicas: 2},
		Actual:  &ServiceInfo{ID: "service-id", Factor: 1},
	}
	if err := client.Apply(ctx, op); err == nil {
		t.Fatal("start accepted failing service detail")
	}
	mode = "action-fail"
	noUpdate := op
	noUpdate.Actual = &ServiceInfo{ID: "service-id", Factor: 2}
	if err := client.Apply(ctx, noUpdate); err == nil {
		t.Fatal("start accepted failing action API")
	}
}

func TestScaleKeepsOriginalIDWhenUpdateReturnsNone(t *testing.T) {
	server := scriptedWriteServer(t, http.StatusOK, "")
	defer server.Close()
	client := NewLogClient(collectorForServer(t, server))
	ctx := context.Background()

	stoppedScale := Operation{
		Action:  "need_scale_out",
		Desired: registry.DesiredState{Action: registry.DesiredActionStop, Replicas: 2},
		Actual:  &ServiceInfo{ID: "service-id", Factor: 1},
	}
	if err := client.Apply(ctx, stoppedScale); err != nil {
		t.Fatalf("scale on stopped desired error = %v", err)
	}

	runningScale := Operation{
		Action:  "need_scale_in",
		Desired: registry.DesiredState{Action: registry.DesiredActionStart, Replicas: 2},
		Actual:  &ServiceInfo{ID: "service-id", Factor: 1, InstanceOnline: 1},
	}
	if err := client.Apply(ctx, runningScale); err != nil {
		t.Fatalf("scale on running service error = %v", err)
	}

	startScale := Operation{
		Action:  "need_scale_out",
		Desired: registry.DesiredState{Action: registry.DesiredActionStart, Replicas: 2},
		Actual:  &ServiceInfo{ID: "service-id", Factor: 1},
	}
	if err := client.Apply(ctx, startScale); err != nil {
		t.Fatalf("scale then start error = %v", err)
	}
}

func TestDoJSONRequestErrorBranches(t *testing.T) {
	mode := "ok"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch mode {
		case "garbage":
			_, _ = w.Write([]byte("not-json"))
		case "api-fail":
			_, _ = w.Write([]byte(`{"status":500,"message":"boom"}`))
		case "null-data":
			_, _ = w.Write([]byte(`{"status":200,"data":null}`))
		case "bad-data":
			_, _ = w.Write([]byte(`{"status":200,"data":123}`))
		default:
			_, _ = w.Write([]byte(`{"status":200,"data":{"id":"x"}}`))
		}
	}))
	defer server.Close()
	collector := collectorForServer(t, server)
	ctx := context.Background()
	var out ServiceCreateResponse

	if err := collector.doJSONRequest(ctx, http.MethodPost, servicePath, nil, make(chan int), &out); err == nil {
		t.Fatal("doJSONRequest() accepted unmarshalable body")
	}
	if err := collector.doJSONRequest(ctx, "BAD METHOD", servicePath, nil, nil, &out); err == nil {
		t.Fatal("doJSONRequest() accepted invalid method")
	}

	mode = "garbage"
	if err := collector.doJSONRequest(ctx, http.MethodGet, servicePath, nil, nil, &out); err == nil {
		t.Fatal("doJSONRequest() accepted non-JSON response")
	}
	mode = "api-fail"
	if err := collector.doJSONRequest(ctx, http.MethodGet, servicePath, nil, nil, &out); err == nil {
		t.Fatal("doJSONRequest() accepted failing API status")
	}
	mode = "null-data"
	if err := collector.doJSONRequest(ctx, http.MethodGet, servicePath, nil, nil, &out); err != nil {
		t.Fatalf("doJSONRequest() with null data error = %v", err)
	}
	mode = "ok"
	if err := collector.doJSONRequest(ctx, http.MethodGet, servicePath, nil, nil, nil); err != nil {
		t.Fatalf("doJSONRequest() with nil out error = %v", err)
	}
	mode = "bad-data"
	if err := collector.doJSONRequest(ctx, http.MethodGet, servicePath, nil, nil, &out); err == nil {
		t.Fatal("doJSONRequest() accepted invalid data payload")
	}

	server.Close()
	if err := collector.doJSONRequest(ctx, http.MethodGet, servicePath, nil, nil, &out); err == nil {
		t.Fatal("doJSONRequest() accepted unreachable server")
	}
}

func TestClientHelperValidationBranches(t *testing.T) {
	for _, input := range []string{"", ":1.0.0", "worker:"} {
		if _, err := normalizeCreateImageRef(input); err == nil {
			t.Fatalf("normalizeCreateImageRef(%q) unexpectedly succeeded", input)
		}
	}
	if !isActualRunning(ServiceInfo{InstanceOnline: 1}) {
		t.Fatal("isActualRunning() ignored online instances")
	}
	if replicasDiffer(Operation{Actual: nil, Desired: registry.DesiredState{Replicas: 2}}) {
		t.Fatal("replicasDiffer() returned true without actual service")
	}
	if replicasDiffer(Operation{Actual: &ServiceInfo{Factor: 1}, Desired: registry.DesiredState{}}) {
		t.Fatal("replicasDiffer() returned true without desired replicas")
	}
	if replicasDiffer(Operation{Actual: &ServiceInfo{InstanceActive: 2}, Desired: registry.DesiredState{Replicas: 2}}) {
		t.Fatal("replicasDiffer() returned true for matching active instances")
	}
	if !replicasDiffer(Operation{Actual: &ServiceInfo{InstanceActive: 1}, Desired: registry.DesiredState{Replicas: 2}}) {
		t.Fatal("replicasDiffer() returned false for distinct active instances")
	}
}

func TestCollectorDetailAndImageConfigErrors(t *testing.T) {
	mode := "ok"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch mode {
		case "empty-config":
			writeAPIResponse(t, w, map[string]any{"config": nil})
		case "fail":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":500,"message":"boom"}`))
		default:
			writeAPIResponse(t, w, ServiceDetail{ID: "id"})
		}
	}))
	defer server.Close()
	collector := collectorForServer(t, server)
	ctx := context.Background()

	if _, err := collector.getServiceDetailByID(ctx, ""); err == nil {
		t.Fatal("getServiceDetailByID() accepted empty id")
	}
	if _, err := collector.getImageConfig(ctx, " "); err == nil {
		t.Fatal("getImageConfig() accepted empty ref")
	}
	mode = "fail"
	if _, err := collector.getServiceDetailByID(ctx, "id"); err == nil {
		t.Fatal("getServiceDetailByID() accepted failing API")
	}
	if _, err := collector.getImageConfig(ctx, "worker@1.0.0"); err == nil {
		t.Fatal("getImageConfig() accepted failing API")
	}
	mode = "empty-config"
	if _, err := collector.getImageConfig(ctx, "worker@1.0.0"); err == nil {
		t.Fatal("getImageConfig() accepted empty config payload")
	}

	server.Close()
	if _, err := collector.getServiceDetailByID(ctx, "id"); err == nil {
		t.Fatal("getServiceDetailByID() accepted unreachable server")
	}
}

func TestEnsureDefaultImageConfigFillsPartialConfig(t *testing.T) {
	cfg := &EcsImageConfig{
		SylixOS: &SylixOS{Resources: &Resources{
			CPU:          &CPU{},
			Memory:       &Memory{},
			Disk:         &Disk{},
			KernelObject: &KernelObject{},
		}},
	}
	got := ensureDefaultImageConfig(cfg)
	if got.SylixOS.Resources.CPU.HighestPrio != 160 || got.SylixOS.Resources.CPU.LowestPrio != 250 || got.SylixOS.Resources.CPU.DefaultPrio != 200 {
		t.Fatalf("CPU defaults = %+v", got.SylixOS.Resources.CPU)
	}
	if got.SylixOS.Resources.Memory.KheapLimit == 0 || got.SylixOS.Resources.Memory.MemoryLimitMB != 2048 {
		t.Fatalf("Memory defaults = %+v", got.SylixOS.Resources.Memory)
	}
	if got.SylixOS.Resources.Disk.LimitMB != 2048 {
		t.Fatalf("Disk defaults = %+v", got.SylixOS.Resources.Disk)
	}
	if got.SylixOS.Resources.KernelObject.ThreadLimit != 4096 || got.SylixOS.Resources.KernelObject.TimerLimit != 64 {
		t.Fatalf("KernelObject defaults = %+v", got.SylixOS.Resources.KernelObject)
	}
}

func TestNewCollectorRejectsMissingPort(t *testing.T) {
	if _, err := NewCollector(Config{IP: "127.0.0.1"}); err == nil {
		t.Fatal("NewCollector() accepted missing port")
	}
}

func TestNewCollectorFromConfigUsesProjectConfig(t *testing.T) {
	collector, err := NewCollectorFromConfig(config.ECSMConfig{IP: "127.0.0.1", Port: 8080, TimeoutSeconds: 1})
	if err != nil || collector == nil {
		t.Fatalf("NewCollectorFromConfig() = (%v, %v)", collector, err)
	}
	if _, err := NewCollectorFromConfig(config.ECSMConfig{}); err == nil {
		t.Fatal("NewCollectorFromConfig() accepted empty config")
	}
}

func TestCollectServicesBreaksWhenTotalReached(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		list := make([]ServiceInfo, defaultServicePageSize)
		writeAPIResponse(t, w, ServicePage{List: list, Total: defaultServicePageSize})
	}))
	defer server.Close()
	collector := collectorForServer(t, server)
	page, err := collector.CollectServices(context.Background())
	if err != nil || len(page.List) != defaultServicePageSize || requests != 1 {
		t.Fatalf("CollectServices() = (%d items, %v), requests=%d", len(page.List), err, requests)
	}
}

func TestCollectServicesFallsBackToCollectedTotal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeAPIResponse(t, w, ServicePage{List: []ServiceInfo{{Name: "only"}}, Total: 0})
	}))
	defer server.Close()
	collector := collectorForServer(t, server)
	page, err := collector.CollectServices(context.Background())
	if err != nil || page.Total != 1 {
		t.Fatalf("CollectServices() = (%+v, %v)", page, err)
	}
}

func TestCollectServicesRequestAndResponseErrors(t *testing.T) {
	badScheme := &Collector{
		cfg:        Config{Scheme: "\x00", IP: "127.0.0.1", Port: 80},
		httpClient: &http.Client{},
	}
	if _, err := badScheme.CollectServices(context.Background()); err == nil {
		t.Fatal("CollectServices() accepted invalid base URL")
	}

	mode := "ok"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch mode {
		case "garbage":
			_, _ = w.Write([]byte("not-json"))
		case "api-fail":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":500,"message":"boom","data":{}}`))
		default:
			writeAPIResponse(t, w, ServicePage{})
		}
	}))
	collector := collectorForServer(t, server)

	mode = "garbage"
	if _, err := collector.CollectServices(context.Background()); err == nil {
		t.Fatal("CollectServices() accepted non-JSON response")
	}
	mode = "api-fail"
	if _, err := collector.CollectServices(context.Background()); err == nil {
		t.Fatal("CollectServices() accepted failing API status")
	}
	server.Close()
	if _, err := collector.CollectServices(context.Background()); err == nil {
		t.Fatal("CollectServices() accepted unreachable server")
	}
}
