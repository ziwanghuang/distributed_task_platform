package runner

import (
	"context"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
)

type Runner interface {
	// Run 运行任务
	Run(ctx context.Context, task domain.Task) error
	// Retry 重试任务的一次执行
	Retry(ctx context.Context, execution domain.TaskExecution) error
}

type Dispatcher struct {
	*Base
}

func NewDispatcher(base *Base) *Dispatcher {
	return &Dispatcher{Base: base}
}

func (d *Dispatcher) Run(_ context.Context, _ domain.Task) error {
	// TODO implement me
	panic("implement me")
}

func (d *Dispatcher) Retry(_ context.Context, _ domain.TaskExecution) error {
	// TODO implement me
	panic("implement me")
}
