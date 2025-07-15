package grpc

import (
	"fmt"

	"github.com/ecodeclub/ekit/syncx"
	"github.com/gotomicro/ego/client/egrpc"
)

type Clients[T any] struct {
	clientMap syncx.Map[string, T]
	creator   func(conn *egrpc.Component) T
}

func NewClients[T any](creator func(conn *egrpc.Component) T) *Clients[T] {
	return &Clients[T]{creator: creator}
}

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
