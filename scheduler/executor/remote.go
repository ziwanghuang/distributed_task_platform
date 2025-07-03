package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	executorv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/executor/v1"
	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/pkg/grpc"
	"github.com/gotomicro/ego/core/elog"
)

var _ Executor = &RemoteExecutor{}

// RemoteExecutor 远程执行器
type RemoteExecutor struct {
	grpcClients *grpc.Clients[executorv1.ExecutorServiceClient] // gRPC客户端池
	logger      *elog.Component
}

// NewRemoteExecutor 创建 RemoteExecutor 实例
func NewRemoteExecutor(
	grpcClients *grpc.Clients[executorv1.ExecutorServiceClient],
) *RemoteExecutor {
	return &RemoteExecutor{
		grpcClients: grpcClients,
		// pollInterval: pollInterval,
		logger: elog.DefaultLogger.With(elog.FieldComponentName("executor.RemoteExecutor")),
	}
}

func (r *RemoteExecutor) Name() string {
	return "REMOTE"
}

func (r *RemoteExecutor) Run(ctx context.Context, execution domain.TaskExecution) (domain.ExecutionState, error) {
	// 根据配置发送执行请求
	switch {
	case execution.Task.GrpcConfig != nil:
		return r.sendGRPCRequest(ctx, &execution)
	case execution.Task.HTTPConfig != nil:
		return r.sendHTTPRequest(ctx, &execution)
	default:
		return domain.ExecutionState{}, fmt.Errorf("未找到有效配置，无法发送请求")
	}
}

// sendGRPCRequest 发送gRPC执行请求
func (r *RemoteExecutor) sendGRPCRequest(ctx context.Context, exec *domain.TaskExecution) (domain.ExecutionState, error) {
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

// sendHTTPRequest 发送HTTP执行请求
func (r *RemoteExecutor) sendHTTPRequest(_ context.Context, exec *domain.TaskExecution) (domain.ExecutionState, error) {
	// TODO: 实现HTTP客户端调用，当前为占位实现
	r.logger.Warn("HTTP执行方式尚未完全实现，使用占位逻辑",
		elog.Int64("taskId", exec.Task.ID),
		elog.String("endpoint", exec.Task.HTTPConfig.Endpoint))

	// 创建HTTP客户端，设置合理的超时时间
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// 构造请求参数 - 使用实际的任务参数而非硬编码数据
	requestData := map[string]any{
		"taskId":      exec.Task.ID,
		"taskName":    exec.Task.Name,
		"executionId": exec.ID,
		"params":      exec.Task.HTTPConfig.Params,
	}
	// 将参数转换为JSON
	jsonBytes, err := json.Marshal(requestData)
	if err != nil {
		return domain.ExecutionState{}, fmt.Errorf("序列化请求参数失败: %w", err)
	}
	// 发送POST请求到执行节点
	resp, err := client.Post(
		exec.Task.HTTPConfig.Endpoint,
		"application/json",
		strings.NewReader(string(jsonBytes)),
	)
	if err != nil {
		return domain.ExecutionState{}, fmt.Errorf("发送HTTP请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return domain.ExecutionState{}, fmt.Errorf("读取HTTP响应失败: %w", err)
	}

	r.logger.Info("收到HTTP执行节点响应",
		elog.String("response", string(body)),
		elog.Int("statusCode", resp.StatusCode))

	// TODO: 解析响应，根据实际API格式处理执行状态
	// 暂时返回需要监控，由轮询机制处理后续状态更新
	return domain.ExecutionState{}, nil
}
