package vsoa

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"ecsm/pkg/registry"
)

// DesiredStateStore 是 VSOA 层依赖的最小存储接口。
type DesiredStateStore interface {
	UpdateDesired(ctx context.Context, state registry.DesiredState) error
	QueryDesired(ctx context.Context, serviceName string) (*registry.DesiredState, error)
	DeleteDesired(ctx context.Context, serviceName string) error
	ListDesired(ctx context.Context) ([]registry.DesiredState, error)
}

// DesiredStateService 负责把 VSOA JSON payload 转换成 registry 存储操作。
type DesiredStateService struct {
	store DesiredStateStore
}

// NewDesiredStateService 创建 desired state VSOA 服务。
func NewDesiredStateService(store DesiredStateStore) (*DesiredStateService, error) {
	if store == nil {
		return nil, fmt.Errorf("desired state store is nil")
	}
	return &DesiredStateService{store: store}, nil
}

// HandleHealthzPayload 处理健康检查请求。
func (s *DesiredStateService) HandleHealthzPayload(_ context.Context, _ []byte) RPCResponse {
	return RPCResponse{OK: true, Message: "desired state vsoa 服务运行正常"}
}

// HandleUpdateDesiredPayload 处理 desired state 修改请求。
func (s *DesiredStateService) HandleUpdateDesiredPayload(ctx context.Context, payload []byte) RPCResponse {
	var req UpdateDesiredRequest
	if err := decodePayload(payload, &req); err != nil {
		return errorResponse(err)
	}
	if req.Replicas == nil {
		return errorResponse(fmt.Errorf("replicas is required"))
	}
	state := registry.DesiredState{
		ServiceName: req.ServiceName,
		Action:      req.Action,
		Replicas:    *req.Replicas,
	}
	if err := s.store.UpdateDesired(ctx, state); err != nil {
		return errorResponse(err)
	}
	return RPCResponse{OK: true, Message: "desired state 修改成功"}
}

// HandleDeleteDesiredPayload 处理 desired state 删除请求。
func (s *DesiredStateService) HandleDeleteDesiredPayload(ctx context.Context, payload []byte) RPCResponse {
	var req DeleteDesiredRequest
	if err := decodePayload(payload, &req); err != nil {
		return errorResponse(err)
	}
	if err := s.store.DeleteDesired(ctx, req.ServiceName); err != nil {
		return errorResponse(err)
	}
	return RPCResponse{OK: true, Message: "desired state 删除成功"}
}

// HandleQueryDesiredPayload 处理单服务 desired state 查询请求。
func (s *DesiredStateService) HandleQueryDesiredPayload(ctx context.Context, payload []byte) DesiredResponse {
	var req QueryDesiredRequest
	if err := decodePayload(payload, &req); err != nil {
		return desiredErrorResponse(err)
	}
	state, err := s.store.QueryDesired(ctx, req.ServiceName)
	if err != nil {
		return desiredErrorResponse(err)
	}
	return DesiredResponse{OK: true, Message: "desired state 查询成功", Data: state}
}

// HandleListDesiredPayload 处理 desired state 列表查询请求。
func (s *DesiredStateService) HandleListDesiredPayload(ctx context.Context, _ []byte) DesiredListResponse {
	states, err := s.store.ListDesired(ctx)
	if err != nil {
		return desiredListErrorResponse(err)
	}
	return DesiredListResponse{OK: true, Message: "desired state 列表查询成功", Total: len(states), Data: states}
}

func decodePayload(payload []byte, out any) error {
	if len(payload) == 0 {
		return fmt.Errorf("request payload is empty")
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("decode request payload: %w", err)
	}
	return nil
}

func errorResponse(err error) RPCResponse {
	resp := RPCResponse{OK: false, Error: err.Error()}
	if errors.Is(err, registry.ErrNotFound) {
		resp.Message = "desired state 不存在"
	}
	return resp
}

func desiredErrorResponse(err error) DesiredResponse {
	resp := DesiredResponse{OK: false, Error: err.Error()}
	if errors.Is(err, registry.ErrNotFound) {
		resp.Message = "desired state 不存在"
	}
	return resp
}

func desiredListErrorResponse(err error) DesiredListResponse {
	return DesiredListResponse{OK: false, Error: err.Error()}
}
