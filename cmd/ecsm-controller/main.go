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

// osExit 便于测试拦截进程退出。
var osExit = os.Exit

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	osExit(run(ctx, os.Args[1:]))
}

// run 执行控制器主流程，返回进程退出码。
func run(ctx context.Context, args []string) int {
	flags := flag.NewFlagSet("ecsm-controller", flag.ContinueOnError)
	configPath := flags.String("config", "configs/demo.yaml", "YAML config file path")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Printf("读取配置失败: %v", err)
		return 1
	}

	if err := app.Run(ctx, cfg); err != nil {
		log.Printf("ecsm-controller 退出: %v", err)
		return 1
	}
	return 0
}
