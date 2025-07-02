package job

import (
	"context"
	"fmt"
	"time"

	executorv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/executor/v1"
	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"gitee.com/flycash/distributed_task_platform/pkg/grpc"
	"github.com/ecodeclub/ekit/syncx"
	"github.com/gotomicro/ego/core/elog"
)

// Manager Job管理器，负责Job的创建、管理、生命周期跟踪
type Manager struct {
	jobs         syncx.Map[int64, *Chans]                        // 正在运行的Job通道
	svc          task.ExecutionService                           // 执行服务
	grpcClients  *grpc.Clients[executorv1.ExecutorServiceClient] // gRPC客户端池
	pollInterval time.Duration

	logger *elog.Component
}

// NewManager 创建 Manager 实例
func NewManager(
	svc task.ExecutionService,
	grpcClients *grpc.Clients[executorv1.ExecutorServiceClient],
	pollInterval time.Duration,
) *Manager {
	return &Manager{
		svc:          svc,
		grpcClients:  grpcClients,
		pollInterval: pollInterval,
		logger:       elog.DefaultLogger.With(elog.FieldComponentName("job.Manager")),
	}
}

// Run 启动任务执行，返回通道和清理函数
func (jm *Manager) Run(ctx context.Context, task domain.Task) (*Chans, func()) {
	j := NewRemoteJob(jm.svc, jm.grpcClients, jm.pollInterval)

	// 启动任务，立即获得通道
	chans := j.Run(ctx, task)

	// 立即存储任务信息
	jm.jobs.Store(task.ID, chans)

	// 返回cleanup函数
	cleanup := func() {
		if _, loaded := jm.jobs.LoadAndDelete(task.ID); loaded {
			jm.logger.Debug("清理任务缓存", elog.Int64("taskID", task.ID))
		}
	}

	return chans, cleanup
}

// RenewFailed 处理续约失败
func (jm *Manager) RenewFailed(_ context.Context, task domain.Task) error {
	if entry, ok := jm.jobs.Load(task.ID); ok {
		if entry != nil {
			entry.Renew <- false // 续约失败发送false
			return nil
		}
		jm.logger.Warn("忽略续约事件",
			elog.String("jobName", task.Name),
			elog.Any("task", task),
		)
	}
	return nil
}

// OnReport 处理执行节点主动上报的任务执行状态
func (jm *Manager) OnReport(_ context.Context, report *domain.Report) error {
	if entry, exists := jm.jobs.Load(report.TaskID); exists {
		if entry != nil {
			entry.Report <- report
		} else {
			jm.logger.Warn("忽略上报进度事件",
				elog.String("jobName", report.TaskName),
				elog.Any("report", report),
			)
		}
	}
	return fmt.Errorf("task %d not found", report.TaskID)
}
