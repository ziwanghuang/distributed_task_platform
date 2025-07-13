package runner

import (
	"context"
	"fmt"
	"gitee.com/flycash/distributed_task_platform/internal/service/acquirer"
	"gitee.com/flycash/distributed_task_platform/internal/service/invoker"
	"time"

	"gitee.com/flycash/distributed_task_platform/internal/event"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/errs"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"gitee.com/flycash/distributed_task_platform/pkg/retry"
	"gitee.com/flycash/distributed_task_platform/pkg/retry/strategy"
	"github.com/gotomicro/ego/core/elog"
)

var _ Runner = &SingleTaskRunner{}

type SingleTaskRunner struct {
	nodeID        string                 // 当前调度节点ID
	taskSvc       task.Service           // 任务服务
	execSvc       task.ExecutionService  // 任务执行服务
	taskAcquirer  acquirer.TaskAcquirer  // 任务抢占器
	invoker       invoker.Invoker        // 这里一般来说就是 invoker.Dispatcher
	producer      event.CompleteProducer // 任务完成事件生产者
	renewInterval time.Duration          // 续约间隔

	logger *elog.Component
}

func NewSingleTaskRunner(
	nodeID string,
	taskSvc task.Service,
	execSvc task.ExecutionService,
	taskAcquirer acquirer.TaskAcquirer,
	invoker invoker.Invoker,
	producer event.CompleteProducer,
	renewInterval time.Duration,
) *SingleTaskRunner {
	return &SingleTaskRunner{
		nodeID:        nodeID,
		taskSvc:       taskSvc,
		execSvc:       execSvc,
		taskAcquirer:  taskAcquirer,
		invoker:       invoker,
		producer:      producer,
		renewInterval: renewInterval,
	}
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
	// 根据分片规则区分任务类型
	if acquiredTask.ShardingRule == nil {
		return s.handleNormalTask(ctx, acquiredTask)
	} else {
		return s.handleShardingTask(ctx, acquiredTask)
	}
}

func (s *SingleTaskRunner) handleNormalTask(ctx context.Context, task domain.Task) error {
	// 抢占成功，立即创建TaskExecution记录
	execution, err := s.execSvc.Create(ctx, domain.TaskExecution{
		Task: task,
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
		s.releaseTask(ctx, task)
		return err
	}

	// 抢占和创建都成功，异步触发任务
	go func() {
		// 执行任务
		result, err1 := s.invoker.Run(ctx, execution)
		if err1 != nil {
			s.logger.Error("执行器执行任务失败", elog.FieldErr(err))
			return
		}
		err1 = s.execSvc.HandleState(ctx, result)
		if err1 != nil {
			s.logger.Error("正常调度任务失败",
				elog.Any("execution", execution),
				elog.FieldErr(err1))
		}
	}()
	return nil
}

func (s *SingleTaskRunner) handleShardingTask(ctx context.Context, task domain.Task) error {
	// 抢占成功，立即创建子任务执行记录
	executions, err := s.execSvc.CreateShardingChildren(ctx, domain.TaskExecution{
		Task: task,
		// 可以认为开始执行了，防止执行节点直接返回"终态"状态Failed，Success等
		StartTime: time.Now().UnixMilli(),
		Status:    domain.TaskExecutionStatusPrepare,
	}, task.ShardingRule.ToScheduleParams())
	if err != nil {
		s.logger.Error("创建子任务执行记录失败",
			elog.Int64("taskID", task.ID),
			elog.String("taskName", task.Name),
			elog.FieldErr(err))
		// 释放任务
		s.releaseTask(ctx, task)
		return err
	}

	// 抢占和创建都成功，异步触发任务
	for i := range executions {
		execution := executions[i]
		go func() {

			// 执行任务
			result, err1 := s.invoker.Run(ctx, execution)
			if err1 != nil {
				s.logger.Error("执行器执行任务失败", elog.FieldErr(err))
				return
			}
			err1 = s.execSvc.HandleState(ctx, result)
			if err1 != nil {
				s.logger.Error("正常调度子任务失败",
					elog.Any("execution", execution),
					elog.FieldErr(err1))
			}
		}()
	}
	return nil
}

// acquireTask 抢占任务
func (s *SingleTaskRunner) acquireTask(ctx context.Context, task domain.Task) (domain.Task, error) {
	// 抢占任务
	acquiredTask, err := s.taskAcquirer.Acquire(ctx, task.ID, s.nodeID)
	if err != nil {
		return domain.Task{}, fmt.Errorf("任务抢占失败: %w", err)
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
	// 更新任务执行时间
	_, err := s.taskSvc.UpdateNextTime(ctx, taskID)
	if err != nil {
		s.logger.Error("更新任务下次执行时间失败",
			elog.Int64("taskID", taskID),
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
	execution.Task = acquiredTask

	// 抢占和创建都成功，异步触发任务
	go func() {

		// 执行任务
		result, err1 := s.invoker.Run(ctx, execution)
		if err1 != nil {
			s.logger.Error("执行器执行任务失败", elog.FieldErr(err))
			return
		}
		err1 = s.retryExecutionStateHandler(ctx, result, retryStrategy)
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
		// 当前不是最后一次重试，计算下次重试时间
		execution.NextRetryTime = time.Now().Add(duration).UnixMilli()
	} else {
		// 当前是最后一次重试，只要不是成功状态一律设置为失败
		if !result.Status.IsSuccess() {
			result.Status = domain.TaskExecutionStatusFailed
		}
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
	execution.Task = acquiredTask

	// 抢占和创建都成功，异步触发任务
	go func() {
		// 执行任务
		result, err1 := s.invoker.Run(ctx, execution)
		if err1 != nil {
			s.logger.Error("执行器执行任务失败", elog.FieldErr(err))
			return
		}
		err1 = s.rescheduleExecutionStateHandler(ctx, result)
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
