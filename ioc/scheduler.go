package ioc

import (
	executorv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/executor/v1"
	"gitee.com/flycash/distributed_task_platform/internal/event"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"gitee.com/flycash/distributed_task_platform/pkg/grpc"
	"gitee.com/flycash/distributed_task_platform/scheduler"
	"gitee.com/flycash/distributed_task_platform/scheduler/executor"
	"github.com/google/uuid"
	"github.com/gotomicro/ego/core/econf"
)

func InitNodeID() string {
	return uuid.New().String()
}

func InitScheduler(
	nodeID string,
	taskSvc task.Service,
	execSvc task.ExecutionService,
	acquirer scheduler.TaskAcquirer,
	executors map[string]executor.Executor,
	consumers map[string]*event.Consumer,
	grpcClients *grpc.Clients[executorv1.ExecutorServiceClient],
) *scheduler.Scheduler {
	var cfg scheduler.Config
	err := econf.UnmarshalKey("scheduler", &cfg)
	if err != nil {
		panic(err)
	}
	return scheduler.NewScheduler(
		nodeID,
		taskSvc,
		execSvc,
		acquirer,
		executors,
		consumers,
		grpcClients,
		cfg,
	)
}

func InitExecutors(
	grpcClients *grpc.Clients[executorv1.ExecutorServiceClient],
	fns map[string]executor.LocalExecuteFunc,
) map[string]executor.Executor {
	remoteExecutor := executor.NewRemoteExecutor(grpcClients)
	localExecutor := executor.NewLocalExecutor(fns)
	return map[string]executor.Executor{
		localExecutor.Name():  localExecutor,
		remoteExecutor.Name(): remoteExecutor,
	}
}

func InitLocalExecuteFuncs() map[string]executor.LocalExecuteFunc {
	return make(map[string]executor.LocalExecuteFunc)
}
