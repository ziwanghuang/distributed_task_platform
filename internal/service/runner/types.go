package runner

import (
	"context"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
)

var _ Runner = &Dispatcher{}

type Runner interface {
	// Run 运行任务
	Run(ctx context.Context, task domain.Task) error
	// Retry 重试任务的一次执行
	Retry(ctx context.Context, execution domain.TaskExecution) error
	// Reschedule 重调度任务的一次执行
	Reschedule(ctx context.Context, execution domain.TaskExecution) error
}

// TaskExecutionStateHandler 任务执行状态处理器，使用前必须成功抢占 state.TaskID 对应的记录，内部会释放 result.TaskID 对应的 Task
type TaskExecutionStateHandler func(ctx context.Context, state domain.ExecutionState) error
