package ioc

import (
	"time"

	executorv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/executor/v1"
	"gitee.com/flycash/distributed_task_platform/internal/service/acquirer"
	"gitee.com/flycash/distributed_task_platform/internal/service/runner"
	"gitee.com/flycash/distributed_task_platform/internal/service/scheduler"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	grpcpkg "gitee.com/flycash/distributed_task_platform/pkg/grpc"
	registry "gitee.com/flycash/distributed_task_platform/pkg/grpc/registry/etcd"
	"gitee.com/flycash/distributed_task_platform/pkg/loadchecker"
	"gitee.com/flycash/distributed_task_platform/pkg/prometheus"
	"github.com/pborman/uuid"
	"google.golang.org/grpc"
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
	registry *registry.Registry,
	lc *loadchecker.ClusterLoadChecker,
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
	grpcClients := grpcpkg.NewClientsV2(registry, time.Second, func(conn *grpc.ClientConn) executorv1.ExecutorServiceClient {
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
		lc,
		prometheus.NewSchedulerMetrics(nodeID),
	)
}
