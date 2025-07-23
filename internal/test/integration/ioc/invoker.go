package ioc

import (
	executorv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/executor/v1"
	"gitee.com/flycash/distributed_task_platform/internal/service/invoker"
	"gitee.com/flycash/distributed_task_platform/pkg/grpc"
)

func InitInvoker(clients *grpc.ClientsV2[executorv1.ExecutorServiceClient], localInvoker *invoker.LocalInvoker) invoker.Invoker {
	return invoker.NewDispatcher(
		invoker.NewHTTPInvoker(),
		invoker.NewGRPCInvoker(clients),
		localInvoker)
}

func NewExecutors(executeFuncs map[string]invoker.LocalExecuteFunc, prepareFuncs map[string]invoker.LocalPrepareFunc) *invoker.LocalInvoker {
	return invoker.NewLocalInvoker(executeFuncs, prepareFuncs)
}
