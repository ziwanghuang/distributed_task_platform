package ioc

import (
	"gitee.com/flycash/distributed_task_platform/pkg/grpc/registry/etcd"
	"github.com/ego-component/eetcd"
)

func InitRegistry(etcdClient *eetcd.Component) *etcd.Registry {
	reg,err := etcd.NewRegistry(etcdClient)
	if err != nil {
		panic(err)
	}
	return reg
}
