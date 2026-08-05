package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadParsesYAMLConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "controller.yaml")
	yaml := "ecsm:\n  scheme: http\n  ip: 127.0.0.1\n  port: 3001\nscanner:\n  interval_seconds: 5\n"
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ECSM.IP != "127.0.0.1" || cfg.ECSM.Port != 3001 || cfg.Scanner.IntervalSeconds != 5 {
		t.Fatalf("Load() = %+v, configuration values not parsed", cfg)
	}
}

func TestLoadWithoutPathReturnsDefault(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\") error = %v", err)
	}
	if cfg != Default() {
		t.Fatalf("Load(\"\") = %+v, want default %+v", cfg, Default())
	}
}
