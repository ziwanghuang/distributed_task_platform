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

type entry struct {
	job   Job
	task  domain.Task
	chans *Chans
}

// Manager Job管理器，负责Job的创建、管理、生命周期跟踪
type Manager struct {
	jobs         syncx.Map[int64, entry]                         // 正在运行的Job
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

// Run 异步执行任务，返回结果channel
func (jm *Manager) Run(ctx context.Context, task domain.Task) <-chan error {
	j := NewRemoteJob(jm.svc, jm.grpcClients, jm.pollInterval)
	// 异步执行
	ch := make(chan error, 1)
	go func() {
		defer func() {
			jm.jobs.Delete(task.ID) // 清理
			close(ch)
		}()

		chs, err := j.Run(ctx, task)

		jm.jobs.Store(task.ID, entry{
			job:   j,
			task:  task,
			chans: chs,
		})

		ch <- err
	}()
	return ch
}

// RenewFailed 处理续约失败
func (jm *Manager) RenewFailed(_ context.Context, task domain.Task) error {
	if entry, ok := jm.jobs.Load(task.ID); ok {
		if entry.chans != nil {
			entry.chans.Renew <- true
			return nil
		}
		jm.logger.Warn("忽略续约事件",
			elog.String("jobName", entry.job.Name()),
			elog.Any("task", task),
		)
	}
	return nil
}

// OnReport 处理执行节点主动上报的任务执行状态
func (jm *Manager) OnReport(_ context.Context, report *domain.Report) error {
	if entry, exists := jm.jobs.Load(report.TaskID); exists {
		if entry.chans != nil {
			entry.chans.Report <- report
		} else {
			jm.logger.Warn("忽略上报进度事件",
				elog.String("jobName", entry.job.Name()),
				elog.Any("report", report),
			)
		}
	}
	return fmt.Errorf("task %d not found", report.TaskID)
}
