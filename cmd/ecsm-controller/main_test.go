package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func freeAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()
	return addr
}

func TestRunExitCodes(t *testing.T) {
	if code := run(context.Background(), []string{"-bad-flag"}); code != 2 {
		t.Fatalf("run() with bad flag = %d, want 2", code)
	}
	if code := run(context.Background(), []string{"-config", filepath.Join(t.TempDir(), "missing.yaml")}); code != 1 {
		t.Fatalf("run() with missing config = %d, want 1", code)
	}
	if code := run(context.Background(), []string{"-config", ""}); code != 1 {
		t.Fatalf("run() with empty config = %d, want 1", code)
	}
}

func TestRunStartsAndStopsController(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	content := fmt.Sprintf(`registry:
  rosedb:
    dir_path: %s
    watch_queue_size: 8
vsoa:
  listen_addr: %s
ecsm:
  ip: 127.0.0.1
  port: 3001
scanner:
  interval_seconds: 1
`, filepath.Join(dir, "db"), freeAddr(t))
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if code := run(ctx, []string{"-config", configPath}); code != 0 {
		t.Fatalf("run() = %d, want 0", code)
	}
}

func TestMainInvokesRun(t *testing.T) {
	var code int
	originalExit := osExit
	originalArgs := os.Args
	osExit = func(c int) { code = c; panic("exit") }
	os.Args = []string{"ecsm-controller", "-config", ""}
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
