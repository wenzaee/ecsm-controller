package app

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/wenzaee/ecsm-controller/pkg/config"
	"github.com/wenzaee/ecsm-controller/pkg/desiredwatcher"
	"github.com/wenzaee/ecsm-controller/pkg/ecsmclient"
	"github.com/wenzaee/ecsm-controller/pkg/queue"
	"github.com/wenzaee/ecsm-controller/pkg/reconciler"
	"github.com/wenzaee/ecsm-controller/pkg/registry"
	"github.com/wenzaee/ecsm-controller/pkg/scanner"
	desiredvsoa "github.com/wenzaee/ecsm-controller/pkg/vsoa"
)

// App 是工程入口组装后的运行实例。
// 它负责持有 RoseDB store 和 VSOA server 的生命周期。
type App struct {
	cfg     config.Config
	store   registry.Store
	server  *desiredvsoa.Server
	watcher *desiredwatcher.Watcher
	queue   *queue.WorkQueue
	recon   *reconciler.Reconciler
	worker  *reconciler.Worker
	scanner *scanner.Scanner
}

// New 根据配置创建 App。
func New(cfg config.Config) (*App, error) {
	if err := validateECSMConfig(cfg.ECSM); err != nil {
		return nil, err
	}

	store, err := registry.OpenRoseDB(registry.RoseDBOptions{
		DirPath:        cfg.Registry.RoseDB.DirPath,
		Sync:           cfg.Registry.RoseDB.Sync,
		WatchQueueSize: cfg.Registry.RoseDB.WatchQueueSize,
	})
	if err != nil {
		return nil, fmt.Errorf("open rosedb store: %w", err)
	}

	// store 非空时 NewDesiredStateService 不会失败，这里忽略错误。
	service, _ := desiredvsoa.NewDesiredStateService(store)

	// service 非空时 NewServer 不会失败，这里忽略错误。
	server, _ := desiredvsoa.NewServer(cfg.VSOA.ListenAddr, cfg.VSOA.Password, service)

	// ECSM 配置已在入口校验过 IP/Port，NewCollectorFromConfig 不会失败。
	collectClient, _ := ecsmclient.NewCollectorFromConfig(cfg.ECSM)

	workQueue := queue.NewWorkQueue(128)
	ecsmClient := ecsmclient.NewLogClient(collectClient)
	recon := reconciler.New(store, ecsmClient)
	worker := reconciler.NewWorker(workQueue, recon)
	scanInterval := time.Duration(cfg.Scanner.IntervalSeconds) * time.Second
	stateScanner := scanner.New(store, ecsmClient, workQueue, scanInterval)

	return &App{
		cfg:     cfg,
		store:   store,
		server:  server,
		watcher: desiredwatcher.New(store, workQueue),
		queue:   workQueue,
		recon:   recon,
		worker:  worker,
		scanner: stateScanner,
	}, nil
}

// Run 启动工程主流程，直到 context 取消或 VSOA 服务返回错误。
func (a *App) Run(ctx context.Context) error {
	log.Printf("[INFO] ecsm-controller 启动: vsoa=%s rosedb=%s",
		a.cfg.VSOA.ListenAddr,
		a.cfg.Registry.RoseDB.DirPath,
	)

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, 4)
	var wg sync.WaitGroup
	start := func(run func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errCh <- run()
		}()
	}

	start(func() error {
		return a.watcher.Run(runCtx)
	})
	start(func() error {
		a.worker.Run(runCtx)
		return nil
	})
	start(func() error {
		return a.scanner.Run(runCtx)
	})
	start(func() error {
		return a.server.Run(runCtx)
	})

	doneCh := make(chan struct{})
	go func() {
		wg.Wait()
		close(doneCh)
	}()

	select {
	case <-ctx.Done():
		cancel()
		<-doneCh
		return nil
	case err := <-errCh:
		cancel()
		<-doneCh
		return err
	}
}

func validateECSMConfig(cfg config.ECSMConfig) error {
	if cfg.IP == "" {
		return fmt.Errorf("ecsm ip is required")
	}
	if cfg.Port <= 0 {
		return fmt.Errorf("ecsm port is required")
	}
	return nil
}

// Close 关闭 App 持有的资源。
func (a *App) Close() error {
	if a == nil {
		return nil
	}
	return closeAll(a.server, a.store)
}

// closeAll 依次关闭资源，返回第一个出现的错误。
func closeAll(closers ...interface{ Close() error }) error {
	var firstErr error
	for _, closer := range closers {
		if closer == nil {
			continue
		}
		if err := closer.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// newAppInstance 创建 App 实例，便于测试注入 Close 失败等场景。
var newAppInstance = New

// Run 根据配置创建并运行 App。
func Run(ctx context.Context, cfg config.Config) error {
	app, err := newAppInstance(cfg)
	if err != nil {
		return err
	}
	defer func() {
		if err := app.Close(); err != nil {
			log.Printf("[WARN] close app failed: %v", err)
		}
	}()
	return app.Run(ctx)
}
