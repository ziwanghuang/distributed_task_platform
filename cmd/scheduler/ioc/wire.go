//go:build wireinject

package ioc

import (
	grpcapi "gitee.com/flycash/distributed_task_platform/internal/grpc"
	"gitee.com/flycash/distributed_task_platform/internal/repository"
	"gitee.com/flycash/distributed_task_platform/internal/repository/dao"
	tasksvc "gitee.com/flycash/distributed_task_platform/internal/service/task"
	"gitee.com/flycash/distributed_task_platform/ioc"
	"github.com/google/wire"
)

var (
	BaseSet = wire.NewSet(
		ioc.InitDB,
		ioc.InitDistributedLock,
		ioc.InitEtcdClient,
		ioc.InitMQ,
		ioc.InitRunner,
		ioc.InitInvoker,
		ioc.InitRegistry,
		ioc.InitPrometheusClient,
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
		ioc.InitNodeID,
		ioc.InitClusterLoadChecker,
		ioc.InitScheduler,
		ioc.InitMySQLTaskAcquirer,
		ioc.InitExecutorServiceGRPCClients,
	)

	compensatorSet = wire.NewSet(
		ioc.InitRetryCompensator,
		ioc.InitRescheduleCompensator,
		ioc.InitShardingCompensator,
		ioc.InitInterruptCompensator,
	)

	producerSet = wire.NewSet(
		ioc.InitCompleteProducer,
	)

	consumerSet = wire.NewSet(
		ioc.InitExecutionReportEventConsumer,
		ioc.InitExecutionBatchReportEventConsumer,
	)
)

func InitSchedulerApp() *ioc.SchedulerApp {
	wire.Build(
		// 基础设施
		BaseSet,

		taskSet,
		taskExecutionSet,
		planSet,
		schedulerSet,
		compensatorSet,
		consumerSet,
		producerSet,

		// GRPC服务器
		grpcapi.NewReporterServer,
		ioc.InitSchedulerNodeGRPCServer,
		ioc.InitTasks,
		wire.Struct(new(ioc.SchedulerApp), "*"),
	)

	return new(ioc.SchedulerApp)
}
