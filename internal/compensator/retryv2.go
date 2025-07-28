package compensator

import (
	"context"
	"fmt"

	"gitee.com/flycash/distributed_task_platform/internal/service/runner"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"gitee.com/flycash/distributed_task_platform/pkg/loopjob"
	"gitee.com/flycash/distributed_task_platform/pkg/sharding"
	"github.com/gotomicro/ego/core/elog"
	"github.com/meoying/dlock-go"
)

// RetryCompensatorV2 重试补偿器
type RetryCompensatorV2 struct {
	runner  runner.Runner
	execSvc task.ExecutionService
	config  RetryConfig
	logger  *elog.Component

	dlockClient  dlock.Client
	sem          loopjob.ResourceSemaphore
	executionStr sharding.ShardingStrategy
}

// NewRetryCompensator 创建重试补偿器
func NewRetryCompensatorV2(
	runner runner.Runner,
	execSvc task.ExecutionService,
	config RetryConfig,
	dlockClient dlock.Client,
	sem loopjob.ResourceSemaphore,
	executionStr sharding.ShardingStrategy,
) *RetryCompensatorV2 {
	return &RetryCompensatorV2{
		runner:       runner,
		execSvc:      execSvc,
		config:       config,
		logger:       elog.DefaultLogger.With(elog.FieldComponentName("compensator.retry")),
		dlockClient:  dlockClient,
		sem:          sem,
		executionStr: executionStr,
	}
}

// Start 启动补偿器
func (r *RetryCompensatorV2) Start(ctx context.Context) {
	const rescheduleKey = "retryKey"
	loopjob.NewShardingLoopJob(r.dlockClient, rescheduleKey, r.retry, r.executionStr, r.sem).Run(ctx)
}

// retry 执行一轮补偿
func (r *RetryCompensatorV2) retry(ctx context.Context) error {
	// 查找可重试的执行记录
	executions, err := r.execSvc.FindRetryableExecutions(
		ctx,
		r.config.BatchSize,
	)
	if err != nil {
		return fmt.Errorf("查找可重试任务失败: %w", err)
	}

	if len(executions) == 0 {
		r.logger.Info("没有找到可重试的任务")
		return nil
	}

	r.logger.Info("找到可重试任务", elog.Int("count", len(executions)))

	// 处理每个可重试的执行
	for i := range executions {
		err = r.runner.Retry(ctx, executions[i])
		if err != nil {
			r.logger.Error("重试任务失败",
				elog.Int64("executionId", executions[i].ID),
				elog.String("taskName", executions[i].Task.Name),
				elog.Int64("retryCount", executions[i].RetryCount),
				elog.FieldErr(err))
			continue
		}
	}
	return nil
}
