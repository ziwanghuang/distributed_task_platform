package ioc

import (
	executorv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/executor/v1"
	reporterv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/reporter/v1"
	grpcapi "gitee.com/flycash/distributed_task_platform/internal/service/scheduler/grpc"
	"gitee.com/flycash/distributed_task_platform/pkg/grpc"
	"github.com/ego-component/eetcd"
	"github.com/ego-component/eetcd/registry"
	egrpc2 "github.com/gotomicro/ego/client/egrpc"
	"github.com/gotomicro/ego/client/egrpc/resolver"
	"github.com/gotomicro/ego/server/egrpc"
)

func InitSchedulerNodeGRPCServer(rs *grpcapi.ReporterServer, etcdClient *eetcd.Component) *egrpc.Component {
	// 注册全局的注册中心
	reg := registry.Load("").Build(registry.WithClientEtcd(etcdClient))
	resolver.Register("etcd", reg)
	server := egrpc.Load("server.scheduler.grpc").Build()
	reporterv1.RegisterReporterServiceServer(server.Server, rs)
	return server
}

func InitExecutorServiceGRPCClients() *grpc.Clients[executorv1.ExecutorServiceClient] {
	return grpc.NewClients(func(conn *egrpc2.Component) executorv1.ExecutorServiceClient {
		return executorv1.NewExecutorServiceClient(conn)
	})
}
