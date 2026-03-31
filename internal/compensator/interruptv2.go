// Package compensator 提供任务执行异常场景下的自动补偿机制。
//
// 本包实现了四类补偿器（均有 V1 和 V2 两个版本）：
//   - InterruptCompensator: 中断超时任务，向执行节点发送中断信号
//   - RetryCompensator:     自动重试失败的任务执行
//   - RescheduleCompensator: 重新调度需要重调度的任务执行
//   - ShardingCompensator:  汇总分片子任务结果，更新父任务最终状态
//
// V1 版本使用简单的循环轮询机制，适合单节点部署。
// V2 版本引入了分布式锁（dlock）+ 分片策略（ShardingStrategy）+ 资源信号量（ResourceSemaphore），
// 支持多节点部署时的协调与负载均衡：
//   - 分布式锁确保同一补偿任务不会被多个节点重复执行
//   - 分片策略将数据按分库分表规则分配给不同节点处理
//   - 资源信号量限制并发补偿的资源消耗
package compensator

import (
	"context"
	"fmt"

	executorv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/executor/v1"
	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/errs"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"gitee.com/flycash/distributed_task_platform/pkg/grpc"
	"gitee.com/flycash/distributed_task_platform/pkg/loopjob"
	"gitee.com/flycash/distributed_task_platform/pkg/sharding"
	"github.com/gotomicro/ego/core/elog"
	"github.com/meoying/dlock-go"
)

// InterruptCompensatorV2 是 V2 版本的中断补偿器。
//
// 职责：定期扫描已超时（超过 maxExecutionTime）但仍处于 Running 状态的任务执行记录，
// 通过 gRPC 向对应的执行节点发送中断信号，强制终止超时任务。
//
// 与 V1 的区别：
//   - 使用 ShardingLoopJob 代替简单 LoopJob，支持分库分表数据的分片扫描
//   - 引入分布式锁（dlockClient），防止多调度节点重复补偿同一批数据
//   - 引入资源信号量（sem），控制补偿任务对系统资源的占用
//
// 核心流程：
//  1. ShardingLoopJob 获取分布式锁 → 按分片规则确定扫描的目标表
//  2. 调用 execSvc.FindTimeoutExecutions 查找超时记录
//  3. 对每条超时记录，通过 gRPC 发送 Interrupt 请求到执行节点
//  4. 执行节点响应成功后，更新执行状态
type InterruptCompensatorV2 struct {
	execSvc      task.ExecutionService                              // 执行记录服务，查询超时记录和更新状态
	config       InterruptConfig                                    // 中断补偿配置（批量大小、超时阈值等）
	logger       *elog.Component                                    // 日志组件
	grpcClients  *grpc.ClientsV2[executorv1.ExecutorServiceClient]  // V2 版 gRPC 客户端池，支持按服务名路由到执行节点
	dlockClient  dlock.Client                                       // 分布式锁客户端，防止多节点重复补偿
	sem          loopjob.ResourceSemaphore                          // 资源信号量，限制并发补偿的资源消耗
	executionStr sharding.ShardingStrategy                          // 分片策略，决定当前节点负责补偿哪些分库分表的数据
}

// NewInterruptCompensatorV2 创建 V2 版中断补偿器实例。
// 参数说明：
//   - grpcClients: V2 版 gRPC 客户端池，用于向执行节点发送中断请求
//   - execSvc: 执行记录服务，用于查询超时记录和更新执行状态
//   - config: 中断补偿配置
//   - dlockClient: 分布式锁客户端
//   - sem: 资源信号量
//   - executionStr: 分库分表路由策略
func NewInterruptCompensatorV2(
	grpcClients *grpc.ClientsV2[executorv1.ExecutorServiceClient],
	execSvc task.ExecutionService,
	config InterruptConfig,
	dlockClient dlock.Client,
	sem loopjob.ResourceSemaphore,
	executionStr sharding.ShardingStrategy,
) *InterruptCompensatorV2 {
	return &InterruptCompensatorV2{
		grpcClients:  grpcClients,
		execSvc:      execSvc,
		config:       config,
		logger:       elog.DefaultLogger.With(elog.FieldComponentName("compensator.interrupt")),
		dlockClient:  dlockClient,
		sem:          sem,
		executionStr: executionStr,
	}
}

// Start 启动中断补偿器的后台循环任务。
// 使用 ShardingLoopJob 框架运行，该框架会：
//  1. 通过分布式锁（key="interruptKey"）确保同一时刻只有一个节点在执行中断补偿
//  2. 按 executionStr 分片策略遍历所有分库分表，依次对每个分片执行补偿逻辑
//  3. 通过 sem 资源信号量控制并发度
func (t *InterruptCompensatorV2) Start(ctx context.Context) {
	const interruptKey = "interruptKey"
	loopjob.NewShardingLoopJob(t.dlockClient, interruptKey, t.interruptTimeoutTasks, t.executionStr, t.sem).Run(ctx)
}

// interruptTimeoutTasks 执行一轮中断补偿。
// 流程：
//  1. 从当前分片对应的表中批量查找超时的执行记录（由 ShardingLoopJob 通过 ctx 注入分片信息）
//  2. 遍历每条超时记录，逐一调用 interruptTaskExecution 发送中断请求
//  3. 单条中断失败不影响其他记录的处理（continue 跳过）
//
//nolint:dupl //忽略
func (t *InterruptCompensatorV2) interruptTimeoutTasks(ctx context.Context) error {
	// 查找超时的执行记录
	executions, err := t.execSvc.FindTimeoutExecutions(ctx, t.config.BatchSize)
	if err != nil {
		return fmt.Errorf("查找可中断任务失败: %w", err)
	}

	if len(executions) == 0 {
		t.logger.Info("没有找到可中断的任务")
		return nil
	}

	t.logger.Info("找到可中断任务", elog.Int("count", len(executions)))

	// 处理每个超时的执行
	for i := range executions {
		err = t.interruptTaskExecution(ctx, executions[i])
		if err != nil {
			t.logger.Error("中断超时任务失败",
				elog.Int64("executionId", executions[i].ID),
				elog.String("taskName", executions[i].Task.Name),
				elog.FieldErr(err))
			continue
		}
		t.logger.Info("成功中断超时任务",
			elog.Int64("executionId", executions[i].ID),
			elog.String("taskName", executions[i].Task.Name))
	}
	return nil
}

// interruptTaskExecution 对单条超时执行记录发送中断请求。
// 流程：
//  1. 检查任务是否配置了 gRPC 调用信息（中断只支持 gRPC 协议）
//  2. 通过 grpcClients 获取对应执行节点的 gRPC 客户端
//  3. 发送 InterruptRequest，携带执行记录 ID
//  4. 如果执行节点响应成功，将返回的最新执行状态更新到数据库
//  5. 如果执行节点响应失败（success=false），返回 ErrInterruptTaskExecutionFailed
func (t *InterruptCompensatorV2) interruptTaskExecution(ctx context.Context, execution domain.TaskExecution) error {
	if execution.Task.GrpcConfig == nil {
		return fmt.Errorf("未找到GPRC配置，无法执行中断任务")
	}
	// 根据任务配置的服务名，从客户端池中获取对应执行节点的 gRPC 客户端
	client := t.grpcClients.Get(execution.Task.GrpcConfig.ServiceName)
	resp, err := client.Interrupt(ctx, &executorv1.InterruptRequest{
		Eid: execution.ID,
	})
	if err != nil {
		return fmt.Errorf("发送中断请求失败：%w", err)
	}
	if !resp.GetSuccess() {
		// 执行节点明确响应中断失败，可能任务已经结束或不可中断
		return errs.ErrInterruptTaskExecutionFailed
	}
	// 将执行节点返回的最新状态同步到调度平台的数据库
	return t.execSvc.UpdateState(ctx, domain.ExecutionStateFromProto(resp.GetExecutionState()))
}
