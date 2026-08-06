package desiredclient

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/acoinfo/vsoa/protocol"
	"github.com/wenzaee/ecsm-controller/pkg/registry"
	desiredvsoa "github.com/wenzaee/ecsm-controller/pkg/vsoa"
)

type blockingRPC struct {
	release chan struct{}
}

func (b *blockingRPC) Call(string, protocol.MessageType, any, *protocol.Message) (*protocol.Message, error) {
	<-b.release
	return nil, errors.New("released")
}
func (b *blockingRPC) Close() error { return nil }

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	return port
}

func TestNewConnectsToRealVSOAServer(t *testing.T) {
	store, err := registry.OpenRoseDB(registry.RoseDBOptions{DirPath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	service, err := desiredvsoa.NewDesiredStateService(store)
	if err != nil {
		t.Fatal(err)
	}
	addr := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	server, err := desiredvsoa.NewServer(addr, "", service)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	time.Sleep(100 * time.Millisecond)

	client, err := New(Option{Address: addr})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	response, err := client.Healthz(context.Background())
	if err != nil || !response.OK {
		t.Fatalf("Healthz() = (%+v, %v)", response, err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestNewFailsWhenServerUnreachable(t *testing.T) {
	_, err := New(Option{Address: "127.0.0.1:1", ConnectTimeout: 200 * time.Millisecond})
	if err == nil {
		t.Fatal("New() unexpectedly connected to unreachable server")
	}
}

func TestCallHandlesNilReplyNilContextAndCancellation(t *testing.T) {
	rpc := &fakeRPCClient{reply: nil}
	client, _ := NewWithRPC(rpc)

	query, err := client.QueryDesired(context.Background(), "api@1.0.0")
	if err != nil || query == nil || query.OK {
		t.Fatalf("QueryDesired() with nil reply = (%+v, %v)", query, err)
	}

	reply := protocol.NewMessage()
	reply.Param = []byte(`{"ok":true}`)
	rpc.reply = reply
	if _, err := client.Healthz(nil); err != nil {
		t.Fatalf("Healthz(nil ctx) error = %v", err)
	}

	blocking := &blockingRPC{release: make(chan struct{})}
	blockingClient, _ := NewWithRPC(blocking)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := blockingClient.ListDesired(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("ListDesired() with cancelled ctx error = %v", err)
	}
	close(blocking.release)

	failing := &fakeRPCClient{err: errors.New("rpc failed")}
	failingClient, _ := NewWithRPC(failing)
	if _, err := failingClient.QueryDesired(context.Background(), "api@1.0.0"); !errors.Is(err, failing.err) {
		t.Fatalf("QueryDesired() error = %v, want rpc error", err)
	}
	if _, err := failingClient.UpdateDesiredRequest(context.Background(), UpdateRequest{}); !errors.Is(err, failing.err) {
		t.Fatalf("UpdateDesiredRequest() error = %v, want rpc error", err)
	}
}

func TestCloseOnNilClients(t *testing.T) {
	var nilClient *Client
	if err := nilClient.Close(); err != nil {
		t.Fatalf("nil Client Close() error = %v", err)
	}
	if err := (&Client{}).Close(); err != nil {
		t.Fatalf("empty Client Close() error = %v", err)
	}
}
