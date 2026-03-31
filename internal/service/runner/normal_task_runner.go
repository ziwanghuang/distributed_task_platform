package runner

import (
	"context"
	"fmt"
	"time"

	"gitee.com/flycash/distributed_task_platform/internal/service/acquirer"
	"gitee.com/flycash/distributed_task_platform/internal/service/invoker"
	"gitee.com/flycash/distributed_task_platform/pkg/grpc/balancer"

	"gitee.com/flycash/distributed_task_platform/internal/event"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"github.com/gotomicro/ego/core/elog"
)

var _ Runner = &NormalTaskRunner{}

// NormalTaskRunner 普通任务（含分片任务）的执行器。
// 职责链：抢占任务 → 创建执行记录 → 通过 Invoker 异步触发执行 → 更新执行状态。
//
// 核心设计：
//   - 任务执行采用异步模式，Run 方法在抢占+创建执行记录后立即返回，
//     实际的远程调用在独立 goroutine 中完成，不阻塞调度循环
//   - 分片任务会创建多个子执行记录，每个分片在独立 goroutine 中并行执行
//   - 支持通过 context 注入路由策略（指定节点/排除节点），由负载均衡器层消费
type NormalTaskRunner struct {
	nodeID       string                 // 当前调度节点ID，用于任务抢占时标识归属
	taskSvc      task.Service           // 任务服务，管理任务元数据
	execSvc      task.ExecutionService  // 任务执行服务，管理执行记录的生命周期
	taskAcquirer acquirer.TaskAcquirer  // 任务抢占器，基于 CAS 乐观锁实现分布式抢占
	invoker      invoker.Invoker        // 远程调用分发器（实际是 invoker.Dispatcher，按协议路由到 gRPC/HTTP/Local）
	producer     event.CompleteProducer // 任务完成事件生产者，用于驱动 DAG 工作流的后继任务

	logger *elog.Component
}

func NewNormalTaskRunner(
	nodeID string,
	taskSvc task.Service,
	execSvc task.ExecutionService,
	taskAcquirer acquirer.TaskAcquirer,
	invoker invoker.Invoker,
	producer event.CompleteProducer,
) *NormalTaskRunner {
	return &NormalTaskRunner{
		nodeID:       nodeID,
		taskSvc:      taskSvc,
		execSvc:      execSvc,
		taskAcquirer: taskAcquirer,
		invoker:      invoker,
		producer:     producer,
		logger:       elog.DefaultLogger.With(elog.FieldComponentName("runner.NormalTaskRunner")),
	}
}

// Run 执行普通任务的完整调度流程。
// 流程：
//  1. 通过 CAS 乐观锁抢占任务（将任务状态从 WAITING 改为 PREEMPTED）
//  2. 保留原始 PlanExecID（DAG 场景下尚未入库的执行 ID）
//  3. 根据任务是否配置了分片规则，分发到不同的处理路径
func (s *NormalTaskRunner) Run(ctx context.Context, task domain.Task) error {
	// 抢占任务
	acquiredTask, err := s.acquireTask(ctx, task)
	if err != nil {
		s.logger.Error("任务抢占失败",
			elog.Int64("taskID", task.ID),
			elog.String("taskName", task.Name),
			elog.FieldErr(err))
		return err
	}
	// 这里使用传入的 PlanExecID ，因为 PlanExecID 还没有入库。
	acquiredTask.PlanExecID = task.PlanExecID
	// 根据分片规则区分任务类型
	if acquiredTask.ShardingRule == nil {
		return s.handleNormalTask(ctx, acquiredTask)
	} else {
		return s.handleShardingTask(ctx, acquiredTask)
	}
}

// acquireTask 通过 CAS 乐观锁抢占任务。
// 底层 SQL：UPDATE tasks SET status='PREEMPTED', schedule_node_id=?, version=version+1
//           WHERE id=? AND version=? AND status='WAITING'
// 只有版本号匹配时才能抢占成功，天然解决多调度节点的并发竞争问题。
func (s *NormalTaskRunner) acquireTask(ctx context.Context, task domain.Task) (domain.Task, error) {
	// 抢占任务
	acquiredTask, err := s.taskAcquirer.Acquire(ctx, task.ID, task.Version, s.nodeID)
	if err != nil {
		return domain.Task{}, fmt.Errorf("任务抢占失败: %w", err)
	}
	// 抢占成功
	return acquiredTask, nil
}

// handleNormalTask 处理非分片的普通任务。
// 流程：
//  1. 创建 TaskExecution 记录（状态 = Prepare），标记任务已开始
//  2. 如果创建失败，释放任务抢占锁让其他节点可以重新调度
//  3. 启动 goroutine 异步调用执行节点，不阻塞调度循环
func (s *NormalTaskRunner) handleNormalTask(ctx context.Context, task domain.Task) error {
	// 抢占成功，立即创建TaskExecution记录
	execution, err := s.execSvc.Create(ctx, domain.TaskExecution{
		Task: task,
		// 可以认为开始执行了，防止执行节点直接返回"终态"状态Failed，Success等
		StartTime: time.Now().UnixMilli(),
		Status:    domain.TaskExecutionStatusPrepare,
	})
	if err != nil {
		s.logger.Error("创建任务执行记录失败",
			elog.Int64("taskID", task.ID),
			elog.String("taskName", task.Name),
			elog.FieldErr(err))
		// 释放任务
		s.releaseTask(ctx, task)
		return err
	}

	// 抢占和创建都成功，异步触发任务执行。
	// 使用 goroutine 异步执行，让调度循环可以继续处理下一个任务，不阻塞主调度流程。
	go func() {
		// 通过 Invoker 发起远程调用（gRPC/HTTP/Local），获取执行节点返回的执行状态
		state, runErr := s.invoker.Run(ctx, execution)
		if runErr != nil {
			s.logger.Error("执行器执行任务失败", elog.FieldErr(runErr))
			return
		}

		// 将执行节点返回的状态更新到数据库，触发状态机迁移
		if updateErr := s.execSvc.UpdateState(ctx, state); updateErr != nil {
			s.logger.Error("正常调度任务失败",
				elog.Any("execution", execution),
				elog.Any("state", state),
				elog.FieldErr(updateErr))
		}
	}()
	return nil
}

// handleShardingTask 处理分片任务。
// 分片任务会根据 ShardingRule 将一个任务拆分为多个子任务并行执行：
//  1. 调用 CreateShardingChildren 创建父执行记录 + N 个子执行记录
//  2. 每个子任务分配到不同的执行节点（通过 context 注入 SpecificNodeID）
//  3. 子任务在独立 goroutine 中并行执行
//  4. 所有子任务完成后，由 ShardingCompensator 汇总父任务状态
func (s *NormalTaskRunner) handleShardingTask(ctx context.Context, task domain.Task) error {
	// 抢占成功，立即创建子任务执行记录
	executions, err := s.execSvc.CreateShardingChildren(ctx, domain.TaskExecution{
		Task: task,
		// 可以认为开始执行了，防止执行节点直接返回"终态"状态Failed，Success等
		StartTime: time.Now().UnixMilli(),
		Status:    domain.TaskExecutionStatusRunning,
	})
	if err != nil {
		s.logger.Error("创建子任务执行记录失败",
			elog.Int64("taskID", task.ID),
			elog.String("taskName", task.Name),
			elog.FieldErr(err))
		// 释放任务
		s.releaseTask(ctx, task)
		return err
	}

	// 抢占和创建都成功，异步触发各分片子任务的并行执行。
	// 每个子任务在独立的 goroutine 中执行，通过 context 携带指定节点 ID 实现分片亲和性调度。
	for i := range executions {
		execution := executions[i]
		go func() {
			// 在 context 中注入指定的执行节点 ID，确保分片子任务被路由到对应的执行节点。
			// 负载均衡器会从 ctx 中读取 SpecificNodeID，优先选择该节点。
			state, runErr := s.invoker.Run(s.WithSpecificNodeIDContext(ctx, execution.ExecutorNodeID), execution)
			if runErr != nil {
				s.logger.Error("执行器执行任务失败", elog.FieldErr(runErr))
				return
			}

			if updateErr := s.execSvc.UpdateState(ctx, state); updateErr != nil {
				s.logger.Error("正常调度子任务失败",
					elog.Any("execution", execution),
					elog.Any("state", state),
					elog.FieldErr(updateErr))
			}
		}()
	}
	return nil
}

// releaseTask 释放任务
func (s *NormalTaskRunner) releaseTask(ctx context.Context, task domain.Task) {
	if err := s.taskAcquirer.Release(ctx, task.ID, s.nodeID); err != nil {
		s.logger.Error("释放任务失败",
			elog.Int64("taskID", task.ID),
			elog.String("taskName", task.Name),
			elog.FieldErr(err))
	}
}

// WithSpecificNodeIDContext 在 context 中注入指定执行节点 ID。
// 负载均衡器的 Picker 会优先选择该节点，实现分片亲和性调度。
// 如果 executorNodeID 为空，返回原始 ctx（由默认轮询策略选择节点）。
func (s *NormalTaskRunner) WithSpecificNodeIDContext(ctx context.Context, executorNodeID string) context.Context {
	if executorNodeID != "" {
		return balancer.WithSpecificNodeID(ctx, executorNodeID)
	}
	return ctx
}

//nolint:dupl //忽略
// Retry 重试执行失败的任务。
// 重试逻辑：
//  1. 非分片任务需要重新抢占（因为任务已被释放），分片任务无需重新抢占（锁一直持有到父任务完成）
//  2. 在 context 中注入"排除节点 ID"，让负载均衡器避开上次执行失败的节点
//  3. 异步发起重试执行，避免阻塞补偿器循环
func (s *NormalTaskRunner) Retry(ctx context.Context, execution domain.TaskExecution) error {
	if !s.isShardedTaskExecution(execution) {
		// 抢占任务
		acquiredTask, err := s.acquireTask(ctx, execution.Task)
		if err != nil {
			s.logger.Error("重试：任务抢占失败",
				elog.Int64("taskID", execution.Task.ID),
				elog.String("taskName", execution.Task.Name),
				elog.FieldErr(err))
			return err
		}
		execution.Task = acquiredTask
	}

	// 抢占和创建都成功，异步触发任务
	go func() {
		// 执行任务，并在 context 中设置要排除的执行节点 ID，避免重调度到同一个节点
		state, err1 := s.invoker.Run(s.WithExcludedNodeIDContext(ctx, execution.ExecutorNodeID), execution)
		if err1 != nil {
			s.logger.Error("执行器执行任务失败", elog.FieldErr(err1))
			return
		}

		err1 = s.execSvc.UpdateState(ctx, state)
		if err1 != nil {
			s.logger.Error("重试任务失败",
				elog.Any("execution", execution),
				elog.Any("state", state),
				elog.FieldErr(err1))
		}
	}()
	return nil
}

// WithExcludedNodeIDContext 在 context 中注入要排除的执行节点 ID。
// 负载均衡器的 Picker 在选择节点时会跳过该节点，用于重试场景避免命中同一个故障节点。
func (s *NormalTaskRunner) WithExcludedNodeIDContext(ctx context.Context, executorNodeID string) context.Context {
	if executorNodeID != "" {
		return balancer.WithExcludedNodeID(ctx, executorNodeID)
	}
	return ctx
}

//nolint:dupl //忽略
// Reschedule 重新调度任务到指定执行节点。
// 与 Retry 的区别：Retry 排除原节点随机选择，Reschedule 指定到原节点重新执行。
// 典型场景：执行节点临时故障恢复后，需要在同一节点继续执行（如保持本地缓存/状态）。
func (s *NormalTaskRunner) Reschedule(ctx context.Context, execution domain.TaskExecution) error {
	if !s.isShardedTaskExecution(execution) {
		// 抢占任务
		acquiredTask, err := s.acquireTask(ctx, execution.Task)
		if err != nil {
			s.logger.Error("重调度：任务抢占失败",
				elog.Int64("taskID", execution.Task.ID),
				elog.String("taskName", execution.Task.Name),
				elog.FieldErr(err))
			return err
		}
		execution.Task = acquiredTask
	}

	// 抢占和创建都成功，异步触发任务
	go func() {
		// 执行任务，并在 context 中设置要指定的执行节点ID
		state, err1 := s.invoker.Run(s.WithSpecificNodeIDContext(ctx, execution.ExecutorNodeID), execution)
		if err1 != nil {
			s.logger.Error("执行器执行任务失败", elog.FieldErr(err1))
			return
		}

		err1 = s.execSvc.UpdateState(ctx, state)
		if err1 != nil {
			s.logger.Error("重调度任务失败",
				elog.Any("execution", execution),
				elog.Any("state", state),
				elog.FieldErr(err1))
		}
	}()
	return nil
}

// isShardedTaskExecution 判断当前执行记录是否属于分片子任务。
// 分片子任务的特征：ShardingParentID 不为 nil 且大于 0。
// 分片任务的"锁"（抢占状态）由父任务持有，直到所有子任务执行完毕后，
// 由 ShardingCompensator 汇总父任务状态并统一释放锁。
// 因此分片子任务在重试/重调度时无需重新抢占。
func (s *NormalTaskRunner) isShardedTaskExecution(execution domain.TaskExecution) bool {
	// 分片任务的”锁“会被一只持有，直到各个子任务执行完后，会在sharding补偿任务中计算父任务的状态的同时释放抢占的task
	// 所以这里无需再次抢占，即使抢占也是失败
	return execution.ShardingParentID != nil && *execution.ShardingParentID > 0
}
