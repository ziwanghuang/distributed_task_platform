package runner

import (
	"context"
	"fmt"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
)

var _ Runner = &Dispatcher{}

// Dispatcher 任务执行分发器，是 Runner 接口的路由层实现。
// 根据任务类型（Normal/Plan）将执行请求分发到对应的 Runner。
// Scheduler 持有的 Runner 实际上就是这个 Dispatcher。
type Dispatcher struct {
	planTaskRunner   Runner // DAG 工作流任务执行器
	normalTaskRunner Runner // 普通任务（含分片任务）执行器
}

// NewDispatcherRunner 创建 Dispatcher 实例
func NewDispatcherRunner(planTaskRunner, normalTaskRunner Runner) Runner {
	return &Dispatcher{
		planTaskRunner:   planTaskRunner,
		normalTaskRunner: normalTaskRunner,
	}
}

// Run 根据任务类型路由到对应的执行器
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
