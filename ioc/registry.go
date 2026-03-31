package ioc

import (
	registry "gitee.com/flycash/distributed_task_platform/pkg/grpc/registry/etcd"
	"github.com/ego-component/eetcd"
)

// InitRegistry 初始化自定义的 etcd 服务注册中心。
// 与 ego 内置注册中心不同，这是平台自己实现的注册中心，
// 支持更丰富的元数据（节点权重、IP 地址等），
// 配合自定义负载均衡器实现智能调度。
// 用于执行节点的服务发现和注册。
func InitRegistry(etcdClient *eetcd.Component) *registry.Registry {
	r, err := registry.NewRegistry(etcdClient)
	if err != nil {
		panic(err)
	}
	return r
}
