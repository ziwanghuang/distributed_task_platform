package ioc

import (
	executorv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/executor/v1"
	"gitee.com/flycash/distributed_task_platform/internal/service/acquirer"
	"gitee.com/flycash/distributed_task_platform/internal/service/picker"
	"gitee.com/flycash/distributed_task_platform/internal/service/runner"
	"gitee.com/flycash/distributed_task_platform/internal/service/scheduler"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"gitee.com/flycash/distributed_task_platform/pkg/grpc"
	"gitee.com/flycash/distributed_task_platform/pkg/loadchecker"
	"gitee.com/flycash/distributed_task_platform/pkg/prometheus"
	"github.com/google/uuid"
	"github.com/gotomicro/ego/core/econf"
)

// InitNodeID 生成当前调度节点的唯一标识。
// 使用 UUID v4 确保全局唯一，用于：
//   - 任务抢占时标识归属节点
//   - 续约时识别当前节点的任务
//   - 服务注册时标识调度节点实例
//
// 注意：每次重启会生成新的 NodeID，之前节点的任务抢占记录会在超时后被其他节点接管。
func InitNodeID() string {
	return uuid.New().String()
}

// InitScheduler 初始化调度器实例，组装所有依赖组件。
// 从配置文件中读取调度器配置（批次大小、超时、间隔等），
// 并初始化 Prometheus 指标收集器。
func InitScheduler(
	nodeID string,
	runner runner.Runner,
	taskSvc task.Service,
	execSvc task.ExecutionService,
	acquirer acquirer.TaskAcquirer,
	grpcClients *grpc.ClientsV2[executorv1.ExecutorServiceClient],
	lc *loadchecker.ClusterLoadChecker,
	nodePicker picker.ExecutorNodePicker,
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
		lc,
		// 初始化指标收集器
		prometheus.NewSchedulerMetrics(nodeID),
		nodePicker,
	)
}
