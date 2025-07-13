//go:build wireinject

package ioc

import (
	"context"

	"gitee.com/flycash/distributed_task_platform/internal/repository"
	"gitee.com/flycash/distributed_task_platform/internal/repository/dao"
	tasksvc "gitee.com/flycash/distributed_task_platform/internal/service/task"
	"gitee.com/flycash/distributed_task_platform/internal/test/ioc"
	"gitee.com/flycash/distributed_task_platform/pkg/executor"
	"gitee.com/flycash/distributed_task_platform/scheduler"
	"github.com/google/wire"
)

var (
	BaseSet = wire.NewSet(
		ioc.InitDBAndTables,
		ioc.InitDistributedLock,
		ioc.InitEtcdClient,
		ioc.InitMQ,
		InitNodeID,
		InitConsumers,
	)

	taskSet = wire.NewSet(
		dao.NewGORMTaskDAO,
		repository.NewTaskRepository,
		tasksvc.NewService,
	)

	taskExecutionSet = wire.NewSet(
		dao.NewGORMTaskExecutionDAO,
		repository.NewTaskExecutionRepository,
		tasksvc.NewExecutionService,
	)

	planSet = wire.NewSet(
		tasksvc.NewPlanService,
	)

	schedulerSet = wire.NewSet(
		NewExecutors,
		InitMySQLTaskAcquirer,
		InitCompleteProducer,
		InitSingleRunner,
		InitPlanRunner,
		InitDispatchRunner,
		InitScheduler,
	)

	compensatorSet = wire.NewSet(
		InitRetryCompensator,
	)
)

type Task interface {
	Start(ctx context.Context)
}

type SchedulerApp struct {
	Scheduler    *scheduler.Scheduler
	TaskSvc      tasksvc.Service
	ExecutionSvc tasksvc.ExecutionService
	Tasks        []Task
}

func (a *SchedulerApp) StartTasks(ctx context.Context) {
	for _, t := range a.Tasks {
		go func(t Task) {
			t.Start(ctx)
		}(t)
	}
}

func InitSchedulerApp(execFunc map[string]executor.LocalExecuteFunc) *SchedulerApp {
	wire.Build(
		// 基础设施
		BaseSet,

		taskSet,
		taskExecutionSet,
		planSet,
		schedulerSet,
		compensatorSet,
		InitTasks,
		wire.Struct(new(SchedulerApp), "*"),
	)

	return new(SchedulerApp)
}
