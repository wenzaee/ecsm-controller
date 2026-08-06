package vsoa

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/acoinfo/vsoa/protocol"
	vsoaServer "github.com/acoinfo/vsoa/server"
	"github.com/wenzaee/ecsm-controller/pkg/registry"
)

func TestRegisterRoutesReportsDuplicateRoutes(t *testing.T) {
	service, _ := NewDesiredStateService(&memoryStore{})
	server, _ := NewServer("127.0.0.1:0", "", service)
	srv := vsoaServer.NewServer("test", vsoaServer.Option{})
	if err := server.registerRoutes(srv); err != nil {
		t.Fatalf("first registerRoutes() error = %v", err)
	}
	if err := server.registerRoutes(srv); err == nil {
		t.Fatal("second registerRoutes() unexpectedly succeeded")
	}
}

func TestStartReportsServeFailure(t *testing.T) {
	service, _ := NewDesiredStateService(&memoryStore{})
	server, _ := NewServer("127.0.0.1:-1", "", service)
	if err := server.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	select {
	case err := <-server.Errors():
		if err == nil {
			t.Fatal("Errors() delivered nil error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for serve failure")
	}
	_ = server.Close()

	// 再次启动并失败时 errCh 已满，应走丢弃分支不阻塞。
	if err := server.Start(); err != nil {
		t.Fatalf("second Start() error = %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	_ = server.Close()
}

func TestStartReturnsRouteRegistrationError(t *testing.T) {
	original := newVSOAServer
	defer func() { newVSOAServer = original }()
	newVSOAServer = func(name string, option vsoaServer.Option) *vsoaServer.Server {
		srv := vsoaServer.NewServer(name, option)
		_ = srv.On(RouteHealthz, protocol.RpcMethodGet, func(_, _ *protocol.Message) {})
		return srv
	}

	service, _ := NewDesiredStateService(&memoryStore{})
	server, _ := NewServer("127.0.0.1:0", "", service)
	if err := server.Start(); err == nil {
		t.Fatal("Start() unexpectedly succeeded with duplicate route")
	}
}

func TestRunReturnsStartError(t *testing.T) {
	service, _ := NewDesiredStateService(&memoryStore{})
	server, _ := NewServer("", "", service)
	if err := server.Run(context.Background()); err == nil {
		t.Fatal("Run() unexpectedly succeeded with empty address")
	}
}

func TestRunReturnsServeError(t *testing.T) {
	service, _ := NewDesiredStateService(&memoryStore{})
	server, _ := NewServer("127.0.0.1:-1", "", service)
	if err := server.Run(context.Background()); err == nil {
		t.Fatal("Run() unexpectedly succeeded with invalid address")
	}
}

func TestWriteResponseFallsBackOnMarshalError(t *testing.T) {
	response := protocol.NewMessage()
	writeResponse(response, make(chan int))
	var value RPCResponse
	if err := json.Unmarshal(response.Param, &value); err != nil {
		t.Fatalf("fallback payload is not RPCResponse: %v", err)
	}
	if value.OK || value.Error == "" {
		t.Fatalf("fallback response = %+v", value)
	}
}

func TestServiceHandlersCoverRemainingBranches(t *testing.T) {
	storeErr := errors.New("store broken")
	service, _ := NewDesiredStateService(&memoryStore{err: storeErr})
	ctx := context.Background()
	replicas := 1
	validPayload := payload(t, UpdateDesiredRequest{
		ServiceName: "api@1.0.0", Action: registry.DesiredActionStart, Replicas: &replicas,
	})

	if resp := service.HandleUpdateDesiredPayload(ctx, validPayload); resp.OK || resp.Error != storeErr.Error() {
		t.Fatalf("update with store error = %+v", resp)
	}
	if resp := service.HandleUpdateDesiredPayload(ctx, payload(t, UpdateDesiredRequest{Action: registry.DesiredActionStart, Replicas: &replicas})); resp.OK || resp.Error == "" {
		t.Fatalf("update with empty service = %+v", resp)
	}
	if resp := service.HandleUpdateDesiredPayload(ctx, payload(t, UpdateDesiredRequest{ServiceName: "api@1.0.0", Action: "restart", Replicas: &replicas})); resp.OK || resp.Error == "" {
		t.Fatalf("update with invalid action = %+v", resp)
	}

	if resp := service.HandleDeleteDesiredPayload(ctx, []byte("not-json")); resp.OK || resp.Error == "" {
		t.Fatalf("delete with invalid payload = %+v", resp)
	}
	if resp := service.HandleDeleteDesiredPayload(ctx, nil); resp.OK || resp.Error == "" {
		t.Fatalf("delete with empty payload = %+v", resp)
	}
	if resp := service.HandleDeleteDesiredPayload(ctx, payload(t, DeleteDesiredRequest{ServiceName: "api@1.0.0"})); resp.OK || resp.Error != storeErr.Error() {
		t.Fatalf("delete with store error = %+v", resp)
	}

	if resp := service.HandleQueryDesiredPayload(ctx, []byte("not-json")); resp.OK || resp.Error == "" {
		t.Fatalf("query with invalid payload = %+v", resp)
	}

	healthy, _ := NewDesiredStateService(&memoryStore{})
	if resp := healthy.HandleDeleteDesiredPayload(ctx, payload(t, DeleteDesiredRequest{ServiceName: "missing@1.0.0"})); resp.OK || resp.Message != "desired state 不存在" {
		t.Fatalf("delete missing = %+v", resp)
	}
}

func TestErrorResponseHelpersMapNotFound(t *testing.T) {
	rpc := errorResponse(registry.ErrNotFound)
	if rpc.OK || rpc.Message != "desired state 不存在" || rpc.Error != registry.ErrNotFound.Error() {
		t.Fatalf("errorResponse() = %+v", rpc)
	}
	desired := desiredErrorResponse(registry.ErrNotFound)
	if desired.OK || desired.Message != "desired state 不存在" {
		t.Fatalf("desiredErrorResponse() = %+v", desired)
	}
	list := desiredListErrorResponse(errors.New("boom"))
	if list.OK || list.Error != "boom" {
		t.Fatalf("desiredListErrorResponse() = %+v", list)
	}
}
