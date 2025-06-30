package task

import (
	"context"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
)

// Service 任务服务接口
type Service interface {
	// SchedulableTasks 获取可调度的任务列表
	SchedulableTasks(ctx context.Context, limit int) ([]domain.Task, error)

	// Acquire 抢占任务
	Acquire(ctx context.Context, task domain.Task) error

	// Release 释放任务
	Release(ctx context.Context, task domain.Task) error

	// Renew 续约任务
	Renew(ctx context.Context, task domain.Task) error
}
