package queue

import (
	"context"
	"log"
	"sync"
)

// WorkQueue 是按 serviceName 去重的简单工作队列。
type WorkQueue struct {
	mu      sync.Mutex
	pending map[string]struct{}
	items   []string
	notify  chan struct{}
}

// NewWorkQueue 创建工作队列。
func NewWorkQueue(size int) *WorkQueue {
	if size <= 0 {
		size = 128
	}
	return &WorkQueue{
		pending: make(map[string]struct{}),
		items:   make([]string, 0, size),
		notify:  make(chan struct{}),
	}
}

// Add 将服务名放入队列；若同服务已经待处理，则合并重复事件。
func (q *WorkQueue) Add(serviceName string) {
	q.mu.Lock()
	if _, ok := q.pending[serviceName]; ok {
		q.mu.Unlock()
		log.Printf("[INFO] queue 重复事件已合并: service=%s", serviceName)
		return
	}

	oldCap := cap(q.items)
	wasEmpty := len(q.items) == 0
	q.pending[serviceName] = struct{}{}
	q.items = append(q.items, serviceName)
	newLen := len(q.items)
	newCap := cap(q.items)
	if wasEmpty {
		close(q.notify)
		q.notify = make(chan struct{})
	}
	q.mu.Unlock()

	if newCap > oldCap {
		log.Printf("[INFO] queue 自动扩容: old_cap=%d new_cap=%d", oldCap, newCap)
	}
	log.Printf("[INFO] queue 入队: service=%s len=%d cap=%d", serviceName, newLen, newCap)
}

// Get 从队列中取出一个服务名。
func (q *WorkQueue) Get(ctx context.Context) (string, bool) {
	for {
		q.mu.Lock()
		if len(q.items) > 0 {
			serviceName := q.items[0]
			q.items[0] = ""
			q.items = q.items[1:]
			q.mu.Unlock()
			return serviceName, true
		}
		notify := q.notify
		q.mu.Unlock()

		select {
		case <-ctx.Done():
			return "", false
		case <-notify:
		}
	}
}

// Done 标记服务处理完成，允许同一个 serviceName 后续再次入队。
func (q *WorkQueue) Done(serviceName string) {
	q.mu.Lock()
	delete(q.pending, serviceName)
	q.mu.Unlock()
}
