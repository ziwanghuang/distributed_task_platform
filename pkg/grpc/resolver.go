package grpc

import (
	"time"

	"gitee.com/flycash/distributed_task_platform/pkg/grpc/registry"
	"golang.org/x/net/context"
	"google.golang.org/grpc/attributes"
	"google.golang.org/grpc/resolver"
)

const (
	initCapacityStr = "initCapacity"
	maxCapacityStr  = "maxCapacity"
	increaseStepStr = "increaseStep"
	growthRateStr   = "growthRate"
	nodeIDStr       = "nodeID"
)

// resolverBuilder 创建自定义的 gRPC 解析器。
// 将 etcd 服务注册中心与 gRPC 的服务发现机制桥接起来。
// URI scheme 为 "executor"，示例：executor:///my-service
type resolverBuilder struct {
	r       registry.Registry // etcd 注册中心
	timeout time.Duration     // 初始解析超时时间
}

func NewResolverBuilder(r registry.Registry, timeout time.Duration) resolver.Builder {
	return &resolverBuilder{
		r:       r,
		timeout: timeout,
	}
}

// Build 创建解析器实例并立即进行首次解析。
// 同时启动后台 watch 协程，监听 etcd 的服务变更事件实现动态服务发现。
// 当有新的执行节点上线/下线时，会自动更新 gRPC 连接池中的地址列表。
func (r *resolverBuilder) Build(target resolver.Target, cc resolver.ClientConn, _ resolver.BuildOptions) (resolver.Resolver, error) {
	res := &executorResolver{
		target:   target,
		cc:       cc,
		registry: r.r,
		close:    make(chan struct{}, 1),
		timeout:  r.timeout,
	}
	res.resolve()
	go res.watch()
	return res, nil
}

func (r *resolverBuilder) Scheme() string {
	return "executor"
}

// executorResolver 自定义 gRPC 解析器，从 etcd 获取执行节点地址列表。
// 实现两种解析模式：
//   - 主动解析（resolve）：直接查询 etcd 获取当前所有服务实例
//   - 被动监听（watch）：订阅 etcd 的变更事件，实时更新地址列表
//
// 每个 ServiceInstance 的元数据（权重、容量等）通过 resolver.Address.Attributes 传递，
// 供下游的负载均衡器使用。
type executorResolver struct {
	target   resolver.Target      // 解析目标，Endpoint() 返回 serviceName
	cc       resolver.ClientConn  // gRPC 客户端连接，用于报告地址更新
	registry registry.Registry    // etcd 注册中心
	close    chan struct{}         // 关闭信号通道
	timeout  time.Duration        // 查询 etcd 的超时时间
}

func (g *executorResolver) ResolveNow(_ resolver.ResolveNowOptions) {
	// 重新获取一下所有服务
	g.resolve()
}

func (g *executorResolver) Close() {
	g.close <- struct{}{}
}

// watch 后台监听 etcd 的服务变更事件。
// 当有新的执行节点注册或已有节点下线时，etcd 会通过 channel 通知，
// 触发 resolve() 重新拉取完整的地址列表并更新 gRPC 连接。
func (g *executorResolver) watch() {
	events := g.registry.Subscribe(g.target.Endpoint())
	for {
		select {
		case <-events:
			g.resolve()

		case <-g.close:
			return
		}
	}
}

// resolve 从 etcd 拉取指定服务的所有实例，转换为 gRPC 地址列表。
// 每个实例的元数据（初始容量、最大容量、增长步长、增长率、节点ID）
// 通过 resolver.Address.Attributes 传递给负载均衡器，
// 负载均衡器据此实现自定义的路由选择策略。
func (g *executorResolver) resolve() {
	serviceName := g.target.Endpoint()
	ctx, cancel := context.WithTimeout(context.Background(), g.timeout)
	instances, err := g.registry.ListServices(ctx, serviceName)
	cancel()
	if err != nil {
		g.cc.ReportError(err)
	}

	address := make([]resolver.Address, 0, len(instances))
	for _, ins := range instances {
		address = append(address, resolver.Address{
			Addr:       ins.Address,
			ServerName: ins.Name,
			Attributes: attributes.New(initCapacityStr, ins.InitCapacity).
				WithValue(maxCapacityStr, ins.MaxCapacity).
				WithValue(increaseStepStr, ins.IncreaseStep).
				WithValue(growthRateStr, ins.GrowthRate).
				WithValue(nodeIDStr, ins.ID),
		})
	}
	err = g.cc.UpdateState(resolver.State{
		Addresses: address,
	})
	if err != nil {
		g.cc.ReportError(err)
	}
}
