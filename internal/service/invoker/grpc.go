package invoker

import (
	"context"
	"fmt"

	executorv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/executor/v1"
	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/pkg/grpc"
	"github.com/gotomicro/ego/core/elog"
)

var _ Invoker = &GRPCInvoker{}

// GRPCInvoker 基于 gRPC 协议的远程任务执行器。
// 通过 ClientsV2 连接执行节点的 ExecutorService，发送执行/预处理请求。
//
// 调用链路：
//
//	GRPCInvoker.Run → ClientsV2.Get(serviceName) → RoutingPicker.Pick → gRPC Execute
//
// context 中携带的路由信息（SpecificNodeID/ExcludedNodeID）
// 会被 RoutingPicker 消费，实现指定节点/排除节点的智能路由。
type GRPCInvoker struct {
	grpcClients *grpc.ClientsV2[executorv1.ExecutorServiceClient] // gRPC客户端池
	logger      *elog.Component
}

// NewGRPCInvoker 创建 GRPCInvoker 实例
func NewGRPCInvoker(
	grpcClients *grpc.ClientsV2[executorv1.ExecutorServiceClient],
) *GRPCInvoker {
	return &GRPCInvoker{
		grpcClients: grpcClients,
		logger:      elog.DefaultLogger.With(elog.FieldComponentName("executor.GRPCInvoker")),
	}
}

func (r *GRPCInvoker) Name() string {
	return "GRPC"
}

// Run 通过 gRPC 远程调用执行节点执行任务。
// 参数通过 exec.GRPCParams() 从执行记录的调度参数中提取，
// 执行节点返回的 ExecutionState 经过 proto → domain 转换后返回。
func (r *GRPCInvoker) Run(ctx context.Context, exec domain.TaskExecution) (domain.ExecutionState, error) {
	client := r.grpcClients.Get(exec.Task.GrpcConfig.ServiceName)
	// 发送执行请求
	resp, err := client.Execute(ctx, &executorv1.ExecuteRequest{
		Eid:      exec.ID,
		TaskId:   exec.Task.ID,
		TaskName: exec.Task.Name,
		Params:   exec.GRPCParams(),
	})
	if err != nil {
		return domain.ExecutionState{}, fmt.Errorf("发送gRPC请求失败: %w", err)
	}
	return domain.ExecutionStateFromProto(resp.GetExecutionState()), nil
}

// Prepare 通过 gRPC 调用执行节点的 Prepare 接口，获取分片任务的预处理参数。
// 执行节点在 Prepare 阶段计算分片所需的参数（如数据范围、总行数等），
// 返回的参数会被合并到 ShardingRule.Params 中用于后续分片计算。
func (r *GRPCInvoker) Prepare(ctx context.Context, exec domain.TaskExecution) (map[string]string, error) {
	client := r.grpcClients.Get(exec.Task.GrpcConfig.ServiceName)
	// 发送执行请求
	resp, err := client.Prepare(ctx, &executorv1.PrepareRequest{
		Eid:      exec.ID,
		TaskId:   exec.Task.ID,
		TaskName: exec.Task.Name,
		Params:   exec.GRPCParams(),
	})
	if err != nil {
		return nil, fmt.Errorf("发送gRPC请求失败: %w", err)
	}
	return resp.GetParams(), nil
}
