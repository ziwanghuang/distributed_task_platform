package scheduler

import (
	"context"
	"fmt"
	"time"
)

// processJobAsync 的详细实现骨架
// 这是整个事件循环的核心协程逻辑
func (sel *SchedulerEventLoop) processJobAsync(job *Job) {
	jobID := job.ID
	// nodeID := sel.nodeID // 预留给后续抢占逻辑使用

	// ============================================================================
	// 阶段1: 抢占任务 (ACTIVE -> PREEMPTED)
	// ============================================================================

	err := sel.taskExecutor.PreemptJob(job)
	if err != nil {
		// 抢占失败，可能被其他节点抢占了
		// TODO: 记录日志，直接返回
		return
	}

	// ============================================================================
	// 阶段2: 启动续约协程 (防止被其他节点重新发现)
	// ============================================================================

	sel.renewalManager.AddJob(jobID)
	defer sel.renewalManager.RemoveJob(jobID) // 确保最后清理

	// ============================================================================
	// 阶段3: 创建JobExecution记录 (状态: PREPARE)
	// ============================================================================

	execution, err := sel.taskExecutor.CreateExecution(job)
	if err != nil {
		// 创建执行记录失败，释放锁
		sel.releaseJobOnError(job, fmt.Sprintf("创建执行记录失败: %v", err))
		return
	}

	// ============================================================================
	// 阶段4: 启动执行监控
	// ============================================================================

	sel.progressMonitor.AddExecution(execution)
	defer sel.progressMonitor.RemoveExecution(execution.ID) // 确保最后清理

	// ============================================================================
	// 阶段5: 调用执行器 (状态: PREPARE -> RUNNING)
	// ============================================================================

	// 更新execution状态为RUNNING
	err = sel.updateExecutionStatus(execution.ID, JobExecutionStatusRunning)
	if err != nil {
		sel.releaseJobOnError(job, fmt.Sprintf("更新执行状态失败: %v", err))
		return
	}

	// 异步调用执行器
	go func() {
		defer func() {
			if r := recover(); r != nil {
				// 执行器panic，标记为失败
				sel.resultCollector.NotifyCompleted(&JobExecution{
					ID:     execution.ID,
					JobID:  execution.JobID,
					Status: JobExecutionStatusFailed,
					Result: fmt.Sprintf("执行器panic: %v", r),
				})
			}
		}()

		// 实际调用执行器
		err := sel.taskExecutor.ExecuteJob(job, execution)
		if err != nil {
			// 执行失败
			sel.resultCollector.NotifyCompleted(&JobExecution{
				ID:     execution.ID,
				JobID:  execution.JobID,
				Status: JobExecutionStatusFailed,
				Result: err.Error(),
			})
		}
		// 注意：成功的情况下，结果通过进度上报或轮询获得
	}()

	// ============================================================================
	// 阶段6: 等待执行完成 (轮询 + 回调)
	// ============================================================================

	// 这里有几种策略：
	// 1. 纯轮询：定期调用ExecutorService.Query
	// 2. 纯回调：等待进度上报
	// 3. 混合模式：回调 + 兜底轮询

	ctx, cancel := context.WithTimeout(sel.ctx, time.Duration(job.Timeout)*time.Second)
	defer cancel()

	completed := make(chan *JobExecution, 1)

	// 启动轮询协程
	go sel.startPollingForExecution(ctx, execution, completed)

	// 等待完成或超时
	select {
	case <-ctx.Done():
		// 超时处理
		sel.handleExecutionTimeout(execution)

	case finalExecution := <-completed:
		// 执行完成处理
		sel.handleExecutionCompleted(job, finalExecution)
	}
}

// ============================================================================
// 辅助方法
// ============================================================================

// releaseJobOnError 错误时释放任务锁
func (sel *SchedulerEventLoop) releaseJobOnError(job *Job, reason string) {
	// TODO: 记录错误日志
	// TODO: 计算下次执行时间（可能需要退避策略）
	// TODO: 释放锁，状态改回ACTIVE
	sel.taskExecutor.ReleaseJob(job)
}

// updateExecutionStatus 更新执行状态
func (sel *SchedulerEventLoop) updateExecutionStatus(executionID int64, status JobExecutionStatus) error {
	// TODO: 调用 executionRepo.UpdateStatus
	return nil
}

// startPollingForExecution 启动轮询协程
func (sel *SchedulerEventLoop) startPollingForExecution(ctx context.Context, execution *JobExecution, completed chan<- *JobExecution) {
	ticker := time.NewTicker(30 * time.Second) // 30秒轮询一次
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			// TODO: 调用ExecutorService.Query查询状态
			// TODO: 如果完成，发送到completed channel
			status := sel.queryExecutionStatus(execution)
			if sel.isTerminalStatus(status) {
				// 构造完成的execution
				finalExecution := &JobExecution{
					ID:     execution.ID,
					JobID:  execution.JobID,
					Status: status,
					// ... 其他字段
				}

				select {
				case completed <- finalExecution:
				default:
					// channel已关闭或已有结果
				}
				return
			}
		}
	}
}

// queryExecutionStatus 查询执行状态
func (sel *SchedulerEventLoop) queryExecutionStatus(execution *JobExecution) JobExecutionStatus {
	// TODO: 根据execution的配置，调用对应的查询接口
	// 可能是gRPC调用或HTTP查询
	return JobExecutionStatusRunning
}

// isTerminalStatus 判断是否为终止状态
func (sel *SchedulerEventLoop) isTerminalStatus(status JobExecutionStatus) bool {
	switch status {
	case JobExecutionStatusSuccess,
		JobExecutionStatusFailed,
		JobExecutionStatusRetryableFailed,
		JobExecutionStatusCanceled,
		JobExecutionStatusTimeout:
		return true
	default:
		return false
	}
}

// handleExecutionTimeout 处理执行超时
func (sel *SchedulerEventLoop) handleExecutionTimeout(execution *JobExecution) {
	// TODO: 1. 更新execution状态为TIMEOUT
	// TODO: 2. 尝试取消执行（如果支持）
	// TODO: 3. 通知结果收集器

	sel.resultCollector.NotifyCompleted(&JobExecution{
		ID:     execution.ID,
		JobID:  execution.JobID,
		Status: JobExecutionStatusTimeout,
		Result: "执行超时",
	})
}

// handleExecutionCompleted 处理执行完成
func (sel *SchedulerEventLoop) handleExecutionCompleted(job *Job, execution *JobExecution) {
	// TODO: 1. 更新execution最终状态
	// TODO: 2. 根据结果决定是否重试
	// TODO: 3. 通知结果收集器

	sel.resultCollector.NotifyCompleted(execution)
}
