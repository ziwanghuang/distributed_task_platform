package ioc

import (
	"time"

	executorv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/executor/v1"
	grpcpkg "gitee.com/flycash/distributed_task_platform/pkg/grpc"

	balancerRegistry "gitee.com/flycash/distributed_task_platform/pkg/grpc/registry/etcd"

	// 导入自定义负载均衡器包，确保其初始化函数被调用
	_ "gitee.com/flycash/distributed_task_platform/pkg/grpc/balancer"
	"google.golang.org/grpc"
)

func InitExecutorServiceGRPCClients(registry *balancerRegistry.Registry) *grpcpkg.ClientsV2[executorv1.ExecutorServiceClient] {
	const defaultTimeout = time.Second
	return grpcpkg.NewClientsV2(
		registry,
		defaultTimeout,
		func(conn *grpc.ClientConn) executorv1.ExecutorServiceClient {
			return executorv1.NewExecutorServiceClient(conn)
		})
}
