package task

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gitee.com/flycash/distributed_task_platform/internal/errs"
	"gitee.com/flycash/distributed_task_platform/internal/event"
	"gitee.com/flycash/distributed_task_platform/internal/service/acquirer"
	"gitee.com/flycash/distributed_task_platform/pkg/retry"
	"github.com/gotomicro/ego/core/elog"
	"go.uber.org/multierr"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/repository"
)

// ExecutionService 任务执行服务接口
type ExecutionService interface {
	// Create 创建任务执行实例
	Create(ctx context.Context, execution domain.TaskExecution) (domain.TaskExecution, error)
	// CreateShardingChildren 创建分片子任务执行实例
	CreateShardingChildren(ctx context.Context, parent domain.TaskExecution) ([]domain.TaskExecution, error)
	// GetByID 根据ID获取执行实例
	GetByID(ctx context.Context, id int64) (domain.TaskExecution, error)
	// UpdateStatus 更新执行状态
	UpdateStatus(ctx context.Context, id int64, status domain.TaskExecutionStatus) error
	// FindRetryableExecutions 查找所有可以重试的执行记录
	// maxRetryCount: 最大重试次数限制
	// prepareTimeoutMs: PREPARE状态超时时间（毫秒），超过此时间未执行视为超时
	// limit: 查询结果数量限制
	FindRetryableExecutions(ctx context.Context, maxRetryCount int64, prepareTimeoutMs int64, limit int) ([]domain.TaskExecution, error)
	// FindShardingParents 查找分片父任务
	FindShardingParents(ctx context.Context, batchSize int) ([]domain.TaskExecution, error)
	// FindShardingChildren 查找分片子任务
	FindShardingChildren(ctx context.Context, parentID int64) ([]domain.TaskExecution, error)
	// UpdateRetryResult 更新重试结果
	UpdateRetryResult(ctx context.Context, id, retryCount, nextRetryTime int64, status domain.TaskExecutionStatus, progress int32, endTime int64, scheduleParams map[string]string, executorNodeID string) error
	// SetRunningState 设置任务为运行状态并更新进度
	SetRunningState(ctx context.Context, id int64, progress int32, executorNodeID string) error
	// UpdateRunningProgress 更新任务执行进度（仅在RUNNING状态下有效）
	UpdateRunningProgress(ctx context.Context, id int64, progress int32) error
	// UpdateScheduleResult 更新调度结果
	UpdateScheduleResult(ctx context.Context, id int64, status domain.TaskExecutionStatus, progress int32, endTime int64, scheduleParams map[string]string, executorNodeID string) error
	// FindReschedulableExecutions 查找所有可以重调度的执行记录
	FindReschedulableExecutions(ctx context.Context, limit int) ([]domain.TaskExecution, error)

	FindExecutionByTaskIDAndPlanExecID(ctx context.Context, taskID int64, planExecID int64) (domain.TaskExecution, error)

	HandleReports(ctx context.Context, reports []*domain.Report) error

	HandleState(ctx context.Context, state domain.ExecutionState) error
}

type executionService struct {
	nodeID       string
	repo         repository.TaskExecutionRepository
	taskSvc      Service
	taskAcquirer acquirer.TaskAcquirer  // 任务抢占器
	producer     event.CompleteProducer // 任务完成事件生产者
	logger       *elog.Component
}

// NewExecutionService 创建任务执行服务实例
func NewExecutionService(
	nodeID string,
	repo repository.TaskExecutionRepository,
	taskSvc Service,
	taskAcquirer acquirer.TaskAcquirer,
	producer event.CompleteProducer,
) ExecutionService {
	return &executionService{
		nodeID:       nodeID,
		repo:         repo,
		taskSvc:      taskSvc,
		taskAcquirer: taskAcquirer,
		producer:     producer,
		logger:       elog.DefaultLogger.With(elog.FieldComponentName("service.execution")),
	}
}

func (s *executionService) HandleReports(ctx context.Context, reports []*domain.Report) error {
	if len(reports) == 0 {
		return nil
	}
	s.logger.Debug("开始处理执行状态上报", elog.Int("count", len(reports)))

	var err error
	processedCount := 0
	skippedCount := 0

	for i := range reports {
		err1 := s.HandleState(ctx, reports[i].ExecutionState)
		if err1 != nil {
			skippedCount++
			s.logger.Error("处理执行节点上报的结果失败",
				elog.Any("result", reports[i].ExecutionState),
				elog.FieldErr(err1))
			// 包装错误，添加上报场景的特定信息
			err = multierr.Append(err,
				fmt.Errorf("处理执行节点上报的结果失败: taskID=%d, executionID=%d: %w",
					reports[i].ExecutionState.TaskID, reports[i].ExecutionState.ID, err1))
			continue
		}
		processedCount++
	}

	// 记录处理统计信息
	s.logger.Info("执行状态上报处理完成",
		elog.Int("total", len(reports)),
		elog.Int("processed", processedCount),
		elog.Int("skipped", skippedCount))
	return err
}

// HandleState 只看上报了什么，完全不看当前是什么状态, 状态迁移有问题：
func (s *executionService) HandleState(ctx context.Context, state domain.ExecutionState) error {
	execution, err := s.GetByID(ctx, state.ID)
	if err != nil {
		return errs.ErrExecutionNotFound
	}

	// 如果一个任务的当前状态已经是 SUCCESS，但因为网络延迟，
	// 又收到了一个它之前上报的 RUNNING 状态，
	// 这个代码会毫无防备地尝试去执行 SetRunningState，这直接破坏了状态机的基本规则
	switch {
	case state.Status.IsRunning():
		return s.setRunningState(ctx, state)
	case state.Status.IsTerminalStatus():
		defer func() {
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
		if _, err3 := s.taskSvc.UpdateNextTime(ctx, execution.Task.ID); err3 != nil {
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

// HandleStateV1 同时检查‘当前状态’和‘上报状态’，确保了任何状态变更都必须符合我们预设的规则
func (s *executionService) HandleStateV1(ctx context.Context, state domain.ExecutionState) error {
	execution, err := s.GetByID(ctx, state.ID)
	if err != nil {
		return errs.ErrExecutionNotFound
	}
	switch {
	case (execution.Status.IsPrepare() ||
		execution.Status.IsFailedRetryable() ||
		execution.Status.IsFailedRescheduled()) && state.Status.IsRunning():
		return s.setRunningState(ctx, state)
	case execution.Status.IsRunning() && state.Status.IsRunning():
		// 正常调度，重试，重调度共享状态转换
		return s.updateRunningProgress(ctx, state)
	case execution.Status.IsRunning() && state.Status.IsTerminalStatus():
		// BUG：缺少业务上下文，无法知道 execution 是正常调度，重试还是重调度。
		return nil
	case execution.Status.IsFailedRetryable() && state.Status.IsTerminalStatus():
		// 发送完成事件
		s.sendCompletedEvent(ctx, state, execution)
		// 重试后立即到达终态
		err = s.updateRetryResult(ctx, execution, state)
		if err != nil {
			s.logger.Error("更新任务执行记录的重试结果失败",
				elog.Int64("taskID", state.TaskID),
				elog.String("taskName", state.TaskName),
				elog.Any("state", state),
				elog.FieldErr(err))
			return err
		}
		return nil
	case (execution.Status.IsPrepare() || execution.Status.IsFailedRescheduled()) && state.Status.IsTerminalStatus():
		// 正常调度或者重调度到达终态
		// 发送完成事件
		s.sendCompletedEvent(ctx, state, execution)

		var err1 error
		if err2 := s.updateScheduleResult(ctx, execution, state); err2 != nil {
			err1 = multierr.Append(err1, fmt.Errorf("更新任务执行记录的调度结果失败：%w", err2))
		}

		// 不管本次调度是否成功，都要更新task的下一次执行时间，分片任务除外，分片任务会在补偿任务重进行
		isShardedChild := execution.ShardingParentID != nil && *execution.ShardingParentID > 0
		if !isShardedChild {
			// 释放任务
			s.releaseTask(ctx, execution.Task)
			if _, err3 := s.taskSvc.UpdateNextTime(ctx, execution.Task.ID); err3 != nil {
				err1 = multierr.Append(err1, fmt.Errorf("更新任务下次更新时间失败：%w", err3))
			}
		}
		return err1
	default:
		s.logger.Info("不支持的状态转换",
			elog.Int64("taskID", execution.Task.ID),
			elog.String("taskName", execution.Task.Name),
			elog.String("currentStatus", execution.Status.String()),
			elog.String("finalStatus", state.Status.String()))
		return errs.ErrInvalidTaskExecutionStatus
	}
}

func (s *executionService) HandleStateV2(ctx context.Context, state domain.ExecutionState) error {
	execution, err := s.GetByID(ctx, state.ID)
	if err != nil {
		return errs.ErrExecutionNotFound
	}
	switch state.Type {
	case domain.ExecutionTypeNormal, domain.ExecutionTypeReschedule:
		return s.handleScheduleExecutionState(ctx, execution, state)
	case domain.ExecutionTypeRetry:
		return s.handleRetryExecutionState(ctx, execution, state)
	default:
		s.logger.Error("未知的执行类型", elog.String("type", string(state.Type)))
		// return errs.ErrInvalidExecutionType
		return errors.New("未知的执行类型")
	}
	/*
				终极BUG场景
				1. prepare 发出请求在schedule_context中传递type = normal，【执行节点1 】长时间运行，无上报状态
				2. retry补偿任务找到，prepare 超时的， 在 scheduler_context 中传递type = retry，【执行节点2 】长时间运行，无上报状态
				3. 【执行节点1 】 运行结束上报，返回 scheduler_context 中传递type = normal，status
				4. 【执行节点2 】运行结束上报，返回 scheduler_context 中传递type = retry, status
				当3和4不是中status 不是终态，RUNNING，会混合。如果是终态，那么后到的state会覆盖先到的state，
			    比如：  【执行节点2 】 返回的， scheduler_context 中传递type = retry, status = success，可能会被【执行节点1 】 返回的 schedule_context中传递type = normal，status=failed 覆盖

				解决方案：
		         1. 在 dao层 TaskExecution 中 增加 version字段
		         2. 在 retry补偿任务找到需要重试的prepare的时候，直接将其状态设置为 ”failed-retryable“ 使version+1，这样正常调度时的（执行节点1）上报因为 version 不匹配而被忽略。
		         3. scheduler_context 中添加 type = normal/retry/reschedule（是否更新执行时间），version = 2

	*/
}

func (s *executionService) handleScheduleExecutionState(ctx context.Context, execution domain.TaskExecution, state domain.ExecutionState) error {
	switch {
	// prepare/failed-reschedule -> running -> running -> success, failed, {failed-retryable, failed-reschedule}
	// prepare/failed-reschedule -> success, failed,{failed-retryable, failed-reschedule}
	// running -> success, failed,{failed-retryable, failed-reschedule}
	case (execution.Status.IsPrepare() || execution.Status.IsFailedRescheduled()) && state.Status.IsRunning():
		return s.setRunningState(ctx, state)
	case execution.Status.IsRunning() && state.Status.IsRunning():
		return s.updateRunningProgress(ctx, state)
	case (execution.Status.IsPrepare() || execution.Status.IsFailedRescheduled() || execution.Status.IsRunning()) && state.Status.IsTerminalStatus():
		// 正常调度或者重调度到达终态
		// 发送完成事件
		s.sendCompletedEvent(ctx, state, execution)
		var err1 error
		if err2 := s.updateScheduleResult(ctx, execution, state); err2 != nil {
			err1 = multierr.Append(err1, fmt.Errorf("更新任务执行记录的调度结果失败：%w", err2))
		}
		// 不管本次调度是否成功，都要更新task的下一次执行时间，分片任务除外，分片任务会在补偿任务重进行
		isShardedChild := execution.ShardingParentID != nil && *execution.ShardingParentID > 0
		if !isShardedChild {
			// 释放任务
			s.releaseTask(ctx, execution.Task)
			// TODO：更新时间问题，success，failed，failed-retryable，立即更新，但是failed-reschedule是不是要等到迁移到success，failed才更新？
			if _, err3 := s.taskSvc.UpdateNextTime(ctx, execution.Task.ID); err3 != nil {
				err1 = multierr.Append(err1, fmt.Errorf("更新任务下次更新时间失败：%w", err3))
			}
		}
		return err1
	default:
		s.logger.Info("正常调度或重调度不支持的状态转换",
			elog.Int64("taskID", execution.Task.ID),
			elog.String("taskName", execution.Task.Name),
			elog.String("currentStatus", execution.Status.String()),
			elog.String("finalStatus", state.Status.String()))
		return errs.ErrInvalidTaskExecutionStatus

	}
}

func (s *executionService) setRunningState(ctx context.Context, state domain.ExecutionState) error {
	err := s.SetRunningState(ctx, state.ID, state.RunningProgress, state.ExecutorNodeID)
	if err != nil {
		s.logger.Error("更新为运行状态失败",
			elog.Int64("taskID", state.TaskID),
			elog.String("taskName", state.TaskName),
			elog.Any("state", state),
			elog.FieldErr(err))
		return err
	}
	return nil
}

func (s *executionService) updateRunningProgress(ctx context.Context, state domain.ExecutionState) error {
	err := s.UpdateRunningProgress(ctx, state.TaskID, state.RunningProgress)
	if err != nil {
		s.logger.Error("更新运行进度失败",
			elog.Int64("taskID", state.TaskID),
			elog.String("taskName", state.TaskName),
			elog.Any("state", state),
			elog.FieldErr(err))
		return err
	}
	return nil
}

func (s *executionService) updateScheduleResult(ctx context.Context, execution domain.TaskExecution, result domain.ExecutionState) error {
	if result.RequestReschedule && result.Status.IsFailedRescheduled() {
		execution.MergeTaskScheduleParams(result.RescheduleParams)
	}
	err := s.UpdateScheduleResult(ctx,
		result.ID,
		result.Status,
		result.RunningProgress,
		time.Now().UnixMilli(),
		execution.Task.ScheduleParams,
		result.ExecutorNodeID)
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

func (s *executionService) handleRetryExecutionState(ctx context.Context, execution domain.TaskExecution, state domain.ExecutionState) error {
	switch {
	// failed-retryable -> running -> running -> success, failed, {failed-retryable, failed-reschedule}
	// failed-retryable -> success, failed,{failed-retryable, failed-reschedule}
	// running -> success, failed,{failed-retryable, failed-reschedule}
	case execution.Status.IsFailedRetryable() && state.Status.IsRunning():
		return s.setRunningState(ctx, state)
	case execution.Status.IsRunning() && state.Status.IsRunning():
		return s.updateRunningProgress(ctx, state)
	case (execution.Status.IsFailedRetryable() || execution.Status.IsRunning()) && state.Status.IsTerminalStatus():
		// 发送完成事件
		s.sendCompletedEvent(ctx, state, execution)
		// 重试后立即到达终态
		err := s.updateRetryResult(ctx, execution, state)
		if err != nil {
			s.logger.Error("更新任务执行记录的重试结果失败",
				elog.Int64("taskID", state.TaskID),
				elog.String("taskName", state.TaskName),
				elog.Any("state", state),
				elog.FieldErr(err))
			return err
		}
		return nil
	default:
		s.logger.Info("重试补偿不支持的状态转换",
			elog.Int64("taskID", execution.Task.ID),
			elog.String("taskName", execution.Task.Name),
			elog.String("currentStatus", execution.Status.String()),
			elog.String("finalStatus", state.Status.String()))
		return errs.ErrInvalidTaskExecutionStatus
	}
}

func (s *executionService) updateRetryResult(ctx context.Context, execution domain.TaskExecution, state domain.ExecutionState) error {
	// FAILED_RETRYABLE 或者 RUNNING  → 终态：重试任务直接完成（成功或最终失败）
	s.logger.Info("重试任务完成",
		elog.Int64("executionID", state.ID),
		elog.String("currentStatus", execution.Status.String()),
		elog.String("finalStatus", state.Status.String()))

	// 更新调度信息
	if state.RequestReschedule && state.Status.IsFailedRescheduled() {
		execution.MergeTaskScheduleParams(state.RescheduleParams)
	}

	// 增加重试计数
	execution.RetryCount++
	// 计算出下次重试时间
	retryStrategy, _ := retry.NewRetry(execution.Task.RetryConfig.ToRetryComponentConfig())
	duration, shouldRetry := retryStrategy.NextWithRetries(int32(execution.RetryCount))
	if shouldRetry {
		// 当前不是最后一次重试，计算下次重试时间
		execution.NextRetryTime = time.Now().Add(duration).UnixMilli()
	} else if !state.Status.IsSuccess() {
		// 当前是最后一次重试，只要不是成功状态一律设置为失败
		state.Status = domain.TaskExecutionStatusFailed
	}
	// 不管是否达到最大重试次数，都要更新状态（主要是重试次数），这样下次重试补偿任务会因其超过最大重试次数而不再重试
	err := s.UpdateRetryResult(ctx,
		state.ID,
		execution.RetryCount,
		execution.NextRetryTime,
		state.Status,
		state.RunningProgress,
		time.Now().UnixMilli(),
		execution.Task.ScheduleParams,
		state.ExecutorNodeID)
	if err != nil {
		s.logger.Error("更新执行计划重试结果失败",
			elog.Int64("taskID", execution.Task.ID),
			elog.String("taskName", execution.Task.Name),
			elog.Any("result", state),
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

func (s *executionService) releaseTask(ctx context.Context, task domain.Task) {
	if err := s.taskAcquirer.Release(ctx, task.ID, s.nodeID); err != nil {
		s.logger.Error("释放任务失败",
			elog.Int64("taskID", task.ID),
			elog.String("taskName", task.Name),
			elog.FieldErr(err))
	}
}

func (s *executionService) sendCompletedEvent(ctx context.Context, state domain.ExecutionState, execution domain.TaskExecution) {
	switch state.Status {
	case domain.TaskExecutionStatusSuccess, domain.TaskExecutionStatusFailed:
		err := s.producer.Produce(ctx, event.Event{
			PlanID: execution.Task.PlanID,
			TaskID: execution.Task.ID,
			Name:   execution.Task.Name,
			Type:   domain.NormalTaskType,
		})
		if err != nil {
			s.logger.Error("发送完成事件失败", elog.Int64("taskID", execution.Task.ID), elog.FieldErr(err))
		}
	default:
		// 其他状态不用做处理
	}
}

func (s *executionService) FindExecutionByTaskIDAndPlanExecID(ctx context.Context, taskID, planExecID int64) (domain.TaskExecution, error) {
	return s.repo.FindExecutionByTaskIDAndPlanExecID(ctx, taskID, planExecID)
}

func (s *executionService) Create(ctx context.Context, execution domain.TaskExecution) (domain.TaskExecution, error) {
	return s.repo.Create(ctx, execution)
}

func (s *executionService) CreateShardingChildren(ctx context.Context, parent domain.TaskExecution) ([]domain.TaskExecution, error) {
	return s.repo.CreateShardingChildren(ctx, parent)
}

func (s *executionService) UpdateStatus(ctx context.Context, id int64, status domain.TaskExecutionStatus) error {
	return s.repo.UpdateStatus(ctx, id, status)
}

func (s *executionService) GetByID(ctx context.Context, id int64) (domain.TaskExecution, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *executionService) FindRetryableExecutions(ctx context.Context, maxRetryCount, prepareTimeoutMs int64, limit int) ([]domain.TaskExecution, error) {
	return s.repo.FindRetryableExecutions(ctx, maxRetryCount, prepareTimeoutMs, limit)
}

func (s *executionService) FindShardingParents(ctx context.Context, batchSize int) ([]domain.TaskExecution, error) {
	return s.repo.FindShardingParents(ctx, batchSize)
}

func (s *executionService) FindShardingChildren(ctx context.Context, parentID int64) ([]domain.TaskExecution, error) {
	return s.repo.FindShardingChildren(ctx, parentID)
}

func (s *executionService) UpdateRetryResult(ctx context.Context, id, retryCount, nextRetryTime int64, status domain.TaskExecutionStatus, progress int32, endTime int64, scheduleParams map[string]string, executorNodeID string) error {
	return s.repo.UpdateRetryResult(ctx, id, retryCount, nextRetryTime, status, progress, endTime, scheduleParams, executorNodeID)
}

func (s *executionService) SetRunningState(ctx context.Context, id int64, progress int32, executorNodeID string) error {
	return s.repo.SetRunningState(ctx, id, progress, executorNodeID)
}

func (s *executionService) UpdateRunningProgress(ctx context.Context, id int64, progress int32) error {
	return s.repo.UpdateRunningProgress(ctx, id, progress)
}

func (s *executionService) UpdateScheduleResult(ctx context.Context, id int64, status domain.TaskExecutionStatus, progress int32, endTime int64, scheduleParams map[string]string, executorNodeID string) error {
	return s.repo.UpdateScheduleResult(ctx, id, status, progress, endTime, scheduleParams, executorNodeID)
}

func (s *executionService) FindReschedulableExecutions(ctx context.Context, limit int) ([]domain.TaskExecution, error) {
	return s.repo.FindReschedulableExecutions(ctx, limit)
}
