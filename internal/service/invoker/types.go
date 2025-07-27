package invoker

import (
	"context"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
)

type Invoker interface {
	Name() string
	// Run 执行任务，返回执行结果
	Run(ctx context.Context, execution domain.TaskExecution) (domain.ExecutionState, error)
	// Prepare 返回业务总数量
	Prepare(ctx context.Context, execution domain.TaskExecution) (map[string]string, error)
}
