package ioc

import (
	executorv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/executor/v1"
	"gitee.com/flycash/distributed_task_platform/internal/compensator"
	"gitee.com/flycash/distributed_task_platform/internal/service/acquirer"
	"gitee.com/flycash/distributed_task_platform/internal/service/runner"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"gitee.com/flycash/distributed_task_platform/pkg/grpc"
	"github.com/gotomicro/ego/core/econf"
)

// InitRetryCompensator 初始化重试补偿器。
// 重试补偿器负责扫描处于 FAILED_RETRYABLE 状态的执行记录，
// 按照配置的重试策略（最大重试次数、批次大小、最小间隔）重新调度执行。
// 配置从 config.yaml 的 compensator.retry 段读取。
func InitRetryCompensator(
	runner runner.Runner,
	execSvc task.ExecutionService,
) *compensator.RetryCompensator {
	var cfg compensator.RetryConfig
	err := econf.UnmarshalKey("compensator.retry", &cfg)
	if err != nil {
		panic(err)
	}
	return compensator.NewRetryCompensator(
		runner,
		execSvc,
		cfg,
	)
}

// InitRescheduleCompensator 初始化重调度补偿器。
// 重调度补偿器负责扫描处于 FAILED_RESCHEDULED 状态的执行记录，
// 创建新的执行实例并重新调度到其他节点执行。
// 适用于执行节点主动请求重调度的场景（如节点资源不足）。
func InitRescheduleCompensator(
	runner runner.Runner,
	execSvc task.ExecutionService,
) *compensator.RescheduleCompensator {
	var cfg compensator.RescheduleConfig
	err := econf.UnmarshalKey("compensator.reschedule", &cfg)
	if err != nil {
		panic(err)
	}
	return compensator.NewRescheduleCompensator(
		runner,
		execSvc,
		cfg)
}

// InitShardingCompensator 初始化分片补偿器。
// 分片补偿器负责检查分片任务（ShardingParentID=0 的父任务）的所有子分片是否全部完成，
// 如果全部完成则汇总结果并更新父任务的状态。
// 参数:
//   - nodeID: 当前调度节点 ID，用于过滤仅处理本节点抢占的任务
//   - taskSvc: 任务服务，用于查询任务信息
//   - execSvc: 执行服务，用于查询和更新执行状态
//   - taskAcquirer: 任务获取器，用于 CAS 抢占
func InitShardingCompensator(
	nodeID string,
	taskSvc task.Service,
	execSvc task.ExecutionService,
	taskAcquirer acquirer.TaskAcquirer,
) *compensator.ShardingCompensator {
	var cfg compensator.ShardingConfig
	err := econf.UnmarshalKey("compensator.sharding", &cfg)
	if err != nil {
		panic(err)
	}
	return compensator.NewShardingCompensator(
		nodeID,
		taskSvc,
		execSvc,
		taskAcquirer,
		cfg)
}

// InitInterruptCompensator 初始化中断补偿器。
// 中断补偿器负责检测超时的执行记录（超过 deadline 仍处于 RUNNING 状态），
// 通过 gRPC 调用执行节点的 Interrupt 接口通知其停止执行，
// 然后将执行状态更新为失败或可重试状态。
func InitInterruptCompensator(
	grpcClients *grpc.ClientsV2[executorv1.ExecutorServiceClient],
	execSvc task.ExecutionService,
) *compensator.InterruptCompensator {
	var cfg compensator.InterruptConfig
	err := econf.UnmarshalKey("compensator.interrupt", &cfg)
	if err != nil {
		panic(err)
	}
	return compensator.NewInterruptCompensator(
		grpcClients,
		execSvc,
		cfg,
	)
}
