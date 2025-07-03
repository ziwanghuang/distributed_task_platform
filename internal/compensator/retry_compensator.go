package compensator

import (
	"context"
	"fmt"
	"time"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"gitee.com/flycash/distributed_task_platform/scheduler"
	"github.com/gotomicro/ego/core/elog"
)

// RetryCompensatorConfig 重试补偿器配置
type RetryCompensatorConfig struct {
	BatchSize              int           // 批次大小
	MinDuration            time.Duration // 最小等待时间，防止空转
	MaxRetryCount          int64         // 最大重试次数
	PrepareTimeoutWindowMs int64         // PREPARE状态超时窗口
}

// RetryCompensator 重试补偿器
type RetryCompensator struct {
	taskSvc   task.Service
	execSvc   task.ExecutionService
	scheduler *scheduler.Scheduler
	config    Config
	logger    *elog.Component
}

// Config 重试补偿器配置
type Config struct {
	MaxRetryCount          int64         `yaml:"maxRetryCount"`          // 最大重试次数
	PrepareTimeoutWindowMs int64         `yaml:"prepareTimeoutWindowMs"` // PREPARE状态超时窗口
	BatchSize              int           `yaml:"batchSize"`              // 批量处理大小
	MinDuration            time.Duration `yaml:"minDuration"`            // 最小等待时间，防止空转
}

// NewRetryCompensator 创建重试补偿器
func NewRetryCompensator(
	taskSvc task.Service,
	execSvc task.ExecutionService,
	scheduler *scheduler.Scheduler,
	config Config,
) *RetryCompensator {
	return &RetryCompensator{
		taskSvc:   taskSvc,
		execSvc:   execSvc,
		scheduler: scheduler,
		config:    config,
		logger:    elog.DefaultLogger.With(elog.FieldComponentName("retry.Compensator")),
	}
}

// Start 启动补偿器
func (r *RetryCompensator) Start(ctx context.Context) {
	r.logger.Info("重试补偿器启动")

	for {
		select {
		case <-ctx.Done():
			r.logger.Info("重试补偿器停止")
			return
		default:
			startTime := time.Now()

			err := r.retry(ctx)
			if err != nil {
				r.logger.Error("重试失败", elog.FieldErr(err))
			}

			// 防空转：确保最小等待时间
			elapsed := time.Since(startTime)
			if elapsed < r.config.MinDuration {
				select {
				case <-ctx.Done():
					return
				case <-time.After(r.config.MinDuration - elapsed):
				}
			}
		}
	}
}

// retry 执行一轮补偿
func (r *RetryCompensator) retry(ctx context.Context) error {
	// 查找可重试的执行记录
	executions, err := r.execSvc.FindRetryableExecutions(
		ctx,
		r.config.MaxRetryCount,
		r.config.PrepareTimeoutWindowMs,
		r.config.BatchSize,
	)
	if err != nil {
		return fmt.Errorf("查找可重试任务失败: %w", err)
	}

	if len(executions) == 0 {
		r.logger.Debug("没有找到可重试的任务")
		return nil
	}

	r.logger.Info("找到可重试任务", elog.Int("count", len(executions)))

	// 处理每个可重试的执行
	for i := range executions {
		err = r.retryExecution(ctx, executions[i])
		if err != nil {
			r.logger.Error("重试任务失败",
				elog.Int64("executionId", executions[i].ID),
				elog.String("taskName", executions[i].Task.Name),
				elog.FieldErr(err))
			continue
		}
	}
	return nil
}

// retryExecution 重试单个执行
func (r *RetryCompensator) retryExecution(ctx context.Context, execution domain.TaskExecution) error {
	r.logger.Info("开始重试任务",
		elog.Int64("executionId", execution.ID),
		elog.String("taskName", execution.Task.Name),
		elog.Int64("retryCount", execution.RetryCount))
	tk, err := r.taskSvc.GetByID(ctx, execution.Task.ID)
	if err != nil {
		return err
	}
	execution.Task = tk
	_, err = r.scheduler.RetryTaskExecution(execution)
	return err
}
