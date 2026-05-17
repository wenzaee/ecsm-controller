package desiredwatcher

import (
	"context"
	"log"

	"ecsm/pkg/queue"
	"ecsm/pkg/registry"
)

// Watcher 只监听 desired state 变更，并把 serviceName 放入工作队列。
// 它不读取 ECSM 实际状态，也不直接调用 ECSM 控制接口。
type Watcher struct {
	store registry.DesiredStore
	queue *queue.WorkQueue
}

// New 创建 desired state watcher。
func New(store registry.DesiredStore, workQueue *queue.WorkQueue) *Watcher {
	if workQueue == nil {
		workQueue = queue.NewWorkQueue(128)
	}
	return &Watcher{
		store: store,
		queue: workQueue,
	}
}

// Run 持续监听 desired state 变更，直到 context 取消或事件通道关闭。
func (w *Watcher) Run(ctx context.Context) error {
	events, err := w.store.WatchDesired(ctx)
	if err != nil {
		return err
	}

	log.Println("[INFO] desired watcher 已启动，等待 desired state 变更")

	for {
		select {
		case <-ctx.Done():
			log.Println("[INFO] desired watcher 收到退出信号")
			return nil
		case event, ok := <-events:
			if !ok {
				log.Println("[WARN] desired event channel 已关闭")
				return nil
			}
			log.Printf("[INFO] desired 变更: service=%s action=%s", event.ServiceName, event.Action)

			// 这里只入队，不直接调用 ECSM。
			// 真正执行应由 Reconciler 重新读取最新 desired state 和 ECSM 实际状态后统一收敛。
			w.queue.Add(event.ServiceName)
		}
	}
}
