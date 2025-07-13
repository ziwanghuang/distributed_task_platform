package ioc

import (
	"time"

	"gitee.com/flycash/distributed_task_platform/internal/event"
	"gitee.com/flycash/distributed_task_platform/internal/service/acquirer"
	"gitee.com/flycash/distributed_task_platform/internal/service/invoker"
	"gitee.com/flycash/distributed_task_platform/internal/service/runner"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
)

func InitPlanRunner(
	planSvc task.PlanService,
	singerRunner *runner.SingleTaskRunner,
) *runner.PlanRunner {
	return runner.NewPlanRunner(planSvc, singerRunner)
}

func InitDispatchRunner(nodeID string,
	singerRunner *runner.SingleTaskRunner,
	planRunner *runner.PlanRunner,
) runner.Runner {
	return runner.NewDispatcherRunner(planRunner, singerRunner)
}

func InitSingleRunner(nodeID string,
	taskSvc task.Service,
	execSvc task.ExecutionService,
	acquirer acquirer.TaskAcquirer,
	invoker invoker.Invoker,
	producer event.CompleteProducer,
) *runner.SingleTaskRunner {
	return runner.NewSingleTaskRunner(
		nodeID,
		taskSvc,
		execSvc,
		acquirer,
		invoker,
		producer,
		3*time.Second,
	)
}

func NewExecutors(execFunc map[string]invoker.LocalExecuteFunc) *invoker.LocalInvoker {
	return invoker.NewLocalExecutor(execFunc)
}
