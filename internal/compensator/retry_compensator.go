package compensator

import (
	"context"
	"fmt"
	"time"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"gitee.com/flycash/distributed_task_platform/pkg/retry"
	"gitee.com/flycash/distributed_task_platform/scheduler/executor"
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
	svc       task.ExecutionService
	executors map[string]executor.Executor
	config    *Config
	logger    *elog.Component
}

// Config 重试补偿器配置
type Config struct {
	MaxRetryCount          int64         // 最大重试次数
	PrepareTimeoutMs       int64         // PREPARE状态超时时间（毫秒）
	PrepareTimeoutWindowMs int64         // PREPARE状态超时窗口
	BatchSize              int           // 批量处理大小
	MinDuration            time.Duration // 最小等待时间，防止空转
	CompensateInterval     time.Duration // 补偿间隔
}

// NewRetryCompensator 创建重试补偿器
func NewRetryCompensator(
	svc task.ExecutionService,
	executors map[string]executor.Executor,
	config *Config,
) *RetryCompensator {
	return &RetryCompensator{
		svc:       svc,
		executors: executors,
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
	executions, err := r.svc.FindRetryableExecutions(
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

	// 使用retry包计算下次重试时间
	if execution.Task.RetryConfig == nil {
		return fmt.Errorf("任务重试配置为空")
	}
	retryStrategy, err := retry.NewRetry(execution.Task.RetryConfig.ToRetryComponentConfig())
	if err != nil {
		return fmt.Errorf("创建重试策略失败: %w", err)
	}

	success := r.executeTask(execution)

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
	return r.svc.UpdateRetryResult(ctx, execution.ID, newRetryCount, nextRetryTime, endTime, status)
}

// executeTask 执行具体任务（暂时用注释代替）
// TODO: 这里需要一个具体的执行组件来执行任务
func (r *RetryCompensator) executeTask(execution domain.TaskExecution) bool {
	r.logger.Info("开始执行重试任务",
		elog.Int64("executionId", execution.ID),
		elog.String("taskName", execution.Task.Name))

	executor, ok := r.executors[execution.Task.Name]
	if !ok {
		return false

	}

	// 使用任务管理器执行任务
	state, err := executor.Run(context.Background(), execution)
	if err != nil {
		r.logger.Error("启动任务执行失败",
			elog.Int64("executionId", execution.ID),
			elog.String("taskName", execution.Task.Name),
			elog.FieldErr(err))
		return false
	}

	// 等待任务完成
	select {
	case err := <-errorCh:
		if err != nil {
			r.logger.Error("任务执行失败",
				elog.Int64("executionId", execution.ID),
				elog.String("taskName", execution.Task.Name),
				elog.FieldErr(err))
			return false
		} else {
			r.logger.Info("任务执行成功",
				elog.Int64("executionId", execution.ID),
				elog.String("taskName", execution.Task.Name))
			return true
		}
	case <-time.After(30 * time.Minute): // 30分钟超时
		r.logger.Error("任务执行超时",
			elog.Int64("executionId", execution.ID),
			elog.String("taskName", execution.Task.Name))
		return false
	}
}
