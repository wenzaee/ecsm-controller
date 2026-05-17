package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/wenzaee/ecsm-controller/pkg/app"
	"github.com/wenzaee/ecsm-controller/pkg/config"
)

func main() {
	configPath := flag.String("config", "configs/demo.yaml", "YAML config file path")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("读取配置失败: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := app.Run(ctx, cfg); err != nil {
		log.Fatalf("ecsm-controller 退出: %v", err)
	}
}
