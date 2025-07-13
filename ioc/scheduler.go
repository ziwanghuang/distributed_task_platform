package ioc

import (
	executorv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/executor/v1"
	"gitee.com/flycash/distributed_task_platform/internal/service/acquirer"
	"gitee.com/flycash/distributed_task_platform/internal/service/scheduler"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"gitee.com/flycash/distributed_task_platform/pkg/grpc"
	"gitee.com/flycash/distributed_task_platform/pkg/mqx"
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
	acquirer acquirer.TaskAcquirer,
	executors map[string]callser.Executor,
	consumers map[string]*mqx.Consumer,
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
	fns map[string]callser.LocalExecuteFunc,
) map[string]callser.Executor {
	remoteExecutor := callser.NewRemoteExecutor(grpcClients)
	localExecutor := callser.NewLocalExecutor(fns)
	return map[string]callser.Executor{
		localExecutor.Name():  localExecutor,
		remoteExecutor.Name(): remoteExecutor,
	}
}

func InitLocalExecuteFuncs() map[string]callser.LocalExecuteFunc {
	return make(map[string]callser.LocalExecuteFunc)
}
