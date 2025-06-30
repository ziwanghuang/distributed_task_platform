package scheduler

import (
	"context"
	"fmt"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"github.com/ecodeclub/ekit/syncx"
	"go.uber.org/multierr"
)

// TaskManager Job管理器，负责Job的创建、管理、生命周期跟踪
type TaskManager struct {
	nodeID         string                           // 当前调度节点ID
	execSvc        task.ExecutionService            // 需要用到的执行服务
	taskExecutions syncx.Map[int64, *TaskExecution] // 正在运行的Job
}

// NewTaskManager 创建TaskManager实例
func NewTaskManager(nodeID string, execSvc task.ExecutionService) *TaskManager {
	return &TaskManager{
		nodeID:  nodeID,
		execSvc: execSvc,
	}
}

// Run 异步执行任务，返回结果channel
func (tm *TaskManager) Run(ctx context.Context, task domain.Task) <-chan error {
	execution := NewTaskExecution(task, tm.execSvc)
	tm.taskExecutions.Store(task.ID, execution)

	// 异步执行
	ch := make(chan error, 1)
	go func() {
		defer func() {
			tm.taskExecutions.Delete(task.ID) // 清理
			close(ch)
		}()
		err := execution.Run(ctx)
		ch <- err
	}()
	return ch
}

// Stop 停止指定的任务
func (tm *TaskManager) Stop(task domain.Task) error {
	if job, ok := tm.taskExecutions.Load(task.ID); ok {
		return job.Stop()
	}
	return fmt.Errorf("task %d not found", task.ID)
}

// RenewFailed 处理续约失败
func (tm *TaskManager) RenewFailed(task domain.Task) error {
	if job, ok := tm.taskExecutions.Load(task.ID); ok {
		return job.HandleRenewFailure()
	}
	return nil
}

// ReportProgress 处理执行节点主动上报的任务执行状态
func (tm *TaskManager) ReportProgress(taskID int64, progress *ProgressReport) error {
	if job, exists := tm.taskExecutions.Load(taskID); exists {
		return job.HandleReport(progress)
	}
	return fmt.Errorf("task %d not found", taskID)
}

// Close 关闭管理器，停止所有正在运行的Job
func (tm *TaskManager) Close() error {
	var err error
	tm.taskExecutions.Range(func(taskID int64, exec *TaskExecution) bool {
		if err1 := exec.Stop(); err1 != nil {
			err = multierr.Append(err, fmt.Errorf("stop task %d failed: %w", taskID, err))
		}
		return true
	})
	return err
}
