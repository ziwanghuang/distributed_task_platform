package ioc

import (
	"time"

	executorv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/executor/v1"
	reporterv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/reporter/v1"
	grpcapi "gitee.com/flycash/distributed_task_platform/internal/grpc"
	grpcpkg "gitee.com/flycash/distributed_task_platform/pkg/grpc"

	balancerRegistry "gitee.com/flycash/distributed_task_platform/pkg/grpc/registry/etcd"

	// 导入自定义负载均衡器包，确保其初始化函数被调用
	_ "gitee.com/flycash/distributed_task_platform/pkg/grpc/balancer"
	"github.com/ego-component/eetcd"
	"github.com/ego-component/eetcd/registry"
	"github.com/gotomicro/ego/client/egrpc/resolver"
	"github.com/gotomicro/ego/server/egrpc"
	"google.golang.org/grpc"
)

func InitSchedulerNodeGRPCServer(rs *grpcapi.ReporterServer, etcdClient *eetcd.Component) *egrpc.Component {
	// 注册全局的注册中心
	reg := registry.Load("").Build(registry.WithClientEtcd(etcdClient))
	resolver.Register("etcd", reg)
	server := egrpc.Load("server.scheduler.grpc").Build()
	reporterv1.RegisterReporterServiceServer(server.Server, rs)
	return server
}

func InitExecutorServiceGRPCClients(registry *balancerRegistry.Registry) *grpcpkg.ClientsV2[executorv1.ExecutorServiceClient] {
	const defaultTimeout = time.Second
	return grpcpkg.NewClientsV2(
		registry,
		defaultTimeout,
		func(conn *grpc.ClientConn) executorv1.ExecutorServiceClient {
			return executorv1.NewExecutorServiceClient(conn)
		})
}
