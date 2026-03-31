package ioc

import (
	"gitee.com/flycash/distributed_task_platform/internal/event"
	"gitee.com/flycash/distributed_task_platform/internal/service/acquirer"
	"gitee.com/flycash/distributed_task_platform/internal/service/invoker"
	"gitee.com/flycash/distributed_task_platform/internal/service/runner"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
)

// InitRunner 初始化任务执行器（Runner）链路。
// 组装方式采用装饰器模式（Dispatcher → Plan/Normal）：
//   - NormalTaskRunner: 处理普通单次任务（抢占→创建执行→调用→上报完成）
//   - PlanRunner: 处理 DAG 工作流任务（解析依赖→按拓扑序执行）
//   - DispatcherRunner: 根据 TaskType（normal/plan）路由到对应的 Runner
//
// 这是调度器核心执行链路的入口。
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
