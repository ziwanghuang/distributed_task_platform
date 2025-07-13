//go:build wireinject

package ioc

import (
	"gitee.com/flycash/distributed_task_platform/internal/repository"
	"gitee.com/flycash/distributed_task_platform/internal/repository/dao"
	grpcapi "gitee.com/flycash/distributed_task_platform/internal/service/scheduler/grpc"
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
		ioc.InitConsumers,
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

	schedulerSet = wire.NewSet(
		ioc.InitNodeID,
		ioc.InitScheduler,
		ioc.InitExecutors,
		ioc.InitLocalExecuteFuncs,
		ioc.InitMySQLTaskAcquirer,
		ioc.InitExecutorServiceGRPCClients,
	)
	compensatorSet = wire.NewSet(
		ioc.InitRetryCompensator,
	)
)

func InitSchedulerApp() *ioc.SchedulerApp {
	wire.Build(
		// 基础设施
		BaseSet,

		taskSet,
		taskExecutionSet,
		schedulerSet,
		compensatorSet,

		// GRPC服务器
		grpcapi.NewReporterServer,
		ioc.InitSchedulerNodeGRPCServer,
		ioc.InitTasks,
		wire.Struct(new(ioc.SchedulerApp), "*"),
	)

	return new(ioc.SchedulerApp)
}
