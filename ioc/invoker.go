package ioc

import (
	executorv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/executor/v1"
	"gitee.com/flycash/distributed_task_platform/internal/service/invoker"
	"gitee.com/flycash/distributed_task_platform/pkg/grpc"
)

func InitInvoker(clients *grpc.Clients[executorv1.ExecutorServiceClient]) invoker.Invoker {
	return invoker.NewDispatcher(
		invoker.NewHTTPInvoker(),
		invoker.NewGRPCInvoker(clients),
		invoker.NewLocalInvoker(map[string]invoker.LocalExecuteFunc{}))
}
