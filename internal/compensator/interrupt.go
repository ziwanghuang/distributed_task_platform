package compensator

import (
	"context"
	"fmt"
	"time"

	"gitee.com/flycash/distributed_task_platform/internal/service/scheduler"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"github.com/gotomicro/ego/core/elog"
)

// InterruptConfig 中断补偿器配置
type InterruptConfig struct {
	BatchSize   int           // 批次大小
	MinDuration time.Duration // 最小等待时间，防止空转
}

// InterruptCompensator 中断补偿器
type InterruptCompensator struct {
	scheduler *scheduler.Scheduler
	execSvc   task.ExecutionService
	config    InterruptConfig
	logger    *elog.Component
}

// NewInterruptCompensator 创建中断补偿器
func NewInterruptCompensator(
	scheduler *scheduler.Scheduler,
	execSvc task.ExecutionService,
	config InterruptConfig,
) *InterruptCompensator {
	return &InterruptCompensator{
		scheduler: scheduler,
		execSvc:   execSvc,
		config:    config,
		logger:    elog.DefaultLogger.With(elog.FieldComponentName("compensator.interrupt")),
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
		err = t.scheduler.InterruptTaskExecution(ctx, executions[i])
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
