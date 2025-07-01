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
	// GetByID 根据ID获取执行实例
	GetByID(ctx context.Context, id int64) (domain.TaskExecution, error)
	// UpdateStatus 更新执行状态
	UpdateStatus(ctx context.Context, id int64, status domain.TaskExecutionStatus) error
}

type executionService struct {
	repo repository.TaskExecutionRepository
}

// NewExecutionService 创建任务执行服务实例
func NewExecutionService(repo repository.TaskExecutionRepository) ExecutionService {
	return &executionService{repo: repo}
}

func (s *executionService) Create(ctx context.Context, execution domain.TaskExecution) (domain.TaskExecution, error) {
	return s.repo.Create(ctx, execution)
}

func (s *executionService) UpdateStatus(ctx context.Context, id int64, status domain.TaskExecutionStatus) error {
	return s.repo.UpdateStatus(ctx, id, status)
}

func (s *executionService) GetByID(ctx context.Context, id int64) (domain.TaskExecution, error) {
	return s.repo.GetByID(ctx, id)
}
