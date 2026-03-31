// Package grpc 提供 gRPC 客户端连接管理能力。
//
// 本包的核心是泛型 Clients 结构体，它为不同的 gRPC 服务提供懒加载、线程安全的客户端缓存池。
// 设计目标：
//   - 按 serviceName 懒加载：首次调用 Get 时才通过 ego 框架的 etcd 服务发现创建连接
//   - 并发安全：使用 syncx.Map（泛型并发 Map）+ LoadOrStore 原子语义，保证同一 serviceName 只创建一个客户端实例
//   - 泛型支持：T 可以是任意 gRPC 生成的 Client 接口（如 SchedulerClient、ExecutorClient 等）
//
// 使用示例：
//
//	clients := grpc.NewClients(func(conn *egrpc.Component) pb.ExecutorClient {
//	    return pb.NewExecutorClient(conn.ClientConn)
//	})
//	client := clients.Get("executor-service") // 首次调用创建连接，后续复用
package grpc

import (
	"fmt"

	"github.com/ecodeclub/ekit/syncx"
	"github.com/gotomicro/ego/client/egrpc"
)

// Clients 是一个泛型 gRPC 客户端连接池。
// 它通过 serviceName 作为 key，懒加载并缓存 gRPC 客户端实例。
//
// 类型参数：
//   - T: gRPC 客户端接口类型（如 pb.ExecutorClient）
//
// 核心字段：
//   - clientMap: 线程安全的泛型 Map，key 为服务名，value 为 gRPC 客户端实例
//   - creator:   客户端工厂函数，接收 ego 的 gRPC 连接组件，返回具体的客户端实例
type Clients[T any] struct {
	clientMap syncx.Map[string, T]
	creator   func(conn *egrpc.Component) T
}

// NewClients 创建一个新的泛型 gRPC 客户端连接池。
// creator 是客户端工厂函数，在首次请求某个 serviceName 时被调用。
func NewClients[T any](creator func(conn *egrpc.Component) T) *Clients[T] {
	return &Clients[T]{creator: creator}
}

// Get 根据 serviceName 获取对应的 gRPC 客户端实例。
//
// 工作流程：
//  1. 先从缓存中查找，命中则直接返回
//  2. 未命中时，通过 ego 框架的 egrpc.Load 创建 gRPC 连接（使用 etcd:/// 前缀触发服务发现）
//  3. 调用 creator 工厂函数将连接转换为具体的客户端实例
//  4. 使用 LoadOrStore 原子存储，确保并发场景下同一 serviceName 只有一个客户端实例
//
// 注意：ego 框架在服务发现失败时会 panic，调用方需确保 etcd 和目标服务可用。
func (c *Clients[T]) Get(serviceName string) T {
	// 尝试加载，如果存在，直接返回
	if client, ok := c.clientMap.Load(serviceName); ok {
		return client
	}
	// 不存在，准备创建
	// 我要初始化 client
	// ego 如果服务发现失败，会 panic
	grpcConn := egrpc.Load("").Build(egrpc.WithAddr(fmt.Sprintf("etcd:///%s", serviceName)))
	newClient := c.creator(grpcConn)
	// 使用 LoadOrStore 原子地存储
	// 如果在当前 goroutine 创建期间，有其他 goroutine 已经存入了值，
	// actual 会是那个已经存在的值，ok 会是 true。
	// 这样可以保证我们总是使用第一个被成功创建和存储的 client。
	actual, _ := c.clientMap.LoadOrStore(serviceName, newClient)
	return actual
}
