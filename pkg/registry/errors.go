package registry

import "errors"

var (
	// ErrNotFound 表示指定服务的 desired/status 记录不存在。
	ErrNotFound = errors.New("registry: not found")
	// ErrInvalidServiceName 表示服务名为空或包含非法路径分隔符。
	ErrInvalidServiceName = errors.New("registry: invalid service name")
	// ErrInvalidDesiredAction 表示 desired action 不在允许范围内。
	ErrInvalidDesiredAction = errors.New("registry: invalid desired action")
	// ErrInvalidReplicas 表示 replicas 非法。
	ErrInvalidReplicas = errors.New("registry: invalid replicas")
	// ErrWatchDisabled 表示当前 RoseDB 实例未开启 watch 能力。
	ErrWatchDisabled = errors.New("registry: watch disabled")
)
