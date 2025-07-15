package balancer

import (
	"google.golang.org/grpc/balancer"
)

// excludeBalancerBuilder 实现 BalancerBuilder 接口
type excludeBalancerBuilder struct{}

// Build 创建新的负载均衡器实例
func (b *excludeBalancerBuilder) Build(cc balancer.ClientConn, _ balancer.BuildOptions) balancer.Balancer {
	return newExcludeBalancer(cc)
}

// Name 返回负载均衡器的名称
func (b *excludeBalancerBuilder) Name() string {
	return ExcludeRoundRobinName
}

// init 函数在包初始化时注册负载均衡器
func init() {
	balancer.Register(&excludeBalancerBuilder{})
}
