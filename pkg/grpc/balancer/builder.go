// Package balancer 提供自定义的 gRPC 负载均衡器。
//
// 本包实现了一个基于路由信息的轮询负载均衡器（RoutingRoundRobin），
// 与标准的 round_robin 不同，它支持根据上下文中携带的路由信息（如目标节点地址）
// 精确路由到指定的 executor 节点。
//
// 注册机制：
//   - 通过 init() 函数在包初始化时自动注册到 gRPC 的全局 balancer 注册表
//   - 客户端创建连接时指定 balancer name 即可使用
//
// 在调度平台中的角色：
//   - scheduler 向 executor 发起 gRPC 调用时，需要将任务路由到特定节点
//   - 本负载均衡器从 gRPC 的 metadata 或 context 中提取目标节点信息，实现精确路由
package balancer

import (
	"google.golang.org/grpc/balancer"
)

// routingBalancerBuilder 实现 gRPC 的 balancer.Builder 接口，
// 用于创建 RoutingRoundRobin 负载均衡器实例。
type routingBalancerBuilder struct{}

// Build 创建一个新的路由负载均衡器实例。
// cc 是 gRPC 提供的 ClientConn 抽象，负载均衡器通过它管理子连接和地址更新。
func (b *routingBalancerBuilder) Build(cc balancer.ClientConn, _ balancer.BuildOptions) balancer.Balancer {
	return newRoutingBalancer(cc)
}

// Name 返回负载均衡器的注册名称。
// gRPC 客户端通过该名称选择使用此负载均衡器。
func (b *routingBalancerBuilder) Name() string {
	return RoutingRoundRobinName
}

// init 在包初始化时将路由负载均衡器注册到 gRPC 的全局 balancer 注册表。
// 注册后，gRPC 客户端可以通过 RoutingRoundRobinName 引用此负载均衡器。
func init() {
	balancer.Register(&routingBalancerBuilder{})
}
