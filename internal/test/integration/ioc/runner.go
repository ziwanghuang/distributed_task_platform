package ioc

import (
	"gitee.com/flycash/distributed_task_platform/internal/event"
	"gitee.com/flycash/distributed_task_platform/internal/service/acquirer"
	"gitee.com/flycash/distributed_task_platform/internal/service/invoker"
	"gitee.com/flycash/distributed_task_platform/internal/service/runner"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
)

func InitPlanTaskRunner(
	planSvc task.PlanService,
	singerRunner *runner.NormalTaskRunner,
) *runner.PlanTaskRunner {
	return runner.NewPlanRunner(planSvc, singerRunner)
}

func InitDispatcherRunner(
	singerRunner *runner.NormalTaskRunner,
	planRunner *runner.PlanTaskRunner,
) runner.Runner {
	return runner.NewDispatcherRunner(planRunner, singerRunner)
}

func InitNormalTaskRunner(nodeID string,
	taskSvc task.Service,
	execSvc task.ExecutionService,
	acquirer acquirer.TaskAcquirer,
	invoker invoker.Invoker,
	producer event.CompleteProducer,
) *runner.NormalTaskRunner {
	return runner.NewNormalTaskRunner(
		nodeID,
		taskSvc,
		execSvc,
		acquirer,
		invoker,
		producer,
	)
}
