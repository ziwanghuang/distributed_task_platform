package compensator

import (
	"context"
	"fmt"

	executorv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/executor/v1"
	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/errs"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"gitee.com/flycash/distributed_task_platform/pkg/grpc"
	"gitee.com/flycash/distributed_task_platform/pkg/loopjob"
	"gitee.com/flycash/distributed_task_platform/pkg/sharding"
	"github.com/gotomicro/ego/core/elog"
	"github.com/meoying/dlock-go"
)

// InterruptCompensatorV2 中断补偿器
type InterruptCompensatorV2 struct {
	execSvc      task.ExecutionService
	config       InterruptConfig
	logger       *elog.Component
	grpcClients  *grpc.ClientsV2[executorv1.ExecutorServiceClient] // gRPC客户端池
	dlockClient  dlock.Client
	sem          loopjob.ResourceSemaphore
	executionStr sharding.ShardingStrategy
}

// NewInterruptCompensator 创建中断补偿器
func NewInterruptCompensatorV2(
	grpcClients *grpc.ClientsV2[executorv1.ExecutorServiceClient],
	execSvc task.ExecutionService,
	config InterruptConfig,
	dlockClient dlock.Client,
	sem loopjob.ResourceSemaphore,
	executionStr sharding.ShardingStrategy,
) *InterruptCompensatorV2 {
	return &InterruptCompensatorV2{
		grpcClients:  grpcClients,
		execSvc:      execSvc,
		config:       config,
		logger:       elog.DefaultLogger.With(elog.FieldComponentName("compensator.interrupt")),
		dlockClient:  dlockClient,
		sem:          sem,
		executionStr: executionStr,
	}
}

// Start 启动补偿器
func (t *InterruptCompensatorV2) Start(ctx context.Context) {
	const interruptKey = "interruptKey"
	loopjob.NewShardingLoopJob(t.dlockClient, interruptKey, t.interruptTimeoutTasks, t.executionStr, t.sem).Run(ctx)
}

// interruptTimeoutTasks 中断超时任务
//
//nolint:dupl //忽略
func (t *InterruptCompensatorV2) interruptTimeoutTasks(ctx context.Context) error {
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

func (t *InterruptCompensatorV2) interruptTaskExecution(ctx context.Context, execution domain.TaskExecution) error {
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
