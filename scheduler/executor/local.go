package executor

import (
	"context"
	"fmt"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
)

var _ Executor = &LocalExecutor{}

type LocalExecuteFunc func(ctx context.Context, execution domain.TaskExecution) (domain.ExecutionState, error)

// LocalExecutor 本地执行器
type LocalExecutor struct {
	fns map[string]LocalExecuteFunc
}

func NewLocalExecutor(fns map[string]LocalExecuteFunc) *LocalExecutor {
	return &LocalExecutor{fns: fns}
}

func (l *LocalExecutor) Name() string {
	return "LOCAL"
}

func (l *LocalExecutor) Run(ctx context.Context, execution domain.TaskExecution) (domain.ExecutionState, error) {
	fn, ok := l.fns[execution.Task.Name]
	if !ok {
		return domain.ExecutionState{}, fmt.Errorf("未注册方法：%s", execution.Task.Name)
	}
	// 直接执行本地函数
	return fn(ctx, execution)
}
