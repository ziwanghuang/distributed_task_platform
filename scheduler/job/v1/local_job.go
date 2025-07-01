package v1

import (
	"context"
	"fmt"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
)

var _ Job = &LocalJob{}

// LocalJob 代表一个正在本地执行的任务实例
type LocalJob struct {
	fns map[string]func(ctx context.Context, task domain.Task) error
}

func (l *LocalJob) Type() string {
	return domain.TaskExecutorTypeLocal.String()
}

func (l *LocalJob) Run(ctx context.Context, task domain.Task) error {
	fn, ok := l.fns[task.Name]
	if !ok {
		return fmt.Errorf("未注册方法：%s", task.Name)
	}
	return fn(ctx, task)
}

func (l *LocalJob) HandleReport(ctx context.Context, _ *domain.Report) error {
	return l.doNothing(ctx)
}

func (l *LocalJob) HandleRenewFailure(ctx context.Context) error {
	return l.doNothing(ctx)
}

func (l *LocalJob) doNothing(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return nil
	default:
		return nil
	}
}

func (l *LocalJob) Stop(ctx context.Context) error {
	return l.doNothing(ctx)
}
