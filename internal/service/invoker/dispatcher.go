package invoker

import (
	"context"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
)

// 编译期断言 Dispatcher 实现了 Invoker 接口
var _ Invoker = &Dispatcher{}

// Dispatcher 是 Invoker 的路由分发器实现。
//
// 它根据任务的执行配置（GrpcConfig / HTTPConfig / 默认 Local）
// 自动选择对应的 Invoker 实现来执行任务。
//
// 路由优先级：
//  1. 如果任务配置了 GrpcConfig → 使用 GRPCInvoker
//  2. 如果任务配置了 HTTPConfig → 使用 HTTPInvoker
//  3. 否则 → 使用 LocalInvoker（本地执行）
//
// 这种设计使得上层调用方无需关心具体的通信协议，
// 只需通过 Dispatcher 统一调用即可。
type Dispatcher struct {
	http  *HTTPInvoker  // HTTP 调用器
	grpc  *GRPCInvoker  // gRPC 调用器
	local *LocalInvoker // 本地调用器
}

// NewDispatcher 创建路由分发器实例，注入三种协议的调用器实现。
func NewDispatcher(http *HTTPInvoker, grpc *GRPCInvoker, local *LocalInvoker) *Dispatcher {
	return &Dispatcher{
		http:  http,
		grpc:  grpc,
		local: local,
	}
}

func (r *Dispatcher) Name() string {
	return "dispatcher"
}

// Run 根据任务的执行配置路由到对应协议的 Invoker 执行。
// 路由策略：GrpcConfig 优先 → HTTPConfig 次之 → 默认 Local。
func (r *Dispatcher) Run(ctx context.Context, execution domain.TaskExecution) (domain.ExecutionState, error) {
	switch {
	case execution.Task.GrpcConfig != nil:
		return r.grpc.Run(ctx, execution)
	case execution.Task.HTTPConfig != nil:
		return r.http.Run(ctx, execution)
	default:
		return r.local.Run(ctx, execution)
	}
}

// Prepare 根据任务的执行配置路由到对应协议的 Invoker 获取业务元数据。
// 路由策略与 Run 方法一致。
func (r *Dispatcher) Prepare(ctx context.Context, execution domain.TaskExecution) (map[string]string, error) {
	switch {
	case execution.Task.GrpcConfig != nil:
		return r.grpc.Prepare(ctx, execution)
	case execution.Task.HTTPConfig != nil:
		return r.http.Prepare(ctx, execution)
	default:
		return r.local.Prepare(ctx, execution)
	}
}
