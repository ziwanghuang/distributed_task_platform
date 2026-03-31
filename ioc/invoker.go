package ioc

import (
	executorv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/executor/v1"
	"gitee.com/flycash/distributed_task_platform/internal/service/invoker"
	"gitee.com/flycash/distributed_task_platform/pkg/grpc"
)

// InitInvoker 初始化任务调用器分发器。
// Dispatcher 根据任务的 ExecutionMethod 字段路由到不同的调用器：
//   - REMOTE + gRPC: 通过 gRPC 客户端调用远程执行节点
//   - REMOTE + HTTP: 通过 HTTP 请求调用远程执行端点
//   - LOCAL: 在调度节点本地直接执行（用于测试和轻量任务）
//
// LocalInvoker 的函数映射表在此处为空，可在业务需要时注册本地执行函数。
func InitInvoker(clients *grpc.ClientsV2[executorv1.ExecutorServiceClient]) invoker.Invoker {
	return invoker.NewDispatcher(
		invoker.NewHTTPInvoker(),
		invoker.NewGRPCInvoker(clients),
		invoker.NewLocalInvoker(map[string]invoker.LocalExecuteFunc{}, map[string]invoker.LocalPrepareFunc{}))
}
