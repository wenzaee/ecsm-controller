package vsoa

import "ecsm/pkg/registry"

const (
	// RouteHealthz 是 desired state VSOA 服务的健康检查接口。
	RouteHealthz = "/desired_state/healthz"
	// RouteUpdateDesired 是修改 desired state 的接口。
	RouteUpdateDesired = "/desired_state/update"
	// RouteDeleteDesired 是删除 desired state 的接口。
	RouteDeleteDesired = "/desired_state/delete"
	// RouteQueryDesired 是查询单个服务 desired state 的接口。
	RouteQueryDesired = "/desired_state/query"
	// RouteListDesired 是查询全部 desired state 的接口。
	RouteListDesired = "/desired_state/list"
)

// UpdateDesiredRequest 是修改 desired state 的请求。
type UpdateDesiredRequest struct {
	ServiceName string                 `json:"service_name"`
	Action      registry.DesiredAction `json:"action"`
	Replicas    *int                   `json:"replicas"`
}

// DeleteDesiredRequest 是删除 desired state 的请求。
type DeleteDesiredRequest struct {
	ServiceName string `json:"service_name"`
}

// QueryDesiredRequest 是查询 desired state 的请求。
type QueryDesiredRequest struct {
	ServiceName string `json:"service_name"`
}

// RPCResponse 是通用 VSOA 响应。
type RPCResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

// DesiredResponse 是单个 desired state 查询响应。
type DesiredResponse struct {
	OK      bool                   `json:"ok"`
	Message string                 `json:"message,omitempty"`
	Data    *registry.DesiredState `json:"data,omitempty"`
	Error   string                 `json:"error,omitempty"`
}

// DesiredListResponse 是 desired state 列表查询响应。
type DesiredListResponse struct {
	OK      bool                    `json:"ok"`
	Message string                  `json:"message,omitempty"`
	Total   int                     `json:"total"`
	Data    []registry.DesiredState `json:"data,omitempty"`
	Error   string                  `json:"error,omitempty"`
}
