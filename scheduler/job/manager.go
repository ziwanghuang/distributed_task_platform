package job

import (
	"context"
	"fmt"
	"time"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/errs"
	"github.com/ecodeclub/ekit/syncx"
	"github.com/gotomicro/ego/core/elog"
	"go.uber.org/multierr"
)

// Manager Job管理器，负责Job的创建、管理、生命周期跟踪
type Manager struct {
	taskChans     syncx.Map[int64, *Chans] // 正在运行的task的通道
	jobs          map[string]Job
	reportTimeout time.Duration
	logger        *elog.Component
}

// NewManager 创建 Manager 实例
func NewManager(
	jobs map[string]Job,
	reportTimeout time.Duration,
) *Manager {
	return &Manager{
		jobs:          jobs,
		reportTimeout: reportTimeout,
		logger:        elog.DefaultLogger.With(elog.FieldComponentName("job.Manager")),
	}
}

// Run 启动任务执行，返回通道和清理函数
func (jm *Manager) Run(ctx context.Context, task domain.Task) (*Chans, func(), error) {
	j, ok := jm.jobs[task.ExecutionMethod.String()]
	if !ok {
		return nil, func() {}, fmt.Errorf("%w: %s", errs.ErrInvalidTaskExecutionMethod, task.ExecutionMethod)
	}
	// 启动任务，立即获得通道
	chans := j.Run(ctx, task)

	// 立即存储任务信息
	jm.taskChans.Store(task.ID, chans)

	// 返回cleanup函数
	cleanup := func() {
		if _, loaded := jm.taskChans.LoadAndDelete(task.ID); loaded {
			jm.logger.Debug("清理任务缓存", elog.Int64("taskID", task.ID))
		}
	}
	return chans, cleanup, nil
}

// RenewFailed 处理续约失败
func (jm *Manager) RenewFailed(_ context.Context, task domain.Task) error {
	if chans, exists := jm.taskChans.Load(task.ID); exists {
		if chans != nil {
			chans.Renew <- false // 续约失败发送false
			return nil
		}
		jm.logger.Warn("忽略续约事件",
			elog.String("jobName", task.Name),
			elog.Any("task", task),
		)
	}
	return nil
}

// HandleReports 处理执行节点主动上报的任务执行状态
func (jm *Manager) HandleReports(ctx context.Context, reports []*domain.Report) error {
	if len(reports) == 0 {
		return nil
	}

	jm.logger.Debug("开始处理执行状态上报", elog.Int("count", len(reports)))

	var combinedErr error
	processedCount := 0
	skippedCount := 0
	channelBlockedCount := 0

	for i := range reports {
		report := reports[i]

		chans, exists := jm.taskChans.Load(report.TaskID)
		if !exists {
			skippedCount++
			jm.logger.Debug("任务不存在，跳过上报",
				elog.Int64("taskID", report.TaskID),
				elog.Int64("executionID", report.ID))
			continue
		}

		if chans == nil {
			skippedCount++
			jm.logger.Warn("任务通道为空，跳过上报",
				elog.Int64("taskID", report.TaskID),
				elog.Int64("executionID", report.ID))
			continue
		}

		// 使用带超时的发送避免阻塞
		if err := jm.sendReportWithTimeout(ctx, chans.Report, report); err != nil {
			channelBlockedCount++
			reportErr := fmt.Errorf("发送上报到任务[%d]执行[%d]失败: %w", report.TaskID, report.ID, err)
			combinedErr = multierr.Append(combinedErr, reportErr)
			jm.logger.Warn("发送上报失败",
				elog.Int64("taskID", report.TaskID),
				elog.Int64("executionID", report.ID),
				elog.FieldErr(err))
		} else {
			processedCount++
			jm.logger.Debug("上报发送成功",
				elog.Int64("taskID", report.TaskID),
				elog.Int64("executionID", report.ID))
		}
	}

	// 记录处理统计信息
	jm.logger.Info("执行状态上报处理完成",
		elog.Int("total", len(reports)),
		elog.Int("processed", processedCount),
		elog.Int("skipped", skippedCount),
		elog.Int("channelBlocked", channelBlockedCount))
	return combinedErr
}

// sendReportWithTimeout 带超时的发送上报，避免阻塞
func (jm *Manager) sendReportWithTimeout(ctx context.Context, reportCh chan<- *domain.Report, report *domain.Report) error {
	// 创建超时上下文
	select {
	case reportCh <- report:
		return nil
	case <-time.After(jm.reportTimeout):
		return fmt.Errorf("发送超时: 通道可能已满")
	case <-ctx.Done():
		return ctx.Err()
	}
}
