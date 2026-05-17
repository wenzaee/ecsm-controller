package scanner

import (
	"context"
	"fmt"
	"log"
	"time"

	"ecsm/pkg/ecsmclient"
	"ecsm/pkg/queue"
	"ecsm/pkg/reconciler"
	"ecsm/pkg/registry"
)

const defaultInterval = 30 * time.Second

// DesiredLister 是 scanner 读取 RoseDB 中全部期望状态所需的接口。
type DesiredLister interface {
	ListDesired(ctx context.Context) ([]registry.DesiredState, error)
}

// ActualReader 是 scanner 读取 ECSM 当前真实状态所需的接口。
type ActualReader interface {
	CollectServices(ctx context.Context) (*ecsmclient.ServicePage, error)
}

// Scanner 定时扫描 RoseDB 中的 desired state，并和 ECSM 实际状态做比对。
// 发现不一致时只将 serviceName 放入工作队列，真正收敛由 reconciler worker 统一完成。
type Scanner struct {
	desired  DesiredLister
	actual   ActualReader
	queue    *queue.WorkQueue
	interval time.Duration
}

// New 创建定时扫描器。
func New(desired DesiredLister, actual ActualReader, workQueue *queue.WorkQueue, interval time.Duration) *Scanner {
	if interval <= 0 {
		interval = defaultInterval
	}
	return &Scanner{
		desired:  desired,
		actual:   actual,
		queue:    workQueue,
		interval: interval,
	}
}

// Run 启动定时扫描，直到 context 取消。
func (s *Scanner) Run(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if err := s.validate(); err != nil {
		return err
	}

	log.Printf("[INFO] scanner 已启动，扫描周期: %s", s.interval)
	s.scanAndLog(ctx)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[INFO] scanner 已停止")
			return nil
		case <-ticker.C:
			s.scanAndLog(ctx)
		}
	}
}

// ScanOnce 执行一次扫描，便于主流程启动时或测试中主动触发。
func (s *Scanner) ScanOnce(ctx context.Context) error {
	if err := s.validate(); err != nil {
		return err
	}

	desiredList, err := s.desired.ListDesired(ctx)
	if err != nil {
		return fmt.Errorf("list desired states: %w", err)
	}
	page, err := s.actual.CollectServices(ctx)
	if err != nil {
		return fmt.Errorf("collect actual services: %w", err)
	}

	log.Printf("[INFO] scanner 开始比对: desired=%d actual=%d", len(desiredList), len(page.List))
	for _, desired := range desiredList {
		if desired.ServiceName == "" {
			log.Println("[WARN] scanner 跳过 serviceName 为空的 desired state")
			continue
		}

		actual, found := reconciler.FindActualService(page.List, desired.ServiceName)
		diff := reconciler.CompareDesiredActual(&desired, actual, found)
		if diff == reconciler.DiffNone {
			log.Printf("[INFO] scanner 比对一致: service=%s", desired.ServiceName)
			continue
		}

		log.Printf("[WARN] scanner 发现不一致: service=%s diff=%s，已放入工作队列",
			desired.ServiceName,
			diff,
		)
		s.queue.Add(desired.ServiceName)
	}
	return nil
}

func (s *Scanner) scanAndLog(ctx context.Context) {
	if err := s.ScanOnce(ctx); err != nil {
		log.Printf("[WARN] scanner 扫描失败: %v", err)
	}
}

func (s *Scanner) validate() error {
	if s.desired == nil {
		return fmt.Errorf("desired lister is nil")
	}
	if s.actual == nil {
		return fmt.Errorf("actual reader is nil")
	}
	if s.queue == nil {
		return fmt.Errorf("work queue is nil")
	}
	return nil
}
