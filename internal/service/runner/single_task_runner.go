package runner

import (
	"context"
	"fmt"
	"time"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/pkg/retry"
	"gitee.com/flycash/distributed_task_platform/pkg/retry/strategy"
	"github.com/gotomicro/ego/core/elog"
)

type SingleTaskRunner struct {
	*Base
}

func NewSingleTaskRunner(base *Base) *SingleTaskRunner {
	return &SingleTaskRunner{Base: base}
}

func (s *SingleTaskRunner) Run(ctx context.Context, task domain.Task) error {
	// 抢令牌和抢占任务
	acquiredTask, err := s.acquireTokenAndTask(ctx, task)
	if err != nil {
		s.logger.Error("任务抢占失败",
			elog.Int64("taskID", task.ID),
			elog.String("taskName", task.Name),
			elog.FieldErr(err))
		return err
	}

	// 抢占成功，立即创建TaskExecution记录
	execution, err := s.execSvc.Create(ctx, domain.TaskExecution{
		Task: *acquiredTask,
		// 可以认为开始执行了，防止执行节点直接返回"终态"状态Failed，Success等
		StartTime: time.Now().UnixMilli(),
		Status:    domain.TaskExecutionStatusPrepare,
	})
	if err != nil {
		s.logger.Error("创建任务执行记录失败",
			elog.Int64("taskID", task.ID),
			elog.String("taskName", task.Name),
			elog.FieldErr(err))
		// 创建失败，释放令牌和锁
		s.releaseTokenAndTask(ctx, *acquiredTask)
		return err
	}

	// 抢占和创建都成功，启动异步任务管理
	go s.handleTaskExecution(ctx, execution, s.handleSchedulingTaskExecutionFunc)
	return nil
}

func (s *SingleTaskRunner) handleSchedulingTaskExecutionFunc(ctx context.Context, res *Result, execution *domain.TaskExecution) (stop bool) {
	switch {
	case execution.Status.IsPrepare() && res.State.Status.IsRunning():
		return s.setRunningState(ctx, res, execution)
	case execution.Status.IsRunning() && res.State.Status.IsRunning():
		return s.updateRunningProgress(ctx, res, execution)
	case (execution.Status.IsPrepare() || execution.Status.IsRunning()) && res.State.Status.IsTerminalStatus():
		return s.updateStatusAndProgressAndEndTime(ctx, res, execution)
	default:
		s.logger.Info("正常调度不支持的状态转换",
			elog.Int64("taskID", execution.Task.ID),
			elog.String("taskName", execution.Task.Name),
			elog.String("currentStatus", execution.Status.String()),
			elog.String("finalStatus", res.State.Status.String()))
		return true
	}
}

func (s *SingleTaskRunner) updateStatusAndProgressAndEndTime(ctx context.Context, res *Result, execution *domain.TaskExecution) (stop bool) {
	err := s.execSvc.UpdateStatusAndProgressAndEndTime(ctx, res.State.ID, res.State.Status, res.State.RunningProgress, time.Now().UnixMilli())
	if err != nil {
		s.logger.Error("更新执行计划状态、进度和结束时间失败",
			elog.Int64("taskID", execution.Task.ID),
			elog.String("taskName", execution.Task.Name),
			elog.Any("state", res.State),
			elog.FieldErr(err))
		return true
	}
	s.logger.Info("任务执行完成",
		elog.Int64("taskID", execution.Task.ID),
		elog.String("taskName", execution.Task.Name),
		elog.String("status", res.State.Status.String()))

	s.updateTaskState(ctx, res, execution)
	return true
}

func (s *SingleTaskRunner) Retry(ctx context.Context, execution domain.TaskExecution) error {
	// 是否有重试配置
	if execution.Task.RetryConfig == nil {
		execution.RetryCount++
		_ = s.execSvc.UpdateRetryResult(ctx, execution.ID, execution.RetryCount, execution.NextRetryTime, time.Now().UnixMilli(), domain.TaskExecutionStatusFailed)
		return fmt.Errorf("任务重试配置为空")
	}

	// 是否能够正常初始化
	retryStrategy, err := retry.NewRetry(execution.Task.RetryConfig.ToRetryComponentConfig())
	if err != nil {
		execution.RetryCount++
		_ = s.execSvc.UpdateRetryResult(ctx, execution.ID, execution.RetryCount, execution.NextRetryTime, time.Now().UnixMilli(), domain.TaskExecutionStatusFailed)
		return fmt.Errorf("创建重试策略失败: %w", err)
	}

	// 抢令牌和抢占任务
	acquiredTask, err := s.acquireTokenAndTask(ctx, execution.Task)
	if err != nil {
		s.logger.Error("重试：任务抢占失败",
			elog.Int64("taskID", execution.Task.ID),
			elog.String("taskName", execution.Task.Name),
			elog.FieldErr(err))
		return err
	}
	execution.Task = *acquiredTask

	// 开始重试调度
	go s.handleTaskExecution(ctx, execution, func(ctx context.Context, result *Result, execution *domain.TaskExecution) (stop bool) {
		return s.handleRetryingTaskExecutionFunc(ctx, result, execution, retryStrategy)
	})
	return nil
}

func (s *SingleTaskRunner) handleRetryingTaskExecutionFunc(ctx context.Context, res *Result, execution *domain.TaskExecution, retryStrategy strategy.Strategy) (stop bool) {
	switch {
	case execution.Status.IsFailedRetryable() && res.State.Status.IsRunning():
		return s.setRunningState(ctx, res, execution)
	case execution.Status.IsRunning() && res.State.Status.IsRunning():
		return s.updateRunningProgress(ctx, res, execution)
	case (execution.Status.IsFailedRetryable() || execution.Status.IsRunning()) && res.State.Status.IsTerminalStatus():
		return s.updateRetryResult(ctx, res, execution, retryStrategy)
	default:
		s.logger.Info("重试补偿不支持的状态转换",
			elog.Int64("taskID", execution.Task.ID),
			elog.String("taskName", execution.Task.Name),
			elog.String("currentStatus", execution.Status.String()),
			elog.String("finalStatus", res.State.Status.String()))
		return true
	}
}

func (s *SingleTaskRunner) updateRetryResult(ctx context.Context, res *Result, execution *domain.TaskExecution, retryStrategy strategy.Strategy) (stop bool) {
	// FAILED_RETRYABLE 或者 RUNNING  → 终态：重试任务直接完成（成功或最终失败）
	s.logger.Info("重试任务完成",
		elog.Int64("executionID", res.State.ID),
		elog.String("currentStatus", execution.Status.String()),
		elog.String("finalStatus", res.State.Status.String()))

	// 设置结束时间
	execution.EndTime = time.Now().UnixMilli()
	// 增加重试计数
	execution.RetryCount++
	// 计算出下次重试时间
	duration, shouldRetry := retryStrategy.NextWithRetries(int32(execution.RetryCount))
	if shouldRetry {
		execution.NextRetryTime = time.Now().Add(duration).UnixMilli()
	}
	// 不管是否达到最大重试次数，都要更新状态（主要是重试次数），这样下次重试补偿任务会因其超过最大重试次数而不再重试
	err := s.execSvc.UpdateRetryResult(ctx, execution.ID, execution.RetryCount, execution.NextRetryTime, execution.EndTime, res.State.Status)
	if err != nil {
		s.logger.Error("更新执行计划重试结果失败",
			elog.Int64("taskID", execution.Task.ID),
			elog.String("taskName", execution.Task.Name),
			elog.Any("state", res.State),
			elog.FieldErr(err))
	}
	// 不管是否达到最大重试次数
	s.updateTaskState(ctx, res, execution)
	return true
}
