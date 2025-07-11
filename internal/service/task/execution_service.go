package task

import (
	"context"

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
}

type executionService struct {
	repo repository.TaskExecutionRepository
}

// NewExecutionService 创建任务执行服务实例
func NewExecutionService(repo repository.TaskExecutionRepository) ExecutionService {
	return &executionService{repo: repo}
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
