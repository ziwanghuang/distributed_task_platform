package scheduler

import (
	"context"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
)

var _ TaskAcquirer = &MySQLTaskAcquirer{}

// TaskAcquirer 任务抢占接口
type TaskAcquirer interface {
	// Acquire 抢占指定任务
	Acquire(ctx context.Context, task domain.Task) error
	// Release 释放指定任务
	Release(ctx context.Context, task domain.Task) error
	// Renew 续约指定任务
	Renew(ctx context.Context, task domain.Task) error
}

// MySQLTaskAcquirer 基于MySQL实现的TaskAcquirer
type MySQLTaskAcquirer struct {
	taskSvc task.Service // 依赖task.Service接口
}

// NewTaskAcquirer 创建TaskAcquirer实例
func NewTaskAcquirer(taskSvc task.Service) *MySQLTaskAcquirer {
	return &MySQLTaskAcquirer{
		taskSvc: taskSvc,
	}
}

// Acquire 抢占指定任务
func (t *MySQLTaskAcquirer) Acquire(ctx context.Context, task domain.Task) error {
	return t.taskSvc.Acquire(ctx, task)
}

// Release 释放指定任务
func (t *MySQLTaskAcquirer) Release(ctx context.Context, task domain.Task) error {
	return t.taskSvc.Release(ctx, task)
}

// Renew 续约指定任务
func (t *MySQLTaskAcquirer) Renew(ctx context.Context, task domain.Task) error {
	return t.taskSvc.Renew(ctx, task)
}
