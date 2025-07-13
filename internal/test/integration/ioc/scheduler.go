package ioc

import (
	executorv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/executor/v1"
	"gitee.com/flycash/distributed_task_platform/internal/event"
	"gitee.com/flycash/distributed_task_platform/internal/service/runner"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"gitee.com/flycash/distributed_task_platform/pkg/acquirer"
	"gitee.com/flycash/distributed_task_platform/pkg/grpc"
	"gitee.com/flycash/distributed_task_platform/scheduler"
	"github.com/ecodeclub/ekit/syncx"
	"github.com/gotomicro/ego/client/egrpc"
	"github.com/pborman/uuid"
	"time"
)
func InitNodeID() string {
	return uuid.New()
}
func InitScheduler(
	nodeID string,
	taskSvc task.Service,
	acquirer acquirer.TaskAcquirer,
	execRunner runner.Runner,
	consumers map[string]*event.Consumer,
) *scheduler.Scheduler {
	conf := scheduler.Config{
		BatchTimeout:     30 * time.Second,
		BatchSize:        10,
		PreemptedTimeout: 10 * time.Second,
		ScheduleInterval: 10 * time.Second,
		RenewInterval:    3 * time.Second,
	}
	grpcClients := grpc.NewClients(func(conn *egrpc.Component) executorv1.ExecutorServiceClient {
		return executorv1.NewExecutorServiceClient(conn)
	})
	return scheduler.NewScheduler(
		nodeID,
		&syncx.Map[int64, runner.TaskExecutionStateHandler]{},
		execRunner,
		taskSvc,
		acquirer,
		consumers,
		grpcClients,
		conf,
	)

}
