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

// RescheduleCompensatorV2 重调度补偿器
type RescheduleCompensatorV2 struct {
	runner  runner.Runner
	execSvc task.ExecutionService
	config  RescheduleConfig
	logger  *elog.Component

	dlockClient  dlock.Client
	sem          loopjob.ResourceSemaphore
	executionStr sharding.ShardingStrategy
}

// NewRescheduleCompensatorV2 创建分库分表版本的重调度补偿器
func NewRescheduleCompensatorV2(
	runner runner.Runner,
	execSvc task.ExecutionService,
	config RescheduleConfig,
	dlockClient dlock.Client,
	sem loopjob.ResourceSemaphore,
	executionStr sharding.ShardingStrategy,
) *RescheduleCompensatorV2 {
	return &RescheduleCompensatorV2{
		runner:       runner,
		execSvc:      execSvc,
		config:       config,
		logger:       elog.DefaultLogger.With(elog.FieldComponentName("compensator.reschedule")),
		dlockClient:  dlockClient,
		sem:          sem,
		executionStr: executionStr,
	}
}

// Start 启动补偿器
func (r *RescheduleCompensatorV2) Start(ctx context.Context) {
	const rescheduleKey = "rescheduleKey"
	loopjob.NewShardingLoopJob(r.dlockClient, rescheduleKey, r.reschedule, r.executionStr, r.sem).Run(ctx)
}

// reschedule 执行一轮补偿
func (r *RescheduleCompensatorV2) reschedule(ctx context.Context) error {
	// 查找可重调度的执行记录
	executions, err := r.execSvc.FindReschedulableExecutions(
		ctx,
		r.config.BatchSize,
	)
	if err != nil {
		return fmt.Errorf("查找可重调度任务失败: %w", err)
	}

	if len(executions) == 0 {
		r.logger.Info("没有找到可重调度的任务")
		return nil
	}

	r.logger.Info("找到可重调度任务", elog.Int("count", len(executions)))

	// 处理每个可重调度的执行
	for i := range executions {
		err = r.runner.Reschedule(ctx, executions[i])
		if err != nil {
			r.logger.Error("重调度失败",
				elog.Int64("executionId", executions[i].ID),
				elog.String("taskName", executions[i].Task.Name),
				elog.FieldErr(err))
			continue
		}
	}
	return nil
}
