package ioc

import (
	"context"

	"github.com/gotomicro/ego/server/egrpc"
)

type Task interface {
	Start(ctx context.Context)
}

type App struct {
	GrpcServer *egrpc.Component
	Tasks      []Task
}

func (a *App) StartTasks(ctx context.Context) {
	for _, t := range a.Tasks {
		go func(t Task) {
			t.Start(ctx)
		}(t)
	}
}
