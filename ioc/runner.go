package ioc

import (
	"gitee.com/flycash/distributed_task_platform/internal/event"
	"gitee.com/flycash/distributed_task_platform/internal/service/acquirer"
	"gitee.com/flycash/distributed_task_platform/internal/service/invoker"
	"gitee.com/flycash/distributed_task_platform/internal/service/runner"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
)

func InitRunner(
	nodeID string,
	taskSvc task.Service,
	execSvc task.ExecutionService,
	planSvc task.PlanService,
	taskAcquirer acquirer.TaskAcquirer,
	invoker invoker.Invoker,
	producer event.CompleteProducer,
) runner.Runner {
	s := runner.NewNormalTaskRunner(
		nodeID,
		taskSvc,
		execSvc,
		taskAcquirer,
		invoker,
		producer,
	)
	return runner.NewDispatcherRunner(runner.NewPlanRunner(planSvc, s), s)
}
