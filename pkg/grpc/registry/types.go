// Package registry 定义了服务注册与发现的核心抽象接口。
//
// 本包是 gRPC 服务发现体系的基础层，定义了 Registry 接口和 ServiceInstance 模型，
// 具体实现位于子包（如 etcd/registry.go）。
//
// 在调度平台中，executor 节点通过 Registry.Register 注册自己，
// scheduler 节点通过 Registry.Subscribe 监听 executor 上下线事件，
// 实现动态的服务发现和负载均衡。
//
// 核心概念：
//   - ServiceInstance: 一个服务实例的元数据，包含地址、权重、容量等负载均衡相关参数
//   - Event: 服务变更事件（上线/下线），通过 Subscribe 返回的 channel 推送
//   - Registry: 服务注册中心的统一接口，支持注册、注销、列举和订阅
package registry

import (
	"context"
	"io"
)

// Registry 是服务注册中心的统一接口。
// 所有注册中心实现（etcd、Consul 等）都应满足此接口。
//
// 方法说明：
//   - Register:     将服务实例注册到注册中心，通常附带 TTL 租约实现自动摘除
//   - UnRegister:   主动注销服务实例
//   - ListServices: 按服务名查询所有在线实例（用于初始化时一次性拉取）
//   - Subscribe:    按服务名订阅变更事件（Watch 模式，实时推送上下线事件）
//   - Close:        释放注册中心连接资源
type Registry interface {
	Register(ctx context.Context, si ServiceInstance) error
	UnRegister(ctx context.Context, si ServiceInstance) error

	ListServices(ctx context.Context, name string) ([]ServiceInstance, error)
	Subscribe(name string) <-chan Event

	io.Closer
}

// ServiceInstance 描述一个服务实例的元数据。
//
// 字段说明：
//   - Name:         服务名称，用于区分不同类型的服务（如 "executor"、"scheduler"）
//   - Address:      实例地址，格式为 ip:port（如 "1.1.0.129:9001"）
//   - ID:           实例唯一标识符
//   - Weight:       静态权重，用于加权负载均衡
//   - InitCapacity: 初始调度容量（该节点启动时能处理的并发任务数）
//   - MaxCapacity:  最大调度容量上限
//   - IncreaseStep: 容量增长步长（每次扩容增加的任务数）
//   - GrowthRate:   容量增长率（用于动态调整调度权重）
//
// 容量相关字段配合自定义负载均衡器（balancer 包）实现智能调度：
// 调度器会根据节点的当前负载和容量参数，动态决定任务分配策略。
type ServiceInstance struct {
	Name         string  // 服务名称
	Address      string  // 实例地址 ip:port
	ID           string  // 实例唯一标识
	Weight       int64   // 静态权重
	InitCapacity int64   // 初始调度容量
	MaxCapacity  int64   // 最大调度容量
	IncreaseStep int64   // 容量增长步长
	GrowthRate   float64 // 容量增长率
}

// EventType 表示服务变更事件的类型。
type EventType int

const (
	// EventTypeUnknown 未知事件类型（零值，用于初始化）
	EventTypeUnknown EventType = iota
	// EventTypeAdd 服务实例上线事件（对应 etcd 的 PUT 操作）
	EventTypeAdd
	// EventTypeDelete 服务实例下线事件（对应 etcd 的 DELETE 操作或租约过期）
	EventTypeDelete
)

// IsAdd 判断是否为服务上线事件
func (e EventType) IsAdd() bool {
	return e == EventTypeAdd
}

// IsDelete 判断是否为服务下线事件
func (e EventType) IsDelete() bool {
	return e == EventTypeDelete
}

// Event 表示一次服务变更事件，由 Registry.Subscribe 返回的 channel 推送。
//
// 字段说明：
//   - Type:     事件类型（Add 或 Delete）
//   - Instance: 发生变更的服务实例信息
type Event struct {
	Type     EventType
	Instance ServiceInstance
}
