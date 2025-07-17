package runner

import (
	"context"
	"fmt"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
)

var _ Runner = &Dispatcher{}

type Dispatcher struct {
	planTaskRunner   Runner
	normalTaskRunner Runner
}

func NewDispatcherRunner(planTaskRunner, normalTaskRunner Runner) Runner {
	return &Dispatcher{
		planTaskRunner:   planTaskRunner,
		normalTaskRunner: normalTaskRunner,
	}
}

func (d *Dispatcher) Run(ctx context.Context, task domain.Task) error {
	switch task.Type {
	case domain.NormalTaskType:
		return d.normalTaskRunner.Run(ctx, task)
	case domain.PlanTaskType:
		return d.planTaskRunner.Run(ctx, task)
	default:
		return fmt.Errorf("unknown task type: %s", task.Type)
	}
}

// Retry plan暂时不会触发Retry逻辑
func (d *Dispatcher) Retry(ctx context.Context, execution domain.TaskExecution) error {
	return d.normalTaskRunner.Retry(ctx, execution)
}

func (d *Dispatcher) Reschedule(ctx context.Context, execution domain.TaskExecution) error {
	return d.normalTaskRunner.Reschedule(ctx, execution)
}
