package runner

import (
	"context"
	"fmt"
	"time"

	"gitee.com/flycash/distributed_task_platform/internal/event"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/errs"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"gitee.com/flycash/distributed_task_platform/pkg/acquirer"
	"gitee.com/flycash/distributed_task_platform/pkg/executor"
	"gitee.com/flycash/distributed_task_platform/pkg/retry"
	"gitee.com/flycash/distributed_task_platform/pkg/retry/strategy"
	"github.com/ecodeclub/ekit/syncx"
	"github.com/gotomicro/ego/core/elog"
	"go.uber.org/multierr"
)

var _ Runner = &SingleTaskRunner{}

type SingleTaskRunner struct {
	nodeID                 string                                       // 当前调度节点ID
	executionStateHandlers *syncx.Map[int64, TaskExecutionStateHandler] // 正在执行任务的结果处理器
	taskSvc                task.Service                                 // 任务服务
	execSvc                task.ExecutionService                        // 任务执行服务
	taskAcquirer           acquirer.TaskAcquirer                        // 任务抢占器
	executors              map[string]executor.Executor                 // 执行器

	renewInterval time.Duration // 续约间隔

	logger *elog.Component
}

func NewSingleTaskRunner(
	nodeID string,
	executionHandlers *syncx.Map[int64, TaskExecutionStateHandler],
	taskSvc task.Service,
	execSvc task.ExecutionService,
	taskAcquirer acquirer.TaskAcquirer,
	executors map[string]executor.Executor,
	renewInterval time.Duration,
) *SingleTaskRunner {
	return &SingleTaskRunner{nodeID: nodeID, executionStateHandlers: executionHandlers, taskSvc: taskSvc, execSvc: execSvc, taskAcquirer: taskAcquirer, executors: executors, renewInterval: renewInterval}
}

func (s *SingleTaskRunner) Run(ctx context.Context, task domain.Task) error {
	// 抢占任务
	acquiredTask, err := s.acquireTask(ctx, task)
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
		// 释放任务
		s.releaseTask(ctx, *acquiredTask)
		return err
	}

	// 抢占和创建都成功，异步触发任务
	go func() {
		// ResultHandleFunc
		err1 := s.handleTaskExecution(ctx, execution, s.scheduleExecutionStateHandler)
		if err1 != nil {
			s.logger.Error("正常调度任务失败",
				elog.Any("execution", execution),
				elog.FieldErr(err1))
		}
	}()
	return nil
}

// acquireTask 抢占任务
func (s *SingleTaskRunner) acquireTask(ctx context.Context, task domain.Task) (*domain.Task, error) {
	// 抢占任务
	acquiredTask, err := s.taskAcquirer.Acquire(ctx, task.ID, s.nodeID)
	if err != nil {
		return nil, fmt.Errorf("任务抢占失败: %w", err)
	}
	// 抢占成功，，返回最新的 Task 对象指针
	return acquiredTask, nil
}

// releaseTask 释放任务
func (s *SingleTaskRunner) releaseTask(ctx context.Context, task domain.Task) {
	if err := s.taskAcquirer.Release(ctx, task.ID, s.nodeID); err != nil {
		s.logger.Error("释放任务失败",
			elog.Int64("taskID", task.ID),
			elog.String("taskName", task.Name),
			elog.FieldErr(err))
	}
}

func (s *SingleTaskRunner) handleTaskExecution(ctx context.Context, execution domain.TaskExecution, handleResultFunc TaskExecutionStateHandler) (err error) {
	s.logger.Info("开始处理任务",
		elog.Int64("taskID", execution.Task.ID),
		elog.String("taskName", execution.Task.Name))

	// 注册结果处理器
	s.executionStateHandlers.Store(execution.ID, handleResultFunc)

	// 选中执行器
	selectedExecutor, ok := s.executors[execution.Task.ExecutionMethod.String()]
	if !ok {
		err := fmt.Errorf("%w: %s", errs.ErrInvalidTaskExecutionMethod, execution.Task.ExecutionMethod)
		s.logger.Error("未找到任务执行器",
			elog.Int64("taskID", execution.Task.ID),
			elog.String("taskName", execution.Task.Name),
			elog.String("executionMethod", execution.Task.ExecutionMethod.String()),
			elog.FieldErr(err))
		return err
	}

	// 执行任务
	result, err := selectedExecutor.Run(ctx, execution)
	if err != nil {
		s.logger.Error("执行器执行任务失败", elog.FieldErr(err))
		return err
	}

	// 处理结果
	err = handleResultFunc(ctx, result)
	if err != nil {
		s.logger.Error("处理任务执行结果失败", elog.FieldErr(err))
		return err
	}
	return nil
}

// scheduleExecutionStateHandler 调度流程的状态机处理器
func (s *SingleTaskRunner) scheduleExecutionStateHandler(ctx context.Context, state domain.ExecutionState) error {
	execution, err := s.execSvc.GetByID(ctx, state.ID)
	if err != nil {
		return errs.ErrExecutionNotFound
	}
	switch {
	case execution.Status.IsPrepare() && state.Status.IsRunning():
		return s.setRunningState(ctx, state)
	case execution.Status.IsRunning() && state.Status.IsRunning():
		return s.updateRunningProgress(ctx, state)
	case (execution.Status.IsPrepare() || execution.Status.IsRunning()) && state.Status.IsTerminalStatus():
		defer func() {
			// 删除注册的状态处理器
			s.executionStateHandlers.Delete(execution.ID)
			// 释放任务
			s.releaseTask(ctx, execution.Task)
		}()

		// 发送完成事件
		s.sendCompletedEvent(ctx, state, execution)

		var err1 error
		if err2 := s.updateScheduleResult(ctx, execution, state); err2 != nil {
			err1 = multierr.Append(err1, fmt.Errorf("更新任务执行记录的调度结果失败：%w", err2))
		}
		// 不管本次调度是否成功，都要更新task的下一次执行时间
		if err3 := s.updateTaskNextTime(ctx, execution.Task.ID); err3 != nil {
			err1 = multierr.Append(err1, fmt.Errorf("更新任务下次更新时间失败：%w", err3))
		}
		return err1
	default:
		s.logger.Info("正常调度不支持的状态转换",
			elog.Int64("taskID", execution.Task.ID),
			elog.String("taskName", execution.Task.Name),
			elog.String("currentStatus", execution.Status.String()),
			elog.String("finalStatus", state.Status.String()))
		return errs.ErrInvalidTaskExecutionStatus
	}
}

func (s *SingleTaskRunner) setRunningState(ctx context.Context, result domain.ExecutionState) error {
	// PREPARE → RUNNING
	// FAILED_RETRYABLE → RUNNING
	// FAILED_RESCHEDULED → RUNNING
	err := s.execSvc.SetRunningState(ctx, result.ID, result.RunningProgress)
	if err != nil {
		s.logger.Error("更新为运行状态失败",
			elog.Int64("taskID", result.TaskID),
			elog.String("taskName", result.TaskName),
			elog.Any("result", result),
			elog.FieldErr(err))
		return err
	}
	return nil
}

func (s *SingleTaskRunner) updateRunningProgress(ctx context.Context, result domain.ExecutionState) error {
	// RUNNING → RUNNING：更新进度
	err := s.execSvc.UpdateRunningProgress(ctx, result.ID, result.RunningProgress)
	if err != nil {
		s.logger.Error("更新运行进度失败",
			elog.Int64("taskID", result.TaskID),
			elog.String("taskName", result.TaskName),
			elog.Any("result", result),
			elog.FieldErr(err))
		return err
	}
	return nil
}

func (s *SingleTaskRunner) updateScheduleResult(ctx context.Context, execution domain.TaskExecution, result domain.ExecutionState) error {
	if result.RequestReschedule && result.Status.IsFailedRescheduled() {
		execution.MergeTaskScheduleParams(result.RescheduleParams)
	}
	err := s.execSvc.UpdateScheduleResult(ctx,
		result.ID,
		result.Status,
		result.RunningProgress,
		time.Now().UnixMilli(),
		execution.Task.ScheduleParams)
	if err != nil {
		s.logger.Error("更新调度结果失败",
			elog.Int64("taskID", execution.Task.ID),
			elog.String("taskName", execution.Task.Name),
			elog.Any("result", result),
			elog.FieldErr(err))
		return err
	}
	s.logger.Info("任务执行完成",
		elog.Int64("taskID", execution.Task.ID),
		elog.String("taskName", execution.Task.Name),
		elog.String("result", result.Status.String()))
	return nil
}

func (s *SingleTaskRunner) updateTaskNextTime(ctx context.Context, taskID int64) error {
	tk, err := s.taskSvc.GetByID(ctx, taskID)
	if err != nil {
		s.logger.Error("获取任务失败",
			elog.FieldErr(err))
		return err
	}
	// 更新任务执行时间
	_, err = s.taskSvc.UpdateNextTime(ctx, tk)
	if err != nil {
		s.logger.Error("更新任务下次执行时间失败",
			elog.Int64("taskID", tk.ID),
			elog.String("taskName", tk.Name),
			elog.FieldErr(err))
		return err
	}
	return nil
}

func (s *SingleTaskRunner) Retry(ctx context.Context, execution domain.TaskExecution) error {
	// 是否有重试配置
	if execution.Task.RetryConfig == nil {
		execution.RetryCount++
		_ = s.execSvc.UpdateRetryResult(ctx,
			execution.ID,
			execution.RetryCount,
			execution.NextRetryTime,
			domain.TaskExecutionStatusFailed,
			execution.RunningProgress,
			time.Now().UnixMilli(),
			execution.Task.ScheduleParams)
		return fmt.Errorf("任务重试配置为空")
	}

	// 是否能够正常初始化重试器
	retryStrategy, err := retry.NewRetry(execution.Task.RetryConfig.ToRetryComponentConfig())
	if err != nil {
		execution.RetryCount++
		_ = s.execSvc.UpdateRetryResult(ctx,
			execution.ID,
			execution.RetryCount,
			execution.NextRetryTime,
			domain.TaskExecutionStatusFailed,
			execution.RunningProgress,
			time.Now().UnixMilli(),
			execution.Task.ScheduleParams)
		return fmt.Errorf("创建重试策略失败: %w", err)
	}

	// 抢占任务
	acquiredTask, err := s.acquireTask(ctx, execution.Task)
	if err != nil {
		s.logger.Error("重试：任务抢占失败",
			elog.Int64("taskID", execution.Task.ID),
			elog.String("taskName", execution.Task.Name),
			elog.FieldErr(err))
		return err
	}
	execution.Task = *acquiredTask

	// 抢占和创建都成功，异步触发任务
	go func() {
		err1 := s.handleTaskExecution(ctx, execution, func(ctx context.Context, state domain.ExecutionState) error {
			return s.retryExecutionStateHandler(ctx, state, retryStrategy)
		})
		if err1 != nil {
			s.logger.Error("正常调度任务失败",
				elog.Any("execution", execution),
				elog.FieldErr(err1))
		}
	}()
	return nil
}

// retryExecutionStateHandler 重试流程的状态机处理器
func (s *SingleTaskRunner) retryExecutionStateHandler(ctx context.Context, state domain.ExecutionState, retryStrategy strategy.Strategy) error {
	execution, err := s.execSvc.GetByID(ctx, state.ID)
	if err != nil {
		return errs.ErrExecutionNotFound
	}
	switch {
	case execution.Status.IsFailedRetryable() && state.Status.IsRunning():
		return s.setRunningState(ctx, state)
	case execution.Status.IsRunning() && state.Status.IsRunning():
		return s.updateRunningProgress(ctx, state)
	case (execution.Status.IsFailedRetryable() || execution.Status.IsRunning()) && state.Status.IsTerminalStatus():
		defer func() {
			// 删除注册的状态处理器
			s.executionStateHandlers.Delete(execution.ID)
			// 释放任务
			s.releaseTask(ctx, execution.Task)
		}()
		return s.updateRetryResult(ctx, execution, state, retryStrategy)
	default:
		s.logger.Info("重试补偿不支持的状态转换",
			elog.Int64("taskID", execution.Task.ID),
			elog.String("taskName", execution.Task.Name),
			elog.String("currentStatus", execution.Status.String()),
			elog.String("finalStatus", state.Status.String()))
		return errs.ErrInvalidTaskExecutionStatus
	}
}

func (s *SingleTaskRunner) updateRetryResult(ctx context.Context, execution domain.TaskExecution, result domain.ExecutionState, retryStrategy strategy.Strategy) error {
	// FAILED_RETRYABLE 或者 RUNNING  → 终态：重试任务直接完成（成功或最终失败）
	s.logger.Info("重试任务完成",
		elog.Int64("executionID", result.ID),
		elog.String("currentStatus", execution.Status.String()),
		elog.String("finalStatus", result.Status.String()))

	// 更新调度信息
	if result.RequestReschedule && result.Status.IsFailedRescheduled() {
		execution.MergeTaskScheduleParams(result.RescheduleParams)
	}

	// 增加重试计数
	execution.RetryCount++
	// 计算出下次重试时间
	duration, shouldRetry := retryStrategy.NextWithRetries(int32(execution.RetryCount))
	if shouldRetry {
		execution.NextRetryTime = time.Now().Add(duration).UnixMilli()
	}
	// 不管是否达到最大重试次数，都要更新状态（主要是重试次数），这样下次重试补偿任务会因其超过最大重试次数而不再重试
	err := s.execSvc.UpdateRetryResult(ctx,
		result.ID,
		execution.RetryCount,
		execution.NextRetryTime,
		result.Status,
		result.RunningProgress,
		time.Now().UnixMilli(),
		execution.Task.ScheduleParams)
	if err != nil {
		s.logger.Error("更新执行计划重试结果失败",
			elog.Int64("taskID", execution.Task.ID),
			elog.String("taskName", execution.Task.Name),
			elog.Any("result", result),
			elog.FieldErr(err))
		if !shouldRetry {
			return fmt.Errorf("%w: %w", errs.ErrExecutionMaxRetriesExceeded, err)
		}
		return err
	}
	if !shouldRetry {
		return errs.ErrExecutionMaxRetriesExceeded
	}
	return nil
}

func (s *SingleTaskRunner) Reschedule(ctx context.Context, execution domain.TaskExecution) error {
	// 抢占任务
	acquiredTask, err := s.acquireTask(ctx, execution.Task)
	if err != nil {
		s.logger.Error("重调度：任务抢占失败",
			elog.Int64("taskID", execution.Task.ID),
			elog.String("taskName", execution.Task.Name),
			elog.FieldErr(err))
		return err
	}
	execution.Task = *acquiredTask

	// 抢占和创建都成功，异步触发任务
	go func() {
		err1 := s.handleTaskExecution(ctx, execution, s.rescheduleExecutionStateHandler)
		if err1 != nil {
			s.logger.Error("正常调度任务失败",
				elog.Any("execution", execution),
				elog.FieldErr(err1))
		}
	}()
	return nil
}

// rescheduleExecutionStateHandler 重调度流程的状态机处理器
func (s *SingleTaskRunner) rescheduleExecutionStateHandler(ctx context.Context, state domain.ExecutionState) error {
	execution, err := s.execSvc.GetByID(ctx, state.ID)
	if err != nil {
		return errs.ErrExecutionNotFound
	}
	switch {
	case execution.Status.IsFailedRescheduled() && state.Status.IsRunning():
		return s.setRunningState(ctx, state)
	case execution.Status.IsRunning() && state.Status.IsRunning():
		return s.updateRunningProgress(ctx, state)
	case (execution.Status.IsFailedRescheduled() || execution.Status.IsRunning()) && state.Status.IsTerminalStatus():
		defer func() {
			// 删除注册的状态处理器
			s.executionStateHandlers.Delete(execution.ID)
			// 释放任务
			s.releaseTask(ctx, execution.Task)
		}()
		// 发送完成事件
		s.sendCompletedEvent(ctx, state, execution)
		return s.updateScheduleResult(ctx, execution, state)
	default:
		s.logger.Info("重调度补偿不支持的状态转换",
			elog.Int64("taskID", execution.Task.ID),
			elog.String("taskName", execution.Task.Name),
			elog.String("currentStatus", execution.Status.String()),
			elog.String("finalStatus", state.Status.String()))
		return errs.ErrInvalidTaskExecutionStatus
	}
}

func (s *SingleTaskRunner) sendCompletedEvent(ctx context.Context, state domain.ExecutionState, execution domain.TaskExecution) {
	switch state.Status {
	case domain.TaskExecutionStatusSuccess, domain.TaskExecutionStatusFailed:
		err := s.producer.Produce(ctx, event.Event{
			PlanID: execution.Task.PlanID,
			TaskID: execution.Task.ID,
			Type:   domain.NormalTaskType,
		})
		if err != nil {
			s.logger.Error("发送完成事件失败", elog.Int64("taskID", execution.Task.ID), elog.FieldErr(err))
		}
	default:
		// 其他状态不用做处理
	}
}
