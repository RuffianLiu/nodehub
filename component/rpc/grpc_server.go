package rpc

import (
	"context"
	"errors"
	"fmt"
	"net"
	"nodehub"

	"github.com/samber/lo"
	"google.golang.org/grpc"
)

// GRPCServer grpc服务
type GRPCServer struct {
	endpoint string
	server   *grpc.Server
	services []grpcService
}

type grpcService struct {
	Code   int32
	Desc   *grpc.ServiceDesc
	Public bool
}

// NewGRPCServer 构造函数
func NewGRPCServer(endpoint string, opts ...grpc.ServerOption) *GRPCServer {
	return &GRPCServer{
		endpoint: endpoint,
		server:   grpc.NewServer(opts...),
	}
}

// RegisterService 注册服务
func (gs *GRPCServer) RegisterService(code int32, desc *grpc.ServiceDesc, impl any, opts ...func(*grpcService)) error {
	if code == 0 {
		return errors.New("code must not be 0")
	}

	for _, s := range gs.services {
		if s.Code == code {
			return fmt.Errorf("code %d already registered", code)
		}
	}

	s := grpcService{
		Code: code,
		Desc: desc,
	}
	for _, opt := range opts {
		opt(&s)
	}
	gs.services = append(gs.services, s)

	gs.server.RegisterService(desc, impl)
	return nil
}

// Name 服务名称
func (gs *GRPCServer) Name() string {
	return "grpc"
}

// Start 启动服务
func (gs *GRPCServer) Start(ctx context.Context) error {
	l, err := net.Listen("tcp", gs.endpoint)
	if err != nil {
		return fmt.Errorf("listen tcp, %w", err)
	}

	return gs.server.Serve(l)
}

// Stop 停止服务
func (gs *GRPCServer) Stop(ctx context.Context) error {
	gs.server.Stop()
	return nil
}

// ToEntry 转换为服务发现条目
func (gs *GRPCServer) ToEntry() nodehub.GRPCEntry {
	return nodehub.GRPCEntry{
		Endpoint: gs.endpoint,
		Services: lo.Map(gs.services, func(s grpcService, _ int) nodehub.GRPCServiceDesc {
			return nodehub.GRPCServiceDesc{
				Code:   s.Code,
				Path:   fmt.Sprintf("/%s", s.Desc.ServiceName),
				Public: s.Public,
			}
		}),
	}
}

// WithPublic 设置是否允许客户端访问
func WithPublic() func(*grpcService) {
	return func(g *grpcService) {
		g.Public = true
	}
}
