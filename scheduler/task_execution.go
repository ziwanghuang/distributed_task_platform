package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
)

// TaskExecution 代表一个正在执行的任务实例
type TaskExecution struct {
	task domain.Task           // 任务信息
	svc  task.ExecutionService // 执行服务

	status           domain.TaskExecutionStatus // 当前执行状态
	progressReportCh chan *ProgressReport       // 进度上报channel
	stopCh           chan struct{}              // 停止信号
	mu               sync.RWMutex               // 保护Job状态

	stopped bool // 是否已停止
}

// NewTaskExecution 创建TaskExecution实例
func NewTaskExecution(task domain.Task, svc task.ExecutionService) *TaskExecution {
	return &TaskExecution{
		task:             task,
		svc:              svc,
		progressReportCh: make(chan *ProgressReport, 1),
		stopCh:           make(chan struct{}),
	}
}

// Run 执行任务，双分支等待逻辑（轮询+上报）
func (j *TaskExecution) Run(ctx context.Context) error {
	j.mu.Lock()
	if j.stopped {
		j.mu.Unlock()
		return fmt.Errorf("job already stopped")
	}
	j.status = domain.TaskExecutionStatusRunning
	j.mu.Unlock()

	// 创建执行实例
	execution := domain.TaskExecution{
		TaskID:   j.task.ID,
		TaskName: j.task.Name,
		Status:   domain.TaskExecutionStatusRunning,
	}
	createdExecution, err := j.svc.Create(ctx, execution)
	if err != nil {
		return fmt.Errorf("create task execution failed: %w", err)
	}

	execution = createdExecution

	// 轮询ticker
	pollTicker := time.NewTicker(30 * time.Second)
	defer pollTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-j.stopCh:
			// 收到停止信号
			return j.markCompleted(ctx, execution.ID, domain.TaskExecutionStatusCancelled, "job stopped")

		case progress := <-j.progressReportCh:
			// 收到进度上报
			if err := j.handleProgressReport(ctx, execution.ID, progress); err != nil {
				return err
			}

			// 如果是终态，任务完成
			if progress.IsTerminal {
				return nil
			}

		case <-pollTicker.C:
			// 轮询检查任务状态
			currentExecution, err1 := j.svc.GetByID(ctx, execution.ID)
			if err1 != nil {
				continue // 轮询错误不致命，继续
			}
			// 检查是否完成
			if j.isTerminalStatus(currentExecution.Status) {
				j.mu.Lock()
				j.status = currentExecution.Status
				j.mu.Unlock()
				return nil
			}
		}
	}
}

// HandleReport 处理执行节点上报的进度信息
func (j *TaskExecution) HandleReport(progress *ProgressReport) error {
	j.mu.RLock()
	if j.stopped {
		j.mu.RUnlock()
		return fmt.Errorf("job already stopped")
	}
	j.mu.RUnlock()

	select {
	case j.progressReportCh <- progress:
		return nil
	default:
		return fmt.Errorf("progress report channel full")
	}
}

// HandleRenewFailure 处理续约失败
func (j *TaskExecution) HandleRenewFailure() error {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.stopped {
		return nil
	}

	// todo: 调用executionSvc更新状态为FAILED-PREEMPTED

	j.stopped = true
	close(j.stopCh)
	return nil
}

// Stop 停止Job
func (j *TaskExecution) Stop() error {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.stopped {
		return nil // 已经停止
	}

	j.stopped = true
	close(j.stopCh)
	return nil
}

// handleProgressReport 处理进度上报
func (j *TaskExecution) handleProgressReport(ctx context.Context, executionID int64, progress *ProgressReport) error {
	j.mu.Lock()
	j.status = progress.Status
	j.mu.Unlock()

	// 更新执行状态
	return j.svc.UpdateStatus(ctx, executionID, progress.Status)
}

// markCompleted 标记任务完成
func (j *TaskExecution) markCompleted(ctx context.Context, executionID int64, status domain.TaskExecutionStatus, message string) error {
	j.mu.Lock()
	j.status = status
	j.mu.Unlock()
	return j.svc.UpdateStatus(ctx, executionID, status)
}

// isTerminalStatus 判断是否为终态
func (j *TaskExecution) isTerminalStatus(status domain.TaskExecutionStatus) bool {
	switch status {
	case domain.TaskExecutionStatusSuccess,
		domain.TaskExecutionStatusFailed:
		// domain.TaskExecutionStatusCancelled:
		return true
	default:
		return false
	}
}
