package ioc

import (
	executorv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/executor/v1"
	"gitee.com/flycash/distributed_task_platform/internal/service/acquirer"
	"gitee.com/flycash/distributed_task_platform/internal/service/runner"
	"gitee.com/flycash/distributed_task_platform/internal/service/scheduler"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"gitee.com/flycash/distributed_task_platform/pkg/grpc"
	"github.com/google/uuid"
	"github.com/gotomicro/ego/core/econf"
)

func InitNodeID() string {
	return uuid.New().String()
}

func InitScheduler(
	nodeID string,
	runner runner.Runner,
	taskSvc task.Service,
	execSvc task.ExecutionService,
	acquirer acquirer.TaskAcquirer,
	grpcClients *grpc.ClientsV2[executorv1.ExecutorServiceClient],
) *scheduler.Scheduler {
	var cfg scheduler.Config
	err := econf.UnmarshalKey("scheduler", &cfg)
	if err != nil {
		panic(err)
	}
	return scheduler.NewScheduler(
		nodeID,
		runner,
		taskSvc,
		execSvc,
		acquirer,
		grpcClients,
		cfg,
	)
}
