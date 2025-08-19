package grpc

import (
	"context"
	"gitee.com/flycash/distributed_task_platform/pkg/grpc/registry"
	"github.com/gotomicro/ego/core/elog"
	"google.golang.org/grpc"
	"net"
)

type Server struct {
	*grpc.Server
	name   string
	reg    registry.Registry
	logger *elog.Component
	addr   string
}

func NewServer(
	name string,
	addr string,
	reg registry.Registry,
) *Server {
	grpcServer := grpc.NewServer()
	return &Server{
		Server: grpcServer,
		name:   name,
		reg:    reg,
		addr:   addr,
		logger: elog.DefaultLogger,
	}
}

func (s *Server) Run() error {
	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	defer listener.Close()

	err = s.reg.Register(context.Background(), registry.ServiceInstance{
		Name:    s.name,
		Address: listener.Addr().String(),
		ID:      "node01",
	})
	if err != nil {
		return err
	}
	s.logger.Info("grpc server started.......")
	return s.Server.Serve(listener)
}
