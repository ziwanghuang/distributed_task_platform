package job

import (
	"context"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
)

type Job interface {
	Type() string
	Run(ctx context.Context, task domain.Task) error
	HandleReport(ctx context.Context, progress *domain.Report) error
	HandleRenewFailure(ctx context.Context) error
	Stop(ctx context.Context) error
}
