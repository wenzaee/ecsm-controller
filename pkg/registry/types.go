package registry

import "time"

// DesiredAction 表示外部系统写入的服务期望动作。
type DesiredAction string

const (
	DesiredActionStart DesiredAction = "start"
	DesiredActionStop  DesiredAction = "stop"
)

// StatusPhase 表示控制器观察到的服务收敛阶段。
type StatusPhase string

const (
	StatusPhasePending     StatusPhase = "pending"
	StatusPhaseReconciling StatusPhase = "reconciling"
	StatusPhaseReady       StatusPhase = "ready"
	StatusPhaseFailed      StatusPhase = "failed"
)

// DesiredState 是外部系统写入的持久化期望状态。
// Reconciler 每次处理服务时都应该重新读取最新值，避免使用过期事件里的旧数据。
type DesiredState struct {
	ServiceName string        `json:"service_name"`
	Action      DesiredAction `json:"action"`
	Replicas    int           `json:"replicas"`
}

// ServiceStatus 记录 Reconciler 已观察到的版本和最近一次收敛结果。
// 它与 DesiredState 分开存储，便于审计和问题排查。
type ServiceStatus struct {
	ServiceName     string      `json:"service_name"`
	ObservedVersion int64       `json:"observed_version"`
	Phase           StatusPhase `json:"phase"`
	LastAction      string      `json:"last_action,omitempty"`
	LastError       string      `json:"last_error,omitempty"`
	UpdatedAt       time.Time   `json:"updated_at"`
}

// EventAction 表示 RoseDB 里 desired state 的变更类型。
type EventAction string

const (
	EventActionPut    EventAction = "put"
	EventActionDelete EventAction = "delete"
)

// DesiredEvent 是 watcher 对外暴露的轻量事件。
// 事件里只包含服务名和动作，真正执行前必须由 Reconciler 再读取最新 DesiredState。
type DesiredEvent struct {
	ServiceName string
	Action      EventAction
}
