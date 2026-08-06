package main

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/acoinfo/vsoa/protocol"
	"github.com/wenzaee/ecsm-controller/pkg/desiredclient"
	"github.com/wenzaee/ecsm-controller/pkg/registry"
	desiredvsoa "github.com/wenzaee/ecsm-controller/pkg/vsoa"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

type scriptRPC struct {
	reply    *protocol.Message
	closeErr error
}

func (s *scriptRPC) Call(string, protocol.MessageType, any, *protocol.Message) (*protocol.Message, error) {
	return s.reply, nil
}

func (s *scriptRPC) Close() error { return s.closeErr }

func startDesiredServer(t *testing.T) string {
	t.Helper()
	store, err := registry.OpenRoseDB(registry.RoseDBOptions{DirPath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	service, err := desiredvsoa.NewDesiredStateService(store)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()

	server, err := desiredvsoa.NewServer(addr, "", service)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	time.Sleep(100 * time.Millisecond)
	return addr
}

func TestRunCLIArgumentAndConfigErrors(t *testing.T) {
	out := &bytes.Buffer{}
	if code := runCLI(context.Background(), []string{"-bad-flag"}, out); code != 2 {
		t.Fatalf("runCLI() with bad flag = %d, want 2", code)
	}
	if code := runCLI(context.Background(), []string{"-action", "restart", "-config", ""}, out); code != 1 {
		t.Fatalf("runCLI() with unsupported action = %d, want 1", code)
	}
	if code := runCLI(context.Background(), []string{"-config", filepath.Join(t.TempDir(), "missing.yaml")}, out); code != 1 {
		t.Fatalf("runCLI() with missing config = %d, want 1", code)
	}
	if code := runCLI(context.Background(), []string{"-config", "", "-addr", "127.0.0.1:1"}, out); code != 1 {
		t.Fatalf("runCLI() with unreachable server = %d, want 1", code)
	}
}

func TestRunCLIActionsAgainstRealServer(t *testing.T) {
	addr := startDesiredServer(t)
	base := []string{"-config", "", "-addr", addr}
	cases := []struct {
		name string
		args []string
		want string
	}{
		{name: "healthz", args: []string{"-action", "healthz"}, want: "ok"},
		{name: "update-by-flags", args: []string{"-action", "update", "-service", "api@1.0.0", "-replicas", "2"}, want: "ok"},
		{name: "update-by-payload", args: []string{"-action", "update", "-payload", `{"service_name":"api@2.0.0","action":"stop"}`}, want: "ok"},
		{name: "query", args: []string{"-action", "query", "-service", "api@1.0.0"}, want: "api@1.0.0"},
		{name: "list", args: []string{"-action", "list"}, want: "total"},
		{name: "delete", args: []string{"-action", "delete", "-service", "api@1.0.0"}, want: "ok"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := &bytes.Buffer{}
			if code := runCLI(context.Background(), append(base, tc.args...), out); code != 0 {
				t.Fatalf("runCLI() = %d, output: %s", code, out.String())
			}
			if !strings.Contains(out.String(), tc.want) {
				t.Fatalf("output = %s, want contains %q", out.String(), tc.want)
			}
		})
	}

	out := &bytes.Buffer{}
	badPayload := append(base, "-action", "update", "-payload", "{not-json")
	if code := runCLI(context.Background(), badPayload, out); code != 1 {
		t.Fatalf("runCLI() with invalid payload = %d, want 1", code)
	}
}

func TestRunCLIReportsCallAndWriterFailures(t *testing.T) {
	addr := startDesiredServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if code := runCLI(ctx, []string{"-config", "", "-addr", addr}, &bytes.Buffer{}); code != 1 {
		t.Fatalf("runCLI() with cancelled context = %d, want 1", code)
	}
	if code := runCLI(context.Background(), []string{"-config", "", "-addr", addr}, failingWriter{}); code != 1 {
		t.Fatalf("runCLI() with failing writer = %d, want 1", code)
	}
}

func TestRunCLILogsCloseFailures(t *testing.T) {
	original := newClient
	newClient = func(desiredclient.Option) (*desiredclient.Client, error) {
		reply := protocol.NewMessage()
		reply.Param = []byte(`{"ok":true}`)
		return desiredclient.NewWithRPC(&scriptRPC{reply: reply, closeErr: errors.New("close failed")})
	}
	t.Cleanup(func() { newClient = original })

	out := &bytes.Buffer{}
	if code := runCLI(context.Background(), []string{"-config", ""}, out); code != 0 {
		t.Fatalf("runCLI() = %d, output: %s", code, out.String())
	}
}

func TestPrintJSONReportsMarshalErrors(t *testing.T) {
	if err := printJSON(&bytes.Buffer{}, make(chan int)); err == nil {
		t.Fatal("printJSON() accepted unmarshalable value")
	}
}

func TestMainInvokesRunCLI(t *testing.T) {
	var code int
	originalExit := osExit
	originalArgs := os.Args
	osExit = func(c int) { code = c; panic("exit") }
	os.Args = []string{"desired-vsoa-client", "-action", "restart", "-config", ""}
	defer func() {
		recover()
		osExit = originalExit
		os.Args = originalArgs
		if code != 1 {
			t.Fatalf("main() exit code = %d, want 1", code)
		}
	}()
	main()
}
