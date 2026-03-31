package compensator

import (
	"context"
	"fmt"
	"time"

	executorv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/executor/v1"
	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/errs"
	"gitee.com/flycash/distributed_task_platform/pkg/grpc"

	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"github.com/gotomicro/ego/core/elog"
)

// InterruptConfig 中断补偿器配置
type InterruptConfig struct {
	BatchSize   int           // 批次大小
	MinDuration time.Duration // 最小等待时间，防止空转
}

// InterruptCompensator 中断补偿器。
// 定期扫描超时的执行记录（Running 状态且超过配置的超时时间），
// 向执行节点发送 gRPC Interrupt 请求，要求中止任务执行。
//
// 中断流程：
//  1. 查询超时的执行记录
//  2. 对每条记录，通过 gRPC 客户端向执行节点发送 InterruptRequest
//  3. 执行节点返回 InterruptResponse（含中断后的执行状态）
//  4. 将中断后的状态更新到数据库
//
// 防空转机制：同 RetryCompensator，通过 MinDuration 防止空转。
type InterruptCompensator struct {
	execSvc     task.ExecutionService
	config      InterruptConfig
	logger      *elog.Component
	grpcClients *grpc.ClientsV2[executorv1.ExecutorServiceClient] // gRPC客户端池，直接发送中断请求
}

// NewInterruptCompensator 创建中断补偿器
func NewInterruptCompensator(
	grpcClients *grpc.ClientsV2[executorv1.ExecutorServiceClient],
	execSvc task.ExecutionService,
	config InterruptConfig,
) *InterruptCompensator {
	return &InterruptCompensator{
		grpcClients: grpcClients,
		execSvc:     execSvc,
		config:      config,
		logger:      elog.DefaultLogger.With(elog.FieldComponentName("compensator.interrupt")),
	}
}

// Start 启动补偿器
func (t *InterruptCompensator) Start(ctx context.Context) {
	t.logger.Info("中断补偿器启动")

	for {
		select {
		case <-ctx.Done():
			t.logger.Info("中断补偿器停止")
			return
		default:
			startTime := time.Now()

			err := t.interruptTimeoutTasks(ctx)
			if err != nil {
				t.logger.Error("中断超时任务失败", elog.FieldErr(err))
			}

			// 防空转：确保最小等待时间
			elapsed := time.Since(startTime)
			if elapsed < t.config.MinDuration {
				select {
				case <-ctx.Done():
					return
				case <-time.After(t.config.MinDuration - elapsed):
				}
			}
		}
	}
}

// interruptTimeoutTasks 中断超时任务
//
//nolint:dupl //忽略
func (t *InterruptCompensator) interruptTimeoutTasks(ctx context.Context) error {
	// 查找超时的执行记录
	executions, err := t.execSvc.FindTimeoutExecutions(ctx, t.config.BatchSize)
	if err != nil {
		return fmt.Errorf("查找可中断任务失败: %w", err)
	}

	if len(executions) == 0 {
		t.logger.Info("没有找到可中断的任务")
		return nil
	}

	t.logger.Info("找到可中断任务", elog.Int("count", len(executions)))

	// 处理每个超时的执行
	for i := range executions {
		err = t.interruptTaskExecution(ctx, executions[i])
		if err != nil {
			t.logger.Error("中断超时任务失败",
				elog.Int64("executionId", executions[i].ID),
				elog.String("taskName", executions[i].Task.Name),
				elog.FieldErr(err))
			continue
		}
		t.logger.Info("成功中断超时任务",
			elog.Int64("executionId", executions[i].ID),
			elog.String("taskName", executions[i].Task.Name))
	}
	return nil
}

func (t *InterruptCompensator) interruptTaskExecution(ctx context.Context, execution domain.TaskExecution) error {
	if execution.Task.GrpcConfig == nil {
		return fmt.Errorf("未找到GPRC配置，无法执行中断任务")
	}
	client := t.grpcClients.Get(execution.Task.GrpcConfig.ServiceName)
	resp, err := client.Interrupt(ctx, &executorv1.InterruptRequest{
		Eid: execution.ID,
	})
	if err != nil {
		return fmt.Errorf("发送中断请求失败：%w", err)
	}
	if !resp.GetSuccess() {
		// 中断失败，忽略状态
		return errs.ErrInterruptTaskExecutionFailed
	}
	return t.execSvc.UpdateState(ctx, domain.ExecutionStateFromProto(resp.GetExecutionState()))
}
