package grpc

import (
	"fmt"
	"time"

	"gitee.com/flycash/distributed_task_platform/pkg/grpc/balancer"
	"gitee.com/flycash/distributed_task_platform/pkg/grpc/registry"
	"github.com/ecodeclub/ekit/syncx"
	"github.com/gotomicro/ego/core/elog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// ClientsV2 是带服务发现和自定义负载均衡的 gRPC 客户端池。
// 使用泛型支持不同的 gRPC Service Client 类型（如 ExecutorServiceClient）。
//
// 核心设计：
//   - 懒加载：首次访问某个 serviceName 时才创建 gRPC 连接
//   - 线程安全：通过 syncx.Map（sync.Map 的泛型封装）保证并发安全
//   - 自动服务发现：每个连接内置 Resolver，监听 etcd 的服务注册变更
//   - 自定义负载均衡：使用 RoutingRoundRobin 策略，支持指定节点/排除节点路由
type ClientsV2[T any] struct {
	clientMap syncx.Map[string, T]    // serviceName → gRPC Client 的并发安全映射
	registry  registry.Registry       // 服务注册中心（etcd），用于服务发现
	timeout   time.Duration           // Resolver 初始化超时时间
	creator   func(conn *grpc.ClientConn) T // 工厂函数，从 gRPC 连接创建具体的 Service Client
}

// NewClientsV2 创建 gRPC 客户端池实例
func NewClientsV2[T any](
	registry registry.Registry,
	timeout time.Duration,
	creator func(conn *grpc.ClientConn) T,
) *ClientsV2[T] {
	return &ClientsV2[T]{
		registry: registry,
		timeout:  timeout,
		creator:  creator,
	}
}

// Get 获取指定 serviceName 的 gRPC 客户端。
// 采用双检锁模式：先尝试从缓存加载，缓存未命中时创建新连接。
// 使用 LoadOrStore 保证并发安全，当多个 goroutine 同时创建时，只有一个会被保留。
//
// 连接配置：
//   - 使用自定义 Resolver（从 etcd 获取服务地址列表）
//   - 使用 RoutingRoundRobin 负载均衡策略（支持指定/排除节点路由）
//   - 使用非加密传输（insecure），生产环境应改为 TLS
func (c *ClientsV2[T]) Get(serviceName string) T {
	// 快速路径：从缓存中加载已有客户端
	if client, ok := c.clientMap.Load(serviceName); ok {
		return client
	}

	// 慢速路径：创建新的 gRPC 连接
	// URI scheme 为 "executor:///"，由自定义 ResolverBuilder 处理
	grpcConn, err := grpc.NewClient(
		fmt.Sprintf("executor:///%s", serviceName),
		// 注入自定义解析器，从 etcd 获取服务地址并监听变更
		grpc.WithResolvers(NewResolverBuilder(c.registry, c.timeout)),
		// 指定使用自定义的路由式轮询负载均衡器
		grpc.WithDefaultServiceConfig(fmt.Sprintf(`{"loadBalancingPolicy":%q}`, balancer.RoutingRoundRobinName)),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		panic(err)
	}
	newClient := c.creator(grpcConn)

	// 原子存储：如果其他 goroutine 已经先存入了，actual 是已有值，loaded=true。
	// 此时需要关闭当前 goroutine 创建的多余连接，防止 gRPC 连接泄漏。
	actual, loaded := c.clientMap.LoadOrStore(serviceName, newClient)
	if loaded {
		// 其他 goroutine 已经创建了连接，关闭当前多余的连接
		if closeErr := grpcConn.Close(); closeErr != nil {
			elog.DefaultLogger.Warn("关闭多余的gRPC连接失败",
				elog.String("serviceName", serviceName),
				elog.FieldErr(closeErr))
		}
	}
	return actual
}
