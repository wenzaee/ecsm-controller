package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wenzaee/ecsm-controller/pkg/config"
	"github.com/wenzaee/ecsm-controller/pkg/registry"
)

func freeListenAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()
	return addr
}

func fullConfig(t *testing.T) config.Config {
	t.Helper()
	return config.Config{
		Registry: config.RegistryConfig{RoseDB: config.RoseDBConfig{
			DirPath:        filepath.Join(t.TempDir(), "db"),
			WatchQueueSize: 8,
		}},
		VSOA:    config.VSOAConfig{ListenAddr: freeListenAddr(t)},
		ECSM:    config.ECSMConfig{IP: "127.0.0.1", Port: 3001},
		Scanner: config.ScannerConfig{IntervalSeconds: 1},
	}
}

func TestNewRejectsInvalidECSMConfig(t *testing.T) {
	if _, err := New(config.Config{}); err == nil {
		t.Fatal("New() accepted missing ECSM config")
	}
}

func TestNewReportsStoreOpenFailure(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "db")
	if err := os.WriteFile(blocker, []byte("block"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := fullConfig(t)
	cfg.Registry.RoseDB.DirPath = blocker
	if _, err := New(cfg); err == nil {
		t.Fatal("New() accepted unusable rosedb dir path")
	}
}

func TestRunStopsWhenContextCancelled(t *testing.T) {
	app, err := New(fullConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx) }()
	time.Sleep(200 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run() did not stop after cancellation")
	}
}

type okCloser struct{}

func (okCloser) Close() error { return nil }

type failCloser struct{ err error }

func (f failCloser) Close() error { return f.err }

type failingStore struct {
	registry.Store
	err error
}

func (f failingStore) Close() error { return f.err }

func TestCloseAllSkipsNilAndReturnsFirstError(t *testing.T) {
	if err := closeAll(nil); err != nil {
		t.Fatalf("closeAll(nil) error = %v", err)
	}
	first := errors.New("first failure")
	second := errors.New("second failure")
	if err := closeAll(okCloser{}, failCloser{err: first}, failCloser{err: second}); !errors.Is(err, first) {
		t.Fatalf("closeAll() error = %v, want first failure", err)
	}
}

func TestPackageRunReportsConfigError(t *testing.T) {
	if err := Run(context.Background(), config.Config{}); err == nil {
		t.Fatal("Run() accepted invalid config")
	}
}

func TestPackageRunLogsCloseFailures(t *testing.T) {
	original := newAppInstance
	newAppInstance = func(cfg config.Config) (*App, error) {
		app, err := New(cfg)
		if err != nil {
			return nil, err
		}
		app.store = failingStore{err: fmt.Errorf("store close failed")}
		return app, nil
	}
	t.Cleanup(func() { newAppInstance = original })

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := Run(ctx, fullConfig(t)); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}
