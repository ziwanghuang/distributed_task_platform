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
	"go.uber.org/multierr"
)

// Manager Job管理器，负责Job的创建、管理、生命周期跟踪
type Manager struct {
	jobs         syncx.Map[int64, Job]                           // 正在运行的Job
	svc          task.ExecutionService                           // 执行服务
	grpcClients  *grpc.Clients[executorv1.ExecutorServiceClient] // gRPC客户端池
	pollInterval time.Duration
}

// NewTaskManager 创建TaskManager实例
func NewTaskManager(
	svc task.ExecutionService,
	grpcClients *grpc.Clients[executorv1.ExecutorServiceClient],
	pollInterval time.Duration,
) *Manager {
	return &Manager{
		svc:          svc,
		grpcClients:  grpcClients,
		pollInterval: pollInterval,
	}
}

// Run 异步执行任务，返回结果channel
func (jm *Manager) Run(ctx context.Context, task domain.Task) <-chan error {
	j := NewRemoteJob(jm.svc, jm.grpcClients, jm.pollInterval)
	jm.jobs.Store(task.ID, j)

	// 异步执行
	ch := make(chan error, 1)
	go func() {
		defer func() {
			jm.jobs.Delete(task.ID) // 清理
			close(ch)
		}()
		err := j.Run(ctx, task)
		ch <- err
	}()
	return ch
}

// Stop 停止指定的任务
func (jm *Manager) Stop(ctx context.Context, task domain.Task) error {
	if job, ok := jm.jobs.Load(task.ID); ok {
		return job.Stop(ctx)
	}
	return fmt.Errorf("task %d not found", task.ID)
}

// RenewFailed 处理续约失败
func (jm *Manager) RenewFailed(ctx context.Context, task domain.Task) error {
	if job, ok := jm.jobs.Load(task.ID); ok {
		return job.HandleRenewFailure(ctx)
	}
	return nil
}

// HandleReport 处理执行节点主动上报的任务执行状态
func (jm *Manager) HandleReport(ctx context.Context, taskID int64, report *domain.Report) error {
	if job, exists := jm.jobs.Load(taskID); exists {
		return job.HandleReport(ctx, report)
	}
	return fmt.Errorf("task %d not found", taskID)
}

// Close 关闭管理器，停止所有正在运行的Job
func (jm *Manager) Close() error {
	var err error
	jm.jobs.Range(func(taskID int64, job Job) bool {
		if err1 := job.Stop(context.Background()); err1 != nil {
			err = multierr.Append(err, fmt.Errorf("stop task %d failed: %w", taskID, err))
		}
		return true
	})
	return err
}
