package ioc

import (
	"github.com/ego-component/eetcd"
)

// InitEtcdClient 初始化 etcd 客户端连接。
// 从 config.yaml 的 etcd 段读取配置（地址列表、连接超时等）。
// etcd 在系统中承担两个核心职责：
//   - 服务发现：调度节点和执行节点通过 etcd 注册/发现彼此
//   - 分布式协调：配合 ego 框架的服务注册中心，实现 gRPC 客户端负载均衡
func InitEtcdClient() *eetcd.Component {
	return eetcd.Load("etcd").Build()
}
