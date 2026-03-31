package compensator

import (
	"context"
	"fmt"

	"gitee.com/flycash/distributed_task_platform/internal/service/runner"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"gitee.com/flycash/distributed_task_platform/pkg/loopjob"
	"gitee.com/flycash/distributed_task_platform/pkg/sharding"
	"github.com/gotomicro/ego/core/elog"
	"github.com/meoying/dlock-go"
)

// RescheduleCompensatorV2 是 V2 版本的重调度补偿器。
//
// 职责：定期扫描处于"需要重调度"状态的执行记录（通常是执行节点主动请求重调度的场景，
// 例如节点负载过高时请求将任务转移到其他节点），并通过 Runner 重新执行调度流程。
//
// 与 V1 的区别：
//   - 使用 ShardingLoopJob 替代简单 LoopJob，支持分库分表的分片扫描
//   - 引入分布式锁（dlockClient），防止多节点重复处理同一批重调度任务
//   - 引入资源信号量（sem），控制补偿任务的资源消耗
//
// 核心流程：
//  1. ShardingLoopJob 获取分布式锁 → 按分片遍历各目标表
//  2. 调用 execSvc.FindReschedulableExecutions 查找可重调度的记录
//  3. 对每条记录调用 runner.Reschedule 重新执行调度
type RescheduleCompensatorV2 struct {
	runner  runner.Runner                 // 任务运行器，负责执行实际的重调度逻辑
	execSvc task.ExecutionService         // 执行记录服务，查询可重调度记录
	config  RescheduleConfig              // 重调度配置（批量大小等）
	logger  *elog.Component               // 日志组件

	dlockClient  dlock.Client             // 分布式锁客户端，防止多节点重复补偿
	sem          loopjob.ResourceSemaphore // 资源信号量，限制并发
	executionStr sharding.ShardingStrategy // 分片策略，决定当前节点负责哪些分表
}

// NewRescheduleCompensatorV2 创建 V2 版重调度补偿器实例。
// 参数说明：
//   - runner: 任务运行器，调用其 Reschedule 方法重新执行任务
//   - execSvc: 执行记录服务
//   - config: 重调度配置
//   - dlockClient: 分布式锁客户端
//   - sem: 资源信号量
//   - executionStr: 分库分表路由策略
func NewRescheduleCompensatorV2(
	runner runner.Runner,
	execSvc task.ExecutionService,
	config RescheduleConfig,
	dlockClient dlock.Client,
	sem loopjob.ResourceSemaphore,
	executionStr sharding.ShardingStrategy,
) *RescheduleCompensatorV2 {
	return &RescheduleCompensatorV2{
		runner:       runner,
		execSvc:      execSvc,
		config:       config,
		logger:       elog.DefaultLogger.With(elog.FieldComponentName("compensator.reschedule")),
		dlockClient:  dlockClient,
		sem:          sem,
		executionStr: executionStr,
	}
}

// Start 启动重调度补偿器的后台循环任务。
// 通过 ShardingLoopJob 框架运行，使用分布式锁（key="rescheduleKey"）保证同一时刻
// 只有一个节点在执行重调度补偿，并按分片策略遍历所有分库分表。
func (r *RescheduleCompensatorV2) Start(ctx context.Context) {
	const rescheduleKey = "rescheduleKey"
	loopjob.NewShardingLoopJob(r.dlockClient, rescheduleKey, r.reschedule, r.executionStr, r.sem).Run(ctx)
}

// reschedule 执行一轮重调度补偿。
// 流程：
//  1. 从当前分片对应的表中批量查找可重调度的执行记录
//  2. 遍历每条记录，调用 runner.Reschedule 重新走调度流程（创建新的执行记录并派发到执行节点）
//  3. 单条重调度失败不影响其他记录的处理
func (r *RescheduleCompensatorV2) reschedule(ctx context.Context) error {
	// 查找可重调度的执行记录
	executions, err := r.execSvc.FindReschedulableExecutions(
		ctx,
		r.config.BatchSize,
	)
	if err != nil {
		return fmt.Errorf("查找可重调度任务失败: %w", err)
	}

	if len(executions) == 0 {
		r.logger.Info("没有找到可重调度的任务")
		return nil
	}

	r.logger.Info("找到可重调度任务", elog.Int("count", len(executions)))

	// 处理每个可重调度的执行
	for i := range executions {
		err = r.runner.Reschedule(ctx, executions[i])
		if err != nil {
			r.logger.Error("重调度失败",
				elog.Int64("executionId", executions[i].ID),
				elog.String("taskName", executions[i].Task.Name),
				elog.FieldErr(err))
			continue
		}
	}
	return nil
}
