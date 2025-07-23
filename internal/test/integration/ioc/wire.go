//go:build wireinject

package ioc

import (
	"context"

	executorv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/executor/v1"
	"gitee.com/flycash/distributed_task_platform/internal/event/reportevt"
	"gitee.com/flycash/distributed_task_platform/internal/service/invoker"
	"gitee.com/flycash/distributed_task_platform/internal/service/runner"
	"gitee.com/flycash/distributed_task_platform/pkg/grpc"

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
		InitRegistry,
		InitPrometheusClient,
		InitExecutorServiceGRPCClients,
		InitCompleteProducer,
		InitShardingRuleScheduleParamBuilder,
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
		InitInvoker,
		InitNormalTaskRunner,
		InitPlanTaskRunner,
		InitDispatcherRunner,
		InitClusterLoadChecker,
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
	Scheduler             *scheduler.Scheduler
	Runner                runner.Runner
	TaskSvc               tasksvc.Service
	ExecutionSvc          tasksvc.ExecutionService
	CompleteEventConsumer *CompleteConsumer
	ReportEventConsumer   *reportevt.ReportEventConsumer
	Clients               *grpc.ClientsV2[executorv1.ExecutorServiceClient]
	Tasks                 []Task
}

func (a *SchedulerApp) StartTasks(ctx context.Context) {
	for _, t := range a.Tasks {
		go func(t Task) {
			t.Start(ctx)
		}(t)
	}
}

func InitSchedulerApp(executeFuncs map[string]invoker.LocalExecuteFunc, prepareFuncs map[string]invoker.LocalPrepareFunc) *SchedulerApp {
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
		InitExecutionReportEventConsumer,
		wire.Struct(new(SchedulerApp), "*"),
	)

	return new(SchedulerApp)
}
