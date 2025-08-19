package main

import (
	executorv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/executor/v1"
	"gitee.com/flycash/distributed_task_platform/api/proto/gen/reporter/v1"
	"gitee.com/flycash/distributed_task_platform/example/grpc"
	"gitee.com/flycash/distributed_task_platform/example/ioc"
	"github.com/gotomicro/ego/client/egrpc"
)

const serverName = "longRunning"

func main() {
	etcdClient := ioc.InitEtcdClient()
	reg := ioc.InitRegistry(etcdClient)
	grpcServer := grpc.NewServer(serverName, "127.0.0.1:8880", reg)
	// 使用ego的注册中心
	// ego 如果服务发现失败，会 panic
	conn := egrpc.Load("").Build(egrpc.WithAddr("127.0.0.1:9002"))
	client := reporterv1.NewReporterServiceClient(conn)
	s := grpc.NewExecutor(client,10000)
	executorv1.RegisterExecutorServiceServer(grpcServer, s)
	// 启动
	if err := grpcServer.Run(); err != nil {
		panic(err)
	}
}
