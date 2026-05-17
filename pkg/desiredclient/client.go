package desiredclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"ecsm/pkg/registry"
	desiredvsoa "ecsm/pkg/vsoa"

	vsoaclient "github.com/acoinfo/vsoa/client"
	"github.com/acoinfo/vsoa/protocol"
)

const defaultConnectTimeout = 3 * time.Second

// Action 表示外部系统写入的服务期望动作。
type Action = registry.DesiredAction

const (
	ActionStart Action = registry.DesiredActionStart
	ActionStop  Action = registry.DesiredActionStop
)

// Response 是通用 Desired State 响应。
type Response = desiredvsoa.RPCResponse

// DesiredResponse 是单个服务 desired state 查询响应。
type DesiredResponse = desiredvsoa.DesiredResponse

// DesiredListResponse 是 desired state 列表查询响应。
type DesiredListResponse = desiredvsoa.DesiredListResponse

// UpdateRequest 是修改 desired state 的完整请求。
type UpdateRequest = desiredvsoa.UpdateDesiredRequest

// RPCClient 是 Client 底层依赖的最小 VSOA RPC 接口。
type RPCClient interface {
	Call(URL string, mt protocol.MessageType, flags any, req *protocol.Message) (*protocol.Message, error)
	Close() error
}

// Option 是 Desired State Client 的连接配置。
type Option struct {
	Address        string
	Password       string
	ConnectTimeout time.Duration
	AutoReconnect  bool
}

// Client 是提供给外部项目使用的 Desired State 客户端。
type Client struct {
	rpc RPCClient
}

// New 创建并连接 Desired State VSOA 服务。
func New(opt Option) (*Client, error) {
	address := strings.TrimSpace(opt.Address)
	if address == "" {
		return nil, fmt.Errorf("address is required")
	}

	connectTimeout := opt.ConnectTimeout
	if connectTimeout <= 0 {
		connectTimeout = defaultConnectTimeout
	}

	inner := vsoaclient.NewClient(vsoaclient.Option{
		Password:       strings.TrimSpace(opt.Password),
		AutoReconnect:  opt.AutoReconnect,
		ConnectTimeout: connectTimeout,
	})
	if _, err := inner.Connect("tcp", address); err != nil {
		return nil, fmt.Errorf("connect desired state vsoa server: %w", err)
	}
	return &Client{rpc: inner}, nil
}

// NewWithRPC 用指定 RPCClient 创建客户端，主要用于测试或适配已有连接。
func NewWithRPC(rpc RPCClient) (*Client, error) {
	if rpc == nil {
		return nil, fmt.Errorf("rpc client is nil")
	}
	return &Client{rpc: rpc}, nil
}

// Close 关闭底层 VSOA 连接。
func (c *Client) Close() error {
	if c == nil || c.rpc == nil {
		return nil
	}
	return c.rpc.Close()
}

// Healthz 检查 Desired State VSOA 服务是否可用。
func (c *Client) Healthz(ctx context.Context) (*Response, error) {
	var resp desiredvsoa.RPCResponse
	if err := c.call(ctx, desiredvsoa.RouteHealthz, protocol.RpcMethodGet, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// UpdateDesired 修改指定服务的期望状态。
func (c *Client) UpdateDesired(ctx context.Context, serviceName string, action Action, replicas int) (*Response, error) {
	req, err := NewUpdateRequest(serviceName, action, replicas)
	if err != nil {
		return nil, err
	}
	return c.UpdateDesiredRequest(ctx, req)
}

// UpdateDesiredRequest 使用完整请求修改指定服务的期望状态。
func (c *Client) UpdateDesiredRequest(ctx context.Context, req UpdateRequest) (*Response, error) {
	if req.Replicas == nil {
		return nil, fmt.Errorf("replicas is required")
	}
	var resp desiredvsoa.RPCResponse
	if err := c.call(ctx, desiredvsoa.RouteUpdateDesired, protocol.RpcMethodSet, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Start 设置服务期望动作为 start。
func (c *Client) Start(ctx context.Context, serviceName string, replicas int) (*Response, error) {
	return c.UpdateDesired(ctx, serviceName, ActionStart, replicas)
}

// Stop 设置服务期望动作为 stop。
func (c *Client) Stop(ctx context.Context, serviceName string, replicas int) (*Response, error) {
	return c.UpdateDesired(ctx, serviceName, ActionStop, replicas)
}

// QueryDesired 查询指定服务的期望状态。
func (c *Client) QueryDesired(ctx context.Context, serviceName string) (*DesiredResponse, error) {
	var resp desiredvsoa.DesiredResponse
	req := desiredvsoa.QueryDesiredRequest{ServiceName: strings.TrimSpace(serviceName)}
	if err := c.call(ctx, desiredvsoa.RouteQueryDesired, protocol.RpcMethodGet, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// DeleteDesired 删除指定服务的期望状态。
func (c *Client) DeleteDesired(ctx context.Context, serviceName string) (*Response, error) {
	var resp desiredvsoa.RPCResponse
	req := desiredvsoa.DeleteDesiredRequest{ServiceName: strings.TrimSpace(serviceName)}
	if err := c.call(ctx, desiredvsoa.RouteDeleteDesired, protocol.RpcMethodSet, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ListDesired 查询全部服务的期望状态。
func (c *Client) ListDesired(ctx context.Context) (*DesiredListResponse, error) {
	var resp desiredvsoa.DesiredListResponse
	if err := c.call(ctx, desiredvsoa.RouteListDesired, protocol.RpcMethodGet, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// NewUpdateRequest 构造修改期望状态的请求。
func NewUpdateRequest(serviceName string, action Action, replicas int) (UpdateRequest, error) {
	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		return desiredvsoa.UpdateDesiredRequest{}, fmt.Errorf("service name is required")
	}
	switch action {
	case registry.DesiredActionStart, registry.DesiredActionStop:
	default:
		return desiredvsoa.UpdateDesiredRequest{}, registry.ErrInvalidDesiredAction
	}
	if replicas < 0 {
		return desiredvsoa.UpdateDesiredRequest{}, registry.ErrInvalidReplicas
	}
	return desiredvsoa.UpdateDesiredRequest{
		ServiceName: serviceName,
		Action:      action,
		Replicas:    &replicas,
	}, nil
}

func (c *Client) call(ctx context.Context, path string, method protocol.RpcMessageType, req any, out any) error {
	if c == nil || c.rpc == nil {
		return fmt.Errorf("desired state client is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	msg := protocol.NewMessage()
	if req != nil {
		payload, err := json.Marshal(req)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		msg.Param = payload
	}

	type result struct {
		reply *protocol.Message
		err   error
	}
	resultCh := make(chan result, 1)
	go func() {
		reply, err := c.rpc.Call(path, protocol.TypeRPC, method, msg)
		resultCh <- result{reply: reply, err: err}
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case got := <-resultCh:
		if got.err != nil {
			return got.err
		}
		if out == nil || got.reply == nil || len(got.reply.Param) == 0 {
			return nil
		}

		var rpcErr desiredvsoa.RPCResponse
		if err := json.Unmarshal(got.reply.Param, &rpcErr); err == nil && rpcErr.Error != "" && !rpcErr.OK {
			return errors.New(rpcErr.Error)
		}

		if err := json.Unmarshal(got.reply.Param, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
		return nil
	}
}
