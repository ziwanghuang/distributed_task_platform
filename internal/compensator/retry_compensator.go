package compensator

import (
	"context"
	"fmt"
	"time"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"gitee.com/flycash/distributed_task_platform/pkg/retry"
	"github.com/gotomicro/ego/core/elog"
)

// RetryCompensatorConfig 重试补偿器配置
type RetryCompensatorConfig struct {
	BatchSize        int           // 批次大小
	MinDuration      time.Duration // 最小等待时间，防止空转
	MaxRetryCount    int64         // 最大重试次数
	PrepareTimeoutMs int64         // PREPARE状态超时时间
}

// RetryCompensator 重试补偿器
type RetryCompensator struct {
	config  RetryCompensatorConfig
	execSvc task.ExecutionService
	logger  *elog.Component
}

// NewRetryCompensator 创建重试补偿器
func NewRetryCompensator(
	config RetryCompensatorConfig,
	execSvc task.ExecutionService,
) *RetryCompensator {
	return &RetryCompensator{
		config:  config,
		execSvc: execSvc,
		logger:  elog.DefaultLogger.With(elog.FieldComponentName("compensator.RetryCompensator")),
	}
}

// Start 启动补偿器
func (r *RetryCompensator) Start(ctx context.Context) error {
	r.logger.Info("重试补偿器启动")

	for {
		select {
		case <-ctx.Done():
			r.logger.Info("重试补偿器停止")
			return ctx.Err()
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
					return ctx.Err()
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
		r.config.PrepareTimeoutMs,
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
	for _, execution := range executions {
		err = r.retryExecution(ctx, execution)
		if err != nil {
			r.logger.Error("重试任务失败",
				elog.Int64("executionId", execution.ID),
				elog.String("taskName", execution.Task.Name),
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

	// 使用retry包计算下次重试时间
	if execution.Task.RetryConfig == nil {
		return fmt.Errorf("任务重试配置为空")
	}
	retryStrategy, err := retry.NewRetry(execution.Task.RetryConfig.ToRetryConfig())
	if err != nil {
		return fmt.Errorf("创建重试策略失败: %w", err)
	}

	// TODO: 实际重试逻辑 - 这里需要一个具体执行Execution的组件
	// 比如: success := r.executionEngine.Execute(ctx, execution)
	success := r.executeTask(execution) // 用注释代替具体实现

	var status domain.TaskExecutionStatus
	var nextRetryTime, endTime int64
	newRetryCount := execution.RetryCount + 1

	if success {
		// 成功了不需要下次重试时间
		status = domain.TaskExecutionStatusSuccess
		nextRetryTime = 0
		endTime = time.Now().UnixMilli()
	} else {
		status = execution.Status // 保持原状态！
		duration, shouldRetry := retryStrategy.NextWithRetries(int32(execution.RetryCount + 1))
		if shouldRetry {
			nextRetryTime = time.Now().Add(duration).UnixMilli()
			endTime = execution.EndTime
		} else {
			// 超过最大重试次数
			status = domain.TaskExecutionStatusFailed
			nextRetryTime = 0
			endTime = time.Now().UnixMilli()
		}
	}
	return r.execSvc.UpdateRetryResult(ctx, execution.ID, newRetryCount, nextRetryTime, endTime, status)
}

// executeTask 执行具体任务（暂时用注释代替）
// TODO: 这里需要一个具体的执行组件来执行任务
func (r *RetryCompensator) executeTask(execution domain.TaskExecution) bool {
	// TODO: 根据execution的类型（LOCAL/REMOTE）调用不同的执行器
	// 比如:
	// if execution.Task.ExecutorType == domain.TaskExecutorTypeLocal {
	//     return r.localExecutor.Execute(execution)
	// } else {
	//     return r.remoteExecutor.Execute(execution)
	// }

	// 暂时返回false表示需要重试
	r.logger.Info("TODO: 实际执行任务逻辑",
		elog.Int64("executionId", execution.ID),
		elog.String("taskName", execution.Task.Name))
	return false // 暂时假设执行失败，需要重试
}
