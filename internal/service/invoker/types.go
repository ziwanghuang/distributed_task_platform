package invoker

import (
	"context"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
)

type Invoker interface {
	Name() string
	// Run 执行任务，返回执行结果
	Run(ctx context.Context, execution domain.TaskExecution) (domain.ExecutionState, error)
}

type InvokerV2 interface {
	Name() string
	// Run 执行任务，返回执行结果， 透传的调度信息 scheduleCtx
	Run(ctx context.Context, execution domain.TaskExecution, scheduleCtx map[string]string) (domain.ExecutionState, error)
}
