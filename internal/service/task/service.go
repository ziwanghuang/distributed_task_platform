package task

import (
	"context"

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
	Acquire(ctx context.Context, task domain.Task) error
	// Release 释放任务
	Release(ctx context.Context, task domain.Task) error
	// Renew 续约任务
	Renew(ctx context.Context, task domain.Task) error
	// UpdateNextTime 更新任务的下次执行时间
	UpdateNextTime(ctx context.Context, task domain.Task) error
}

type service struct {
	repo           repository.TaskRepository
	scheduleNodeId string // 当前调度节点ID
}

// NewService 创建任务服务实例
func NewService(repo repository.TaskRepository, scheduleNodeId string) Service {
	return &service{
		repo:           repo,
		scheduleNodeId: scheduleNodeId,
	}
}

func (s *service) Create(ctx context.Context, task domain.Task) (domain.Task, error) {
	return s.repo.Create(ctx, task)
}

func (s *service) SchedulableTasks(ctx context.Context, preemptedTimeoutMs int64, limit int) ([]domain.Task, error) {
	return s.repo.SchedulableTasks(ctx, preemptedTimeoutMs, limit)
}

func (s *service) Acquire(ctx context.Context, task domain.Task) error {
	if task.ScheduleNodeID == "" {
		return errs.ErrInvalidTaskScheduleNodeID
	}
	return s.repo.Acquire(ctx, task)
}

func (s *service) Release(ctx context.Context, task domain.Task) error {
	if task.ScheduleNodeID == "" {
		return errs.ErrInvalidTaskScheduleNodeID
	}
	return s.repo.Release(ctx, task)
}

func (s *service) Renew(ctx context.Context, task domain.Task) error {
	if task.ScheduleNodeID == "" {
		return errs.ErrInvalidTaskScheduleNodeID
	}
	return s.repo.Renew(ctx, task)
}

func (s *service) UpdateNextTime(ctx context.Context, task domain.Task) error {
	// 计算并设置下次执行时间
	nextTime := task.CalculateNextTime()
	if nextTime.IsZero() {
		// 不需要继续执行了
		return nil
	}
	task.NextTime = nextTime.UnixMilli()
	return s.repo.UpdateNextTime(ctx, task.ID, task.Version, task.NextTime)
}
