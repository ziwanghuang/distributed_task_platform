package job

import (
	"context"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
)

type Job interface {
	Name() string
	// Run 启动任务执行，所有错误（包括启动错误和执行错误）都通过 Chans.Error 通道传递
	Run(ctx context.Context, task domain.Task) *Chans
}

type Chans struct {
	Report chan<- *domain.Report
	Renew  chan<- bool
	Error  <-chan error // 任务执行错误通道
}
