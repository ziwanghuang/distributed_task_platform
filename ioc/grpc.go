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

// InitSchedulerNodeGRPCServer 初始化调度节点的 gRPC 服务端。
// 流程：
//  1. 基于 etcd 客户端创建 ego 注册中心，并注册为全局 resolver
//  2. 从 config.yaml 的 server.scheduler.grpc 段加载 gRPC 服务配置（端口 9002）
//  3. 注册 ReporterService：执行节点通过此服务上报执行状态
func InitSchedulerNodeGRPCServer(rs *grpcapi.ReporterServer, etcdClient *eetcd.Component) *egrpc.Component {
	// 注册全局的 etcd 注册中心，供 gRPC resolver 使用
	reg := registry.Load("").Build(registry.WithClientEtcd(etcdClient))
	resolver.Register("etcd", reg)
	server := egrpc.Load("server.scheduler.grpc").Build()
	reporterv1.RegisterReporterServiceServer(server.Server, rs)
	return server
}

// InitExecutorServiceGRPCClients 初始化执行节点的 gRPC 客户端连接池。
// 基于自定义的 etcd 注册中心（非 ego 内置）实现服务发现，
// 支持自定义负载均衡策略（routing_balancer），
// 可根据执行节点的 CPU/内存指标选择最优节点。
func InitExecutorServiceGRPCClients(registry *balancerRegistry.Registry) *grpcpkg.ClientsV2[executorv1.ExecutorServiceClient] {
	const defaultTimeout = time.Second
	return grpcpkg.NewClientsV2(
		registry,
		defaultTimeout,
		func(conn *grpc.ClientConn) executorv1.ExecutorServiceClient {
			return executorv1.NewExecutorServiceClient(conn)
		})
}
