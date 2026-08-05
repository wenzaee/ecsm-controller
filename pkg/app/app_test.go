package app

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/wenzaee/ecsm-controller/pkg/config"
)

func TestValidateECSMConfig(t *testing.T) {
	for _, test := range []struct {
		name string
		cfg  config.ECSMConfig
		want bool
	}{
		{name: "missing IP", cfg: config.ECSMConfig{Port: 3001}, want: true},
		{name: "missing port", cfg: config.ECSMConfig{IP: "127.0.0.1"}, want: true},
		{name: "valid", cfg: config.ECSMConfig{IP: "127.0.0.1", Port: 3001}, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := validateECSMConfig(test.cfg); (err != nil) != test.want {
				t.Fatalf("validateECSMConfig() error = %v, want error=%t", err, test.want)
			}
		})
	}
}

func TestCloseHandlesNilApp(t *testing.T) {
	var app *App
	if err := app.Close(); err != nil {
		t.Fatalf("nil App Close() error = %v", err)
	}
}

func TestNewBuildsAndClosesApplication(t *testing.T) {
	app, err := New(config.Config{
		Registry: config.RegistryConfig{RoseDB: config.RoseDBConfig{DirPath: filepath.Join(t.TempDir(), "db")}},
		ECSM:     config.ECSMConfig{IP: "127.0.0.1", Port: 3001},
		Scanner:  config.ScannerConfig{IntervalSeconds: 1},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if app.store == nil || app.server == nil || app.watcher == nil || app.worker == nil || app.scanner == nil {
		t.Fatalf("New() returned incomplete application: %+v", app)
	}
	if err := app.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestRunReturnsServerStartupErrorAndCleansUp(t *testing.T) {
	app, err := New(config.Config{
		Registry: config.RegistryConfig{RoseDB: config.RoseDBConfig{DirPath: filepath.Join(t.TempDir(), "db")}},
		ECSM:     config.ECSMConfig{IP: "127.0.0.1", Port: 3001},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Run(context.Background()); err == nil {
		t.Fatal("Run() unexpectedly succeeded without VSOA address")
	}
	if err := app.Close(); err != nil {
		t.Fatal(err)
	}
}
