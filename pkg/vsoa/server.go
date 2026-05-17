package vsoa

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"

	vsoaProtocol "github.com/acoinfo/vsoa/protocol"
	vsoaServer "github.com/acoinfo/vsoa/server"
)

// Server 把 desired state 存储能力通过 VSOA RPC 暴露出去。
type Server struct {
	addr     string
	password string
	service  *DesiredStateService

	mu     sync.Mutex
	server *vsoaServer.Server
	errCh  chan error
	ctx    context.Context
}

// NewServer 创建 desired state VSOA Server。
func NewServer(addr, password string, service *DesiredStateService) (*Server, error) {
	if service == nil {
		return nil, fmt.Errorf("desired state service is nil")
	}
	return &Server{
		addr:     strings.TrimSpace(addr),
		password: strings.TrimSpace(password),
		service:  service,
		errCh:    make(chan error, 1),
	}, nil
}

// Start 后台启动 VSOA 服务。
func (s *Server) Start() error {
	if s.addr == "" {
		return fmt.Errorf("vsoa server address is empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server != nil {
		return nil
	}

	srv := vsoaServer.NewServer("ecsm desired state vsoa server", vsoaServer.Option{
		Password: s.password,
	})
	if err := s.registerRoutes(srv); err != nil {
		return err
	}

	s.server = srv
	go func() {
		log.Printf("[INFO] desired state VSOA 服务启动: addr=%s", s.addr)
		if err := srv.Serve(s.addr); err != nil && err != vsoaServer.ErrServerClosed {
			select {
			case s.errCh <- err:
			default:
			}
		}
	}()
	return nil
}

// Run 启动 VSOA 服务并阻塞，直到 context 取消或服务返回错误。
func (s *Server) Run(ctx context.Context) error {
	s.setContext(ctx)
	defer s.setContext(nil)

	if err := s.Start(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return s.Close()
	case err := <-s.errCh:
		return err
	}
}

// Close 关闭 VSOA 服务。
func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server == nil {
		return nil
	}
	err := s.server.Close()
	s.server = nil
	return err
}

func (s *Server) setContext(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ctx = ctx
}

func (s *Server) context() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx == nil {
		return context.Background()
	}
	return s.ctx
}

// Errors 返回后台服务错误通道。
func (s *Server) Errors() <-chan error {
	return s.errCh
}

func (s *Server) registerRoutes(srv *vsoaServer.Server) error {
	routes := []struct {
		path    string
		method  vsoaProtocol.RpcMessageType
		handler func(*vsoaProtocol.Message, *vsoaProtocol.Message)
	}{
		{path: RouteHealthz, method: vsoaProtocol.RpcMethodGet, handler: s.handleHealthz},
		{path: RouteUpdateDesired, method: vsoaProtocol.RpcMethodSet, handler: s.handleUpdateDesired},
		{path: RouteDeleteDesired, method: vsoaProtocol.RpcMethodSet, handler: s.handleDeleteDesired},
		{path: RouteQueryDesired, method: vsoaProtocol.RpcMethodGet, handler: s.handleQueryDesired},
		{path: RouteListDesired, method: vsoaProtocol.RpcMethodGet, handler: s.handleListDesired},
	}

	for _, route := range routes {
		if err := srv.On(route.path, route.method, route.handler); err != nil {
			return fmt.Errorf("register route %s: %w", route.path, err)
		}
	}
	return nil
}

func (s *Server) handleHealthz(req, resp *vsoaProtocol.Message) {
	writeResponse(resp, s.service.HandleHealthzPayload(s.context(), requestPayload(req)))
}

func (s *Server) handleUpdateDesired(req, resp *vsoaProtocol.Message) {
	writeResponse(resp, s.service.HandleUpdateDesiredPayload(s.context(), requestPayload(req)))
}

func (s *Server) handleDeleteDesired(req, resp *vsoaProtocol.Message) {
	writeResponse(resp, s.service.HandleDeleteDesiredPayload(s.context(), requestPayload(req)))
}

func (s *Server) handleQueryDesired(req, resp *vsoaProtocol.Message) {
	writeResponse(resp, s.service.HandleQueryDesiredPayload(s.context(), requestPayload(req)))
}

func (s *Server) handleListDesired(req, resp *vsoaProtocol.Message) {
	writeResponse(resp, s.service.HandleListDesiredPayload(s.context(), requestPayload(req)))
}

func requestPayload(req *vsoaProtocol.Message) []byte {
	if req == nil {
		return nil
	}
	if len(req.Param) > 0 {
		return req.Param
	}
	return req.Data
}

func writeResponse(resp *vsoaProtocol.Message, value any) {
	payload, err := json.Marshal(value)
	if err != nil {
		payload, _ = json.Marshal(RPCResponse{OK: false, Error: err.Error()})
	}
	resp.Param = payload
	resp.Data = nil
}
