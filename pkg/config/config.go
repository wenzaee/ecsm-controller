package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config 是控制器的总配置。
// 当前只收敛已经实现的 registry 和 VSOA 配置项，暂未使用的配置保持空值。
type Config struct {
	Registry RegistryConfig `json:"registry" yaml:"registry"`
	VSOA     VSOAConfig     `json:"vsoa" yaml:"vsoa"`
	ECSM     ECSMConfig     `json:"ecsm" yaml:"ecsm"`
	Scanner  ScannerConfig  `json:"scanner" yaml:"scanner"`
}

// RegistryConfig 是 desired/status 存储相关配置。
type RegistryConfig struct {
	RoseDB RoseDBConfig `json:"rosedb" yaml:"rosedb"`
}

// RoseDBConfig 是 RoseDB 后端配置。
type RoseDBConfig struct {
	DirPath        string `json:"dir_path" yaml:"dir_path"`
	Sync           bool   `json:"sync" yaml:"sync"`
	WatchQueueSize uint64 `json:"watch_queue_size" yaml:"watch_queue_size"`
}

// VSOAConfig 是 VSOA 服务相关配置。
type VSOAConfig struct {
	ListenAddr string `json:"listen_addr" yaml:"listen_addr"`
	Password   string `json:"password" yaml:"password"`
}

// ECSMConfig 是 ECSM HTTP API 采集相关配置。
type ECSMConfig struct {
	Scheme         string `json:"scheme" yaml:"scheme"`
	IP             string `json:"ip" yaml:"ip"`
	Port           int    `json:"port" yaml:"port"`
	TimeoutSeconds int    `json:"timeout_seconds" yaml:"timeout_seconds"`
}

// ScannerConfig 是 RoseDB desired state 兜底扫描配置。
type ScannerConfig struct {
	IntervalSeconds int `json:"interval_seconds" yaml:"interval_seconds"`
}

// Default 返回默认配置。
// 没有明确默认值的配置保持空值，由启动入口或部署配置决定。
func Default() Config {
	return Config{}
}

// Load 从本地 YAML 文件读取配置。
// path 为空时返回 Default，便于没有配置文件的场景继续使用空配置。
func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("read config file %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config file %s: %w", path, err)
	}
	return cfg, nil
}
