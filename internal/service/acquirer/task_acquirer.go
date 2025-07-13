package acquirer

import (
	"context"

	"gitee.com/flycash/distributed_task_platform/internal/repository"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
)

var _ TaskAcquirer = &MySQLTaskAcquirer{}

// TaskAcquirer 任务抢占接口
type TaskAcquirer interface {
	// Acquire 抢占指定任务
	Acquire(ctx context.Context, taskID int64, scheduleNodeID string) (domain.Task, error)
	// Release 释放指定任务
	Release(ctx context.Context, taskID int64, scheduleNodeID string) error
	// Renew 续约所有抢占到的任务
	Renew(ctx context.Context, scheduleNodeID string) error
}

// MySQLTaskAcquirer 基于MySQL实现的TaskAcquirer
type MySQLTaskAcquirer struct {
	taskRepo repository.TaskRepository // 依赖task.Service接口
}

// NewTaskAcquirer 创建TaskAcquirer实例
func NewTaskAcquirer(taskSvc repository.TaskRepository) *MySQLTaskAcquirer {
	return &MySQLTaskAcquirer{
		taskRepo: taskSvc,
	}
}

// Acquire 抢占指定任务，返回抢占后的任务信息
func (t *MySQLTaskAcquirer) Acquire(ctx context.Context, taskID int64, scheduleNodeID string) (domain.Task, error) {
	tk, err := t.taskRepo.Acquire(ctx, taskID, scheduleNodeID)
	if err != nil {
		return domain.Task{}, err
	}
	return tk, nil
}

// Release 释放指定任务
func (t *MySQLTaskAcquirer) Release(ctx context.Context, taskID int64, scheduleNodeID string) error {
	_, err := t.taskRepo.Release(ctx, taskID, scheduleNodeID)
	return err
}

// Renew 续约指定任务，返回续约后的任务信息
func (t *MySQLTaskAcquirer) Renew(ctx context.Context, scheduleNodeID string) error {
	return t.taskRepo.Renew(ctx, scheduleNodeID)
}
