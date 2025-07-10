package task

import (
	"context"
	"fmt"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/errs"
	"gitee.com/flycash/distributed_task_platform/internal/repository"
)

// Service 任务服务接口
type Service interface {
	// Create 创建任务
	Create(ctx context.Context, task domain.Task) (domain.Task, error)
	// SchedulableTasks 获取可调度的任务列表，preemptedTimeoutMs 表示处于 PREEMPTED 状态任务的超时时间（毫秒）
	SchedulableTasks(ctx context.Context, preemptedTimeoutMs int64, limit int) ([]domain.Task, error)
	// Acquire 抢占任务
	Acquire(ctx context.Context, id int64, scheduleNodeID string) (domain.Task, error)
	// Release 释放任务
	Release(ctx context.Context, id int64, scheduleNodeID string) (domain.Task, error)
	// Renew 续约所有抢占到的任务
	Renew(ctx context.Context, scheduleNodeID string) error
	// UpdateNextTime 更新任务的下次执行时间
	UpdateNextTime(ctx context.Context, task domain.Task) (domain.Task, error)
	// GetByID 根据ID获取task
	GetByID(ctx context.Context, id int64) (domain.Task, error)
}

type service struct {
	repo repository.TaskRepository
}

// NewService 创建任务服务实例
func NewService(repo repository.TaskRepository) Service {
	return &service{
		repo: repo,
	}
}

func (s *service) Create(ctx context.Context, task domain.Task) (domain.Task, error) {
	// 计算并设置下次执行时间
	nextTime, err := task.CalculateNextTime()
	if err != nil {
		return domain.Task{}, fmt.Errorf("%w: %w", errs.ErrInvalidTaskCronExpr, err)
	}
	if nextTime.IsZero() {
		return domain.Task{}, errs.ErrInvalidTaskCronExpr
	}
	task.NextTime = nextTime.UnixMilli()
	return s.repo.Create(ctx, task)
}

func (s *service) SchedulableTasks(ctx context.Context, preemptedTimeoutMs int64, limit int) ([]domain.Task, error) {
	return s.repo.SchedulableTasks(ctx, preemptedTimeoutMs, limit)
}

func (s *service) Acquire(ctx context.Context, id int64, scheduleNodeID string) (domain.Task, error) {
	if scheduleNodeID == "" {
		return domain.Task{}, errs.ErrInvalidTaskScheduleNodeID
	}
	return s.repo.Acquire(ctx, id, scheduleNodeID)
}

func (s *service) Release(ctx context.Context, id int64, scheduleNodeID string) (domain.Task, error) {
	if scheduleNodeID == "" {
		return domain.Task{}, errs.ErrInvalidTaskScheduleNodeID
	}
	return s.repo.Release(ctx, id, scheduleNodeID)
}

func (s *service) Renew(ctx context.Context, scheduleNodeID string) error {
	if scheduleNodeID == "" {
		return errs.ErrInvalidTaskScheduleNodeID
	}
	return s.repo.Renew(ctx, scheduleNodeID)
}

func (s *service) UpdateNextTime(ctx context.Context, task domain.Task) (domain.Task, error) {
	// 计算并设置下次执行时间
	nextTime, err := task.CalculateNextTime()
	if err != nil {
		return domain.Task{}, fmt.Errorf("%w: %w", errs.ErrInvalidTaskCronExpr, err)
	}
	if nextTime.IsZero() {
		// 不需要继续执行了
		return task, nil
	}
	task.NextTime = nextTime.UnixMilli()
	return s.repo.UpdateNextTime(ctx, task.ID, task.Version, task.NextTime)
}

func (s *service) GetByID(ctx context.Context, id int64) (domain.Task, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *service) UpdateScheduleParams(ctx context.Context, task domain.Task, params map[string]string) (domain.Task, error) {
	task.UpdateScheduleParams(params)
	return s.repo.UpdateScheduleParams(ctx, task.ID, task.Version, task.ScheduleParams)
}
