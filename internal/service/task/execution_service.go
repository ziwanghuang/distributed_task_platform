package task

import (
	"context"
	"fmt"
	"gitee.com/flycash/distributed_task_platform/internal/errs"
	"gitee.com/flycash/distributed_task_platform/internal/event"
	"gitee.com/flycash/distributed_task_platform/internal/service/acquirer"
	"github.com/gotomicro/ego/core/elog"
	"go.uber.org/multierr"
	"time"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/repository"
)

// ExecutionService 任务执行服务接口
type ExecutionService interface {
	// Create 创建任务执行实例
	Create(ctx context.Context, execution domain.TaskExecution) (domain.TaskExecution, error)
	// CreateShardingChildren 创建分片子任务执行实例
	CreateShardingChildren(ctx context.Context, execution domain.TaskExecution, scheduleParams []map[string]string) ([]domain.TaskExecution, error)
	// GetByID 根据ID获取执行实例
	GetByID(ctx context.Context, id int64) (domain.TaskExecution, error)
	// UpdateStatus 更新执行状态
	UpdateStatus(ctx context.Context, id int64, status domain.TaskExecutionStatus) error
	// FindRetryableExecutions 查找所有可以重试的执行记录
	// maxRetryCount: 最大重试次数限制
	// prepareTimeoutMs: PREPARE状态超时时间（毫秒），超过此时间未执行视为超时
	// limit: 查询结果数量限制
	FindRetryableExecutions(ctx context.Context, maxRetryCount int64, prepareTimeoutMs int64, limit int) ([]domain.TaskExecution, error)
	// UpdateRetryResult 更新重试结果
	UpdateRetryResult(ctx context.Context, id, retryCount, nextRetryTime int64, status domain.TaskExecutionStatus, progress int32, endTime int64, scheduleParams map[string]string) error
	// SetRunningState 设置任务为运行状态并更新进度、开始时间
	SetRunningState(ctx context.Context, id int64, progress int32) error
	// UpdateRunningProgress 更新任务执行进度（仅在RUNNING状态下有效）
	UpdateRunningProgress(ctx context.Context, id int64, progress int32) error
	// UpdateScheduleResult 更新调度结果
	UpdateScheduleResult(ctx context.Context, id int64, status domain.TaskExecutionStatus, progress int32, endTime int64, scheduleParams map[string]string) error
	// FindReschedulableExecutions 查找所有可以重调度的执行记录
	FindReschedulableExecutions(ctx context.Context, limit int) ([]domain.TaskExecution, error)

	FindExecutionByTaskIDAndPlanExecID(ctx context.Context, taskID int64, planExecID int64) (domain.TaskExecution, error)

	HandleReports(ctx context.Context, reports []*domain.Report) error

	HandleState(ctx context.Context, state domain.ExecutionState) error
}

type executionService struct {
	repo         repository.TaskExecutionRepository
	logger       *elog.Component
	taskAcquirer acquirer.TaskAcquirer // 任务抢占器
	taskSvc      Service
	producer     event.CompleteProducer // 任务完成事件生产者
	nodeID       string
}

// NewExecutionService 创建任务执行服务实例
func NewExecutionService(repo repository.TaskExecutionRepository) ExecutionService {
	return &executionService{repo: repo, logger: elog.DefaultLogger}
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

func (s *executionService) HandleState(ctx context.Context, state domain.ExecutionState) error {
	execution, err := s.GetByID(ctx, state.ID)
	if err != nil {
		return errs.ErrExecutionNotFound
	}
	switch {
	case state.Status.IsRunning():
		return s.SetRunningState(ctx, state.ID, state.RunningProgress)
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

func (s *executionService) updateScheduleResult(ctx context.Context, execution domain.TaskExecution, result domain.ExecutionState) error {
	if result.RequestReschedule && result.Status.IsFailedRescheduled() {
		execution.MergeTaskScheduleParams(result.RescheduleParams)
	}
	err := s.UpdateScheduleResult(ctx,
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

func (s *executionService) CreateShardingChildren(ctx context.Context, execution domain.TaskExecution, scheduleParams []map[string]string) ([]domain.TaskExecution, error) {
	return s.repo.CreateShardingChildren(ctx, execution, scheduleParams)
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

func (s *executionService) UpdateRetryResult(ctx context.Context, id, retryCount, nextRetryTime int64, status domain.TaskExecutionStatus, progress int32, endTime int64, scheduleParams map[string]string) error {
	return s.repo.UpdateRetryResult(ctx, id, retryCount, nextRetryTime, status, progress, endTime, scheduleParams)
}

func (s *executionService) SetRunningState(ctx context.Context, id int64, progress int32) error {
	return s.repo.SetRunningState(ctx, id, progress)
}

func (s *executionService) UpdateRunningProgress(ctx context.Context, id int64, progress int32) error {
	return s.repo.UpdateRunningProgress(ctx, id, progress)
}

func (s *executionService) UpdateScheduleResult(ctx context.Context, id int64, status domain.TaskExecutionStatus, progress int32, endTime int64, scheduleParams map[string]string) error {
	return s.repo.UpdateScheduleResult(ctx, id, status, progress, endTime, scheduleParams)
}

func (s *executionService) FindReschedulableExecutions(ctx context.Context, limit int) ([]domain.TaskExecution, error) {
	return s.repo.FindReschedulableExecutions(ctx, limit)
}
