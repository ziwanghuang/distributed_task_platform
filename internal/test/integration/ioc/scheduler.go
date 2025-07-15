package ioc

import (
	"time"

	executorv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/executor/v1"
	"gitee.com/flycash/distributed_task_platform/internal/service/acquirer"
	"gitee.com/flycash/distributed_task_platform/internal/service/runner"
	"gitee.com/flycash/distributed_task_platform/internal/service/scheduler"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"gitee.com/flycash/distributed_task_platform/pkg/grpc"
	"github.com/gotomicro/ego/client/egrpc"
	"github.com/pborman/uuid"
)

func InitNodeID() string {
	return uuid.New()
}

func InitScheduler(
	nodeID string,
	execRunner runner.Runner,
	taskSvc task.Service,
	execSvc task.ExecutionService,
	acquirer acquirer.TaskAcquirer,
) *scheduler.Scheduler {
	const batchTimeout = 30 * time.Second
	const batchSize = 10
	const preemptedTimeout = 10 * time.Second
	const scheduleInterval = 10 * time.Second
	const renewInterval = 3 * time.Second
	conf := scheduler.Config{
		BatchTimeout:     batchTimeout,
		BatchSize:        batchSize,
		PreemptedTimeout: preemptedTimeout,
		ScheduleInterval: scheduleInterval,
		RenewInterval:    renewInterval,
	}
	grpcClients := grpc.NewClients(func(conn *egrpc.Component) executorv1.ExecutorServiceClient {
		return executorv1.NewExecutorServiceClient(conn)
	})
	return scheduler.NewScheduler(
		nodeID,
		execRunner,
		taskSvc,
		execSvc,
		acquirer,
		grpcClients,
		conf,
	)
}
