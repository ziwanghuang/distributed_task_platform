package v2

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

func (l *LocalJob) Name() string {
	return domain.TaskExecutorTypeLocal.String()
}

func (l *LocalJob) Run(ctx context.Context, task domain.Task) (*Chans, error) {
	fn, ok := l.fns[task.Name]
	if !ok {
		return nil, fmt.Errorf("未注册方法：%s", task.Name)
	}
	return nil, fn(ctx, task)
}
