//go:build wireinject

package ioc

import (
	"context"

	"gitee.com/flycash/distributed_task_platform/internal/service/invoker"

	"gitee.com/flycash/distributed_task_platform/internal/repository"
	"gitee.com/flycash/distributed_task_platform/internal/repository/dao"
	"gitee.com/flycash/distributed_task_platform/internal/service/scheduler"
	tasksvc "gitee.com/flycash/distributed_task_platform/internal/service/task"
	"gitee.com/flycash/distributed_task_platform/internal/test/ioc"
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
		InitRegistry,
	)

	taskSet = wire.NewSet(
		dao.NewGORMTaskDAO,
		repository.NewTaskRepository,
		tasksvc.NewService,
	)

	taskExecutionSet = wire.NewSet(
		dao.NewGORMTaskExecutionDAO,
		repository.NewTaskExecutionRepository,
		InitCompleteProducer,
		tasksvc.NewExecutionService,
	)

	planSet = wire.NewSet(
		tasksvc.NewPlanService,
	)

	schedulerSet = wire.NewSet(
		NewExecutors,
		InitMySQLTaskAcquirer,
		initDispatcherExecutor,
		InitSingleRunner,
		InitPlanRunner,
		InitDispatchRunner,
		InitScheduler,
	)

	compensatorSet = wire.NewSet(
		InitRetryCompensator,
	)
)

func initDispatcherExecutor(localExecutor *invoker.LocalInvoker) invoker.Invoker {
	return invoker.NewDispatcher(nil, nil, localExecutor)
}

type Task interface {
	Start(ctx context.Context)
}

type SchedulerApp struct {
	Scheduler    *scheduler.Scheduler
	TaskSvc      tasksvc.Service
	ExecutionSvc tasksvc.ExecutionService
	Consumer     *CompleteConsumer
	Tasks        []Task
}

func (a *SchedulerApp) StartTasks(ctx context.Context) {
	for _, t := range a.Tasks {
		go func(t Task) {
			t.Start(ctx)
		}(t)
	}
}

func InitSchedulerApp(execFunc map[string]invoker.LocalExecuteFunc) *SchedulerApp {
	wire.Build(
		// 基础设施
		BaseSet,

		taskSet,
		taskExecutionSet,
		planSet,
		schedulerSet,
		compensatorSet,
		InitTasks,
		InitCompleteConsumer,
		wire.Struct(new(SchedulerApp), "*"),
	)

	return new(SchedulerApp)
}
