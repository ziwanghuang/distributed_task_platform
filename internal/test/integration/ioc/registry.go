package ioc

import (
	registry "gitee.com/flycash/distributed_task_platform/pkg/grpc/registry/etcd"
	"github.com/ego-component/eetcd"
)

func InitRegistry(etcdClient *eetcd.Component) *registry.Registry {
	r, err := registry.NewRegistry(etcdClient)
	if err != nil {
		panic(err)
	}
	return r
}
