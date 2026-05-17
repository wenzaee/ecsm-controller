package registry

import "context"

// DesiredStore 抽象 desired state 的持久化读写能力。
// 上层控制器只依赖这个接口，后续可以替换 RoseDB 为其他存储。
type DesiredStore interface {
	PutDesired(ctx context.Context, state DesiredState) error
	UpdateDesired(ctx context.Context, state DesiredState) error
	GetDesired(ctx context.Context, serviceName string) (*DesiredState, error)
	QueryDesired(ctx context.Context, serviceName string) (*DesiredState, error)
	DeleteDesired(ctx context.Context, serviceName string) error
	ListDesired(ctx context.Context) ([]DesiredState, error)
	WatchDesired(ctx context.Context) (<-chan DesiredEvent, error)
}

// StatusStore 抽象服务收敛状态的持久化读写能力。
type StatusStore interface {
	PutStatus(ctx context.Context, status ServiceStatus) error
	GetStatus(ctx context.Context, serviceName string) (*ServiceStatus, error)
	DeleteStatus(ctx context.Context, serviceName string) error
	ListStatus(ctx context.Context) ([]ServiceStatus, error)
}

// Store 是 DesiredStore 和 StatusStore 的组合接口。
type Store interface {
	DesiredStore
	StatusStore
	Close() error
}
