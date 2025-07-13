package ioc

import (
	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/event"
	"gitee.com/flycash/distributed_task_platform/internal/service/runner"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"gitee.com/flycash/distributed_task_platform/pkg/acquirer"
	"gitee.com/flycash/distributed_task_platform/pkg/executor"
	"github.com/ecodeclub/ekit/syncx"
	"github.com/gotomicro/ego/core/econf"
	"time"
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
	executors map[string]executor.Executor,
	producer event.CompleteProducer,
) *runner.SingleTaskRunner {
	return runner.NewSingleTaskRunner(
		nodeID,
		&syncx.Map[int64, runner.TaskExecutionStateHandler]{},
		taskSvc,
		execSvc,
		acquirer,
		executors,
		producer,
		3*time.Second,
	)
}

func NewExecutors(execFunc map[string] executor.LocalExecuteFunc) map[string]executor.Executor {
	econf.Set("executor.RemoteExecutor", "test")
	executors := map[string]executor.Executor{
		domain.TaskExecutionMethodLocal.String(): executor.NewLocalExecutor(execFunc),
	}
	return executors
}
