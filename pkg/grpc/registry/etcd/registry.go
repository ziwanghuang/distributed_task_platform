// Package etcd 提供基于 etcd 的服务注册中心实现。
//
// 本包实现了 registry.Registry 接口，利用 etcd 的以下特性：
//   - KV 存储：以 /services/task_platform/executor/{name}/{address} 为 key 存储服务实例信息
//   - 租约（Lease）：通过 concurrency.Session 自动维护租约续期，节点宕机时 key 自动删除
//   - Watch 机制：实时监听服务变更事件，支持 executor 节点上下线感知
//
// Key 格式：/services/task_platform/executor/{serviceName}/{address}
// Value 格式：ServiceInstance 的 JSON 序列化
//
// 在调度平台中的角色：
//   - executor 启动时调用 Register 注册自己
//   - scheduler 通过 Subscribe 监听 executor 变更，驱动自定义负载均衡器更新节点池
//   - executor 正常退出时调用 UnRegister 主动注销，异常退出时依赖租约过期自动摘除
package etcd

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"gitee.com/flycash/distributed_task_platform/pkg/grpc/registry"
	"github.com/ego-component/eetcd"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"
)

const (
	// defaultPrefix 是服务注册的 etcd key 前缀，格式为 /services/task_platform/executor
	defaultPrefix = "/services/task_platform/executor"
)

// typesMap 将 etcd 的事件类型映射为 registry 包定义的事件类型。
// PUT 事件对应服务上线（新注册或租约续期），DELETE 事件对应服务下线（主动注销或租约过期）。
var typesMap = map[mvccpb.Event_EventType]registry.EventType{
	mvccpb.PUT:    registry.EventTypeAdd,
	mvccpb.DELETE: registry.EventTypeDelete,
}

// Registry 是基于 etcd 的服务注册中心实现。
//
// 核心字段：
//   - sess:        etcd 并发会话，内部维护一个带 TTL 的租约，自动续期。
//     服务注册时使用该租约，节点异常退出时租约过期，对应的 key 自动删除。
//   - client:      eetcd 客户端组件（ego 框架封装），提供 etcd v3 API 操作
//   - watchCancel: 保存所有 Subscribe 创建的 Watch 取消函数，Close 时统一取消
//   - mutex:       保护 watchCancel 切片的并发安全
type Registry struct {
	sess   *concurrency.Session
	client *eetcd.Component

	mutex       sync.RWMutex
	watchCancel []func()
}

// NewRegistry 创建一个基于 etcd 的注册中心实例。
// 内部会创建一个 concurrency.Session，自动维护租约续期。
// 如果 Session 创建失败（如 etcd 不可达），返回错误。
func NewRegistry(c *eetcd.Component) (*Registry, error) {
	sess, err := concurrency.NewSession(c.Client)
	if err != nil {
		return nil, err
	}
	return &Registry{
		sess:   sess,
		client: c,
	}, nil
}

// Register 将服务实例注册到 etcd。
// 将 ServiceInstance 序列化为 JSON 存储，并绑定到 Session 的租约上。
// 租约过期或 Session 关闭时，该 key 会自动删除，实现故障自动摘除。
func (r *Registry) Register(ctx context.Context, si registry.ServiceInstance) error {
	val, err := json.Marshal(si)
	if err != nil {
		return err
	}
	_, err = r.client.Put(ctx, r.instanceKey(si),
		string(val), clientv3.WithLease(r.sess.Lease()))
	return err
}

// instanceKey 生成服务实例在 etcd 中的完整 key。
// 格式：/services/task_platform/executor/{name}/{address}
func (r *Registry) instanceKey(s registry.ServiceInstance) string {
	return fmt.Sprintf("%s/%s/%s", defaultPrefix, s.Name, s.Address)
}

// UnRegister 从 etcd 中注销服务实例。
// 直接删除对应的 key，无需等待租约过期。
func (r *Registry) UnRegister(ctx context.Context, si registry.ServiceInstance) error {
	_, err := r.client.Delete(ctx, r.instanceKey(si))
	return err
}

// ListServices 按服务名查询所有在线的服务实例。
// 使用前缀查询（WithPrefix）获取该服务名下的所有实例 key，逐个反序列化返回。
// 主要用于初始化时一次性拉取全量服务列表。
func (r *Registry) ListServices(ctx context.Context, name string) ([]registry.ServiceInstance, error) {
	resp, err := r.client.Get(ctx, r.serviceKey(name), clientv3.WithPrefix())
	if err != nil {
		return nil, err
	}
	res := make([]registry.ServiceInstance, 0, len(resp.Kvs))
	for _, kv := range resp.Kvs {
		var si registry.ServiceInstance
		err = json.Unmarshal(kv.Value, &si)
		if err != nil {
			return nil, err
		}
		res = append(res, si)
	}
	return res, nil
}

// serviceKey 生成服务名的 etcd key 前缀。
// 格式：/services/task_platform/executor/{name}
func (r *Registry) serviceKey(name string) string {
	return fmt.Sprintf("%s/%s", defaultPrefix, name)
}

// Subscribe 订阅指定服务名的变更事件（Watch 模式）。
//
// 工作流程：
//  1. 创建一个可取消的 context（Close 时统一取消）
//  2. 设置 WithRequireLeader 确保只在 etcd leader 节点上 Watch（避免脑裂场景的脏数据）
//  3. 启动后台 goroutine 持续监听 Watch channel
//  4. 将 etcd 的 PUT/DELETE 事件转换为 registry.Event 推送到返回的 channel
//
// 调用方通过读取返回的 channel 获取实时的服务上下线事件。
func (r *Registry) Subscribe(name string) <-chan registry.Event {
	ctx, cancel := context.WithCancel(context.Background())
	ctx = clientv3.WithRequireLeader(ctx)
	r.mutex.Lock()
	r.watchCancel = append(r.watchCancel, cancel)
	r.mutex.Unlock()
	ch := r.client.Watch(ctx, r.serviceKey(name), clientv3.WithPrefix())
	res := make(chan registry.Event)
	go func() {
		for {
			select {
			case resp := <-ch:
				if resp.Canceled {
					return
				}
				if resp.Err() != nil {
					continue
				}
				for _, event := range resp.Events {
					res <- registry.Event{
						Type: typesMap[event.Type],
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return res
}

// Close 关闭注册中心，释放所有资源。
// 先取消所有 Watch goroutine，再关闭 etcd Session（释放租约）。
// 注意：不关闭 etcd client 本身，因为 client 是外部传入的，可能被其他模块共享。
func (r *Registry) Close() error {
	r.mutex.Lock()
	for _, cancel := range r.watchCancel {
		cancel()
	}
	r.mutex.Unlock()
	// 因为 client 是外面传进来的，所以我们这里不能关掉它。它可能被其它的人使用着
	return r.sess.Close()
}
