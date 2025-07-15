package compensator

import (
	"context"
	"fmt"
	"time"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/service/acquirer"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"github.com/gotomicro/ego/core/elog"
	"go.uber.org/multierr"
)

// ShardingConfig 分片任务补偿器配置
type ShardingConfig struct {
	BatchSize   int           // 批次大小
	MinDuration time.Duration // 最小等待时间，防止空转
}

// ShardingCompensator 分片任务补偿器
type ShardingCompensator struct {
	nodeID       string
	taskSvc      task.Service
	execSvc      task.ExecutionService
	taskAcquirer acquirer.TaskAcquirer // 任务抢占器
	config       ShardingConfig
	logger       *elog.Component
}

// NewShardingCompensator 创建分片任务补偿器
func NewShardingCompensator(
	nodeID string,
	taskSvc task.Service,
	execSvc task.ExecutionService,
	taskAcquirer acquirer.TaskAcquirer,
	config ShardingConfig,
) *ShardingCompensator {
	return &ShardingCompensator{
		nodeID:       nodeID,
		taskSvc:      taskSvc,
		execSvc:      execSvc,
		taskAcquirer: taskAcquirer,
		config:       config,
		logger:       elog.DefaultLogger.With(elog.FieldComponentName("compensator.sharding")),
	}
}

// Start 启动补偿器
func (r *ShardingCompensator) Start(ctx context.Context) {
	r.logger.Info("分片任务补偿器启动")

	for {
		select {
		case <-ctx.Done():
			r.logger.Info("分片任务补偿器停止")
			return
		default:
		}

		// 查找可重调度的执行记录
		executions, err := r.execSvc.FindShardingParents(
			ctx,
			r.config.BatchSize,
		)
		if err != nil {
			r.logger.Error("查找分片父任务失败", elog.FieldErr(err))
			continue
		}

		if len(executions) == 0 {
			r.logger.Info("没有找到分片父任务")
			time.Sleep(r.config.MinDuration)
		}

		r.logger.Info("找到分片父任务", elog.Int("count", len(executions)))

		for i := range executions {
			err = r.handle(ctx, executions[i])
			if err != nil {
				r.logger.Error("分片任务补偿失败",
					elog.Int64("executionId", executions[i].ID),
					elog.String("taskName", executions[i].Task.Name),
					elog.FieldErr(err))
				continue
			}
		}
	}
}

// handle 执行一轮补偿
func (r *ShardingCompensator) handle(ctx context.Context, parent domain.TaskExecution) error {
	// 取该父任务下的【所有】子任务
	children, err := r.execSvc.FindShardingChildren(ctx, parent.ID)
	if err != nil {
		return fmt.Errorf("查找分片子任务失败: %w", err)
	}

	// 边界情况：如果一个父任务没有任何子任务，说明创建流程出了问题，应标记为失败。
	if len(children) == 0 {
		r.logger.Warn("分片父任务没有任何子任务，可能因创建异常导致，强制标记为失败",
			elog.Int64("parentId", parent.ID))
		defer r.releaseTask(ctx, parent.Task)
		var errs error
		if _, err := r.taskSvc.UpdateNextTime(ctx, parent.Task.ID); err != nil {
			errs = multierr.Append(errs, fmt.Errorf("更新任务下次更新时间失败：%w", err))
		}
		err = r.execSvc.UpdateScheduleResult(ctx, parent.ID, domain.TaskExecutionStatusFailed, 0, time.Now().UnixMilli(), nil, "")
		if err != nil {
			errs = multierr.Append(errs, fmt.Errorf("更新父任务最终状态失败: %w", err))
		}
		return errs
	}

	anyFailed := false
	successCount := 0
	for i := range children {
		if !children[i].Status.IsSuccess() && !children[i].Status.IsFailed() {
			// 只要发现任何一个子任务还在运行，就立即中止本轮补偿。
			r.logger.Info("子任务未全部完成，等待下次补偿...", elog.Int64("parentId", parent.ID))
			return nil
		}
		if children[i].Status.IsFailed() {
			anyFailed = true
		}
		if children[i].Status.IsSuccess() {
			successCount++
		}
	}

	defer r.releaseTask(ctx, parent.Task)

	var errs error

	// 持久化父任务的最终状态
	var finalStatus domain.TaskExecutionStatus
	if anyFailed {
		finalStatus = domain.TaskExecutionStatusFailed
		r.logger.Info("部分子任务失败，父任务标记为FAILED", elog.Int64("parentId", parent.ID))
	} else {
		finalStatus = domain.TaskExecutionStatusSuccess
		r.logger.Info("所有子任务成功，父任务标记为SUCCESS", elog.Int64("parentId", parent.ID))
	}
	const unit = 100
	progress := int32((successCount * unit) / len(children))
	err = r.execSvc.UpdateScheduleResult(ctx, parent.ID, finalStatus, progress, time.Now().UnixMilli(), nil, "")
	if err != nil {
		errs = multierr.Append(errs, fmt.Errorf("更新父任务最终状态失败: %w", err))
	}
	// 不管父任务成功与否都要更新任务下一次执行时间
	_, err = r.taskSvc.UpdateNextTime(ctx, parent.Task.ID)
	if err != nil {
		errs = multierr.Append(errs, fmt.Errorf("更新任务下次更新时间失败：%w", err))
	}
	return errs
}

// 在 ShardingCompensator 中也需要一个 releaseTask 的辅助方法
func (r *ShardingCompensator) releaseTask(ctx context.Context, task domain.Task) {
	if err := r.taskAcquirer.Release(ctx, task.ID, r.nodeID); err != nil {
		r.logger.Error("释放任务失败",
			elog.Int64("taskID", task.ID),
			elog.FieldErr(err))
	}
}
