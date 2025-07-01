package job

import (
	"context"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
)

var _ Job = &LocalJob{}

// LocalJob 代表一个正在本地执行的任务实例
type LocalJob struct{}

func (l *LocalJob) Type() string {
	return domain.TaskExecutorTypeLocal.String()
}

func (l *LocalJob) Run(ctx context.Context, task domain.Task) error {
	// TODO implement me
	panic("implement me")
}

func (l *LocalJob) HandleReport(ctx context.Context, progress *domain.Report) error {
	// TODO implement me
	panic("implement me")
}

func (l *LocalJob) HandleRenewFailure(ctx context.Context) error {
	// TODO implement me
	panic("implement me")
}

func (l *LocalJob) Stop(ctx context.Context) error {
	// TODO implement me
	panic("implement me")
}
