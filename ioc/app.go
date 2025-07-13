package ioc

import (
	"context"
	"gitee.com/flycash/distributed_task_platform/internal/service/scheduler"

	"github.com/gotomicro/ego/server/egrpc"
)

type Task interface {
	Start(ctx context.Context)
}

type SchedulerApp struct {
	GRPC      *egrpc.Component
	Scheduler *scheduler.Scheduler
	Tasks     []Task
}

func (a *SchedulerApp) StartTasks(ctx context.Context) {
	for _, t := range a.Tasks {
		go func(t Task) {
			t.Start(ctx)
		}(t)
	}
}
