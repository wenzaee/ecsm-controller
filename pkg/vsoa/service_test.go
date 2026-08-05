package vsoa

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/acoinfo/vsoa/protocol"
	"github.com/wenzaee/ecsm-controller/pkg/registry"
)

type memoryStore struct {
	states map[string]registry.DesiredState
	err    error
}

func (s *memoryStore) UpdateDesired(_ context.Context, state registry.DesiredState) error {
	if s.err != nil {
		return s.err
	}
	if s.states == nil {
		s.states = make(map[string]registry.DesiredState)
	}
	s.states[state.ServiceName] = state
	return nil
}
func (s *memoryStore) QueryDesired(_ context.Context, name string) (*registry.DesiredState, error) {
	if s.err != nil {
		return nil, s.err
	}
	state, ok := s.states[name]
	if !ok {
		return nil, registry.ErrNotFound
	}
	return &state, nil
}
func (s *memoryStore) DeleteDesired(_ context.Context, name string) error {
	if s.err != nil {
		return s.err
	}
	if _, ok := s.states[name]; !ok {
		return registry.ErrNotFound
	}
	delete(s.states, name)
	return nil
}
func (s *memoryStore) ListDesired(_ context.Context) ([]registry.DesiredState, error) {
	if s.err != nil {
		return nil, s.err
	}
	states := make([]registry.DesiredState, 0, len(s.states))
	for _, state := range s.states {
		states = append(states, state)
	}
	return states, nil
}

func payload(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestDesiredStateServiceCRUDHandlers(t *testing.T) {
	store := &memoryStore{}
	service, err := NewDesiredStateService(store)
	if err != nil {
		t.Fatal(err)
	}
	replicas := 3
	update := service.HandleUpdateDesiredPayload(context.Background(), payload(t, UpdateDesiredRequest{
		ServiceName: "worker@1.0.0", Action: registry.DesiredActionStart, Replicas: &replicas,
	}))
	if !update.OK {
		t.Fatalf("update response = %+v", update)
	}
	query := service.HandleQueryDesiredPayload(context.Background(), payload(t, QueryDesiredRequest{ServiceName: "worker@1.0.0"}))
	if !query.OK || query.Data == nil || query.Data.Replicas != 3 {
		t.Fatalf("query response = %+v", query)
	}
	list := service.HandleListDesiredPayload(context.Background(), nil)
	if !list.OK || list.Total != 1 {
		t.Fatalf("list response = %+v", list)
	}
	deleted := service.HandleDeleteDesiredPayload(context.Background(), payload(t, DeleteDesiredRequest{ServiceName: "worker@1.0.0"}))
	if !deleted.OK {
		t.Fatalf("delete response = %+v", deleted)
	}
	missing := service.HandleQueryDesiredPayload(context.Background(), payload(t, QueryDesiredRequest{ServiceName: "worker@1.0.0"}))
	if missing.OK || missing.Message != "desired state 不存在" {
		t.Fatalf("missing query response = %+v", missing)
	}
}

func TestDesiredStateServiceValidationAndStoreErrors(t *testing.T) {
	if _, err := NewDesiredStateService(nil); err == nil {
		t.Fatal("NewDesiredStateService(nil) unexpectedly succeeded")
	}
	service, _ := NewDesiredStateService(&memoryStore{err: errors.New("storage unavailable")})
	if response := service.HandleUpdateDesiredPayload(context.Background(), []byte("not-json")); response.OK || response.Error == "" {
		t.Fatalf("invalid payload response = %+v", response)
	}
	if response := service.HandleUpdateDesiredPayload(context.Background(), payload(t, UpdateDesiredRequest{ServiceName: "api@1", Action: registry.DesiredActionStart})); response.OK || response.Error == "" {
		t.Fatalf("missing replicas response = %+v", response)
	}
	if response := service.HandleListDesiredPayload(context.Background(), nil); response.OK || response.Error == "" {
		t.Fatalf("store failure response = %+v", response)
	}
	if response := service.HandleHealthzPayload(context.Background(), nil); !response.OK {
		t.Fatalf("health response = %+v", response)
	}
}

func TestServerHelpersAndValidation(t *testing.T) {
	service, _ := NewDesiredStateService(&memoryStore{})
	if _, err := NewServer(":1234", "", nil); err == nil {
		t.Fatal("NewServer() accepted nil service")
	}
	server, err := NewServer("   ", " password ", service)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Start(); err == nil {
		t.Fatal("Start() accepted empty address")
	}
	if server.context() == nil {
		t.Fatal("context() returned nil")
	}
	ctx := context.WithValue(context.Background(), "request-id", "abc")
	server.setContext(ctx)
	if server.context().Value("request-id") != "abc" {
		t.Fatal("setContext() value was not retained")
	}
	if err := server.Close(); err != nil {
		t.Fatalf("Close() before Start() error = %v", err)
	}

	request := protocol.NewMessage()
	request.Data = []byte("data")
	if string(requestPayload(request)) != "data" {
		t.Fatal("requestPayload() did not fall back to Data")
	}
	request.Param = []byte("param")
	if string(requestPayload(request)) != "param" || requestPayload(nil) != nil {
		t.Fatal("requestPayload() did not prioritize Param")
	}
	response := protocol.NewMessage()
	writeResponse(response, RPCResponse{OK: true, Message: "ok"})
	var value RPCResponse
	if err := json.Unmarshal(response.Param, &value); err != nil || !value.OK || response.Data != nil {
		t.Fatalf("writeResponse() = (%+v, %v)", value, err)
	}
}

func TestServerRouteHandlersSerializeServiceResponses(t *testing.T) {
	service, _ := NewDesiredStateService(&memoryStore{})
	server, _ := NewServer(":1234", "", service)
	updateReq := protocol.NewMessage()
	replicas := 1
	updateReq.Param = payload(t, UpdateDesiredRequest{ServiceName: "api@1.0.0", Action: registry.DesiredActionStart, Replicas: &replicas})
	updateResp := protocol.NewMessage()
	server.handleUpdateDesired(updateReq, updateResp)
	var update RPCResponse
	if err := json.Unmarshal(updateResp.Param, &update); err != nil || !update.OK {
		t.Fatalf("update route response = %+v, %v", update, err)
	}
	for _, handler := range []func(*protocol.Message, *protocol.Message){server.handleHealthz, server.handleListDesired} {
		response := protocol.NewMessage()
		handler(protocol.NewMessage(), response)
		if len(response.Param) == 0 {
			t.Fatal("route handler did not write a response")
		}
	}
	queryReq := protocol.NewMessage()
	queryReq.Param = payload(t, QueryDesiredRequest{ServiceName: "api@1.0.0"})
	queryResp := protocol.NewMessage()
	server.handleQueryDesired(queryReq, queryResp)
	if len(queryResp.Param) == 0 {
		t.Fatal("query handler did not write a response")
	}
	deleteReq := protocol.NewMessage()
	deleteReq.Param = payload(t, DeleteDesiredRequest{ServiceName: "api@1.0.0"})
	deleteResp := protocol.NewMessage()
	server.handleDeleteDesired(deleteReq, deleteResp)
	if len(deleteResp.Param) == 0 {
		t.Fatal("delete handler did not write a response")
	}
}

func TestServerRunStartsRoutesAndStopsOnCancellation(t *testing.T) {
	service, _ := NewDesiredStateService(&memoryStore{})
	server, err := NewServer("127.0.0.1:0", "", service)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := server.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if server.server != nil {
		t.Fatal("Run() did not close server after context cancellation")
	}
	if server.Errors() == nil {
		t.Fatal("Errors() returned nil channel")
	}
}
