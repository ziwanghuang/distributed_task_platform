package job

import (
	"context"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
)

type Job interface {
	Name() string
	Run(ctx context.Context, task domain.Task) (*Chans, error)
}

type Chans struct {
	Report chan<- *domain.Report
	Renew  chan<- bool
}
