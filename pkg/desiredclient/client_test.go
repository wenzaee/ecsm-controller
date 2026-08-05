package desiredclient

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/acoinfo/vsoa/protocol"
	"github.com/wenzaee/ecsm-controller/pkg/registry"
	desiredvsoa "github.com/wenzaee/ecsm-controller/pkg/vsoa"
)

type fakeRPCClient struct {
	mu       sync.Mutex
	paths    []string
	methods  []protocol.RpcMessageType
	payloads [][]byte
	reply    *protocol.Message
	err      error
	closed   bool
}

func (f *fakeRPCClient) Call(path string, _ protocol.MessageType, flags any, req *protocol.Message) (*protocol.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paths = append(f.paths, path)
	if method, ok := flags.(protocol.RpcMessageType); ok {
		f.methods = append(f.methods, method)
	}
	f.payloads = append(f.payloads, append([]byte(nil), req.Param...))
	return f.reply, f.err
}
func (f *fakeRPCClient) Close() error { f.closed = true; return nil }

func rpcReply(t *testing.T, value any) *protocol.Message {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	message := protocol.NewMessage()
	message.Param = data
	return message
}

func TestClientCallsAndDecodesResponses(t *testing.T) {
	rpc := &fakeRPCClient{reply: rpcReply(t, desiredvsoa.RPCResponse{OK: true, Message: "ok"})}
	client, err := NewWithRPC(rpc)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Start(context.Background(), " worker@1.0.0 ", 2)
	if err != nil || !response.OK {
		t.Fatalf("Start() = (%+v, %v)", response, err)
	}
	if len(rpc.paths) != 1 || rpc.paths[0] != desiredvsoa.RouteUpdateDesired || rpc.methods[0] != protocol.RpcMethodSet {
		t.Fatalf("recorded RPC call = paths=%v methods=%v", rpc.paths, rpc.methods)
	}
	var request desiredvsoa.UpdateDesiredRequest
	if err := json.Unmarshal(rpc.payloads[0], &request); err != nil {
		t.Fatal(err)
	}
	if request.ServiceName != "worker@1.0.0" || request.Action != ActionStart || request.Replicas == nil || *request.Replicas != 2 {
		t.Fatalf("update request = %+v", request)
	}

	rpc.reply = rpcReply(t, desiredvsoa.DesiredResponse{OK: true, Data: &registry.DesiredState{ServiceName: "worker@1.0.0"}})
	query, err := client.QueryDesired(context.Background(), "worker@1.0.0")
	if err != nil || !query.OK || query.Data == nil {
		t.Fatalf("QueryDesired() = (%+v, %v)", query, err)
	}
	if err := client.Close(); err != nil || !rpc.closed {
		t.Fatalf("Close() = %v, closed=%t", err, rpc.closed)
	}
	rpc.reply = rpcReply(t, desiredvsoa.RPCResponse{OK: true})
	if response, err := client.Healthz(context.Background()); err != nil || !response.OK {
		t.Fatalf("Healthz() = (%+v, %v)", response, err)
	}
	if response, err := client.Stop(context.Background(), "worker@1.0.0", 0); err != nil || !response.OK {
		t.Fatalf("Stop() = (%+v, %v)", response, err)
	}
	if response, err := client.DeleteDesired(context.Background(), "worker@1.0.0"); err != nil || !response.OK {
		t.Fatalf("DeleteDesired() = (%+v, %v)", response, err)
	}
	rpc.reply = rpcReply(t, desiredvsoa.DesiredListResponse{OK: true, Total: 1})
	if response, err := client.ListDesired(context.Background()); err != nil || !response.OK || response.Total != 1 {
		t.Fatalf("ListDesired() = (%+v, %v)", response, err)
	}
}

func TestClientErrorPathsAndRequestBuilder(t *testing.T) {
	if _, err := NewWithRPC(nil); err == nil {
		t.Fatal("NewWithRPC(nil) unexpectedly succeeded")
	}
	if _, err := New(Option{}); err == nil {
		t.Fatal("New(Option{}) unexpectedly succeeded")
	}
	request, err := NewUpdateRequest(" api@1.0.0 ", ActionStop, 0)
	if err != nil || request.ServiceName != "api@1.0.0" || request.Replicas == nil {
		t.Fatalf("NewUpdateRequest() = (%+v, %v)", request, err)
	}

	client := &Client{}
	if _, err := client.Healthz(context.Background()); err == nil {
		t.Fatal("Healthz() on uninitialized client unexpectedly succeeded")
	}
	rpc := &fakeRPCClient{err: errors.New("network failed")}
	client, _ = NewWithRPC(rpc)
	if _, err := client.DeleteDesired(context.Background(), "api@1.0.0"); !errors.Is(err, rpc.err) {
		t.Fatalf("DeleteDesired() error = %v, want RPC error", err)
	}
	rpc.err = nil
	rpc.reply = protocol.NewMessage()
	rpc.reply.Param = []byte("{")
	if _, err := client.Healthz(context.Background()); err == nil {
		t.Fatal("Healthz() unexpectedly decoded malformed response")
	}
}
