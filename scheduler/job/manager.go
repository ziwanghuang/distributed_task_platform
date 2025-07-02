package job

import (
	"context"
	"fmt"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/errs"
	"github.com/ecodeclub/ekit/syncx"
	"github.com/gotomicro/ego/core/elog"
)

// Manager Job管理器，负责Job的创建、管理、生命周期跟踪
type Manager struct {
	taskChans syncx.Map[int64, *Chans] // 正在运行的task的通道
	jobs      map[string]Job
	logger    *elog.Component
}

// NewManager 创建 Manager 实例
func NewManager(
	jobs map[string]Job,
) *Manager {
	return &Manager{
		jobs:   jobs,
		logger: elog.DefaultLogger.With(elog.FieldComponentName("job.Manager")),
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
	for i := range reports {
		if chans, exists := jm.taskChans.Load(reports[i].TaskID); exists {
			if chans != nil {
				select {
				case chans.Report <- reports[i]:
					// 发送成功
				case <-ctx.Done():
					return ctx.Err()
				default:
					// 通道满了，记录警告但继续处理
					jm.logger.Warn("上报通道满", elog.Int64("taskID", reports[i].TaskID))
				}
			}
		} else {
			jm.logger.Debug("任务不存在", elog.Int64("taskID", reports[i].TaskID))
		}
	}
	return nil
}
