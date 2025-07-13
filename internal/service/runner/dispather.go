package runner

import (
	"context"
	"fmt"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
)

var _ Runner = &Dispatcher{}

// Dispatcher
type Dispatcher struct {
	planRunner       Runner
	singleTaskRunner Runner
}

func NewDispatcherRunner(planRunner, singleRunner Runner) Runner {
	return &Dispatcher{
		planRunner:       planRunner,
		singleTaskRunner: singleRunner,
	}
}

func (d *Dispatcher) Run(ctx context.Context, task domain.Task) error {
	switch task.Type {
	case domain.NormalTaskType:
		return d.singleTaskRunner.Run(ctx, task)
	case domain.PlanTaskType:
		return d.planRunner.Run(ctx, task)
	default:
		return fmt.Errorf("unknown task type: %s", task.Type)
	}
}

// Retry plan暂时不会触发Retry逻辑
func (d *Dispatcher) Retry(ctx context.Context, execution domain.TaskExecution) error {
	return d.singleTaskRunner.Retry(ctx, execution)
}

func (d *Dispatcher) Reschedule(ctx context.Context, execution domain.TaskExecution) error {
	return d.singleTaskRunner.Reschedule(ctx, execution)
}
