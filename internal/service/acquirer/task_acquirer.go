package acquirer

import (
	"context"

	"gitee.com/flycash/distributed_task_platform/internal/repository"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
)

var _ TaskAcquirer = &MySQLTaskAcquirer{}

// TaskAcquirer 任务抢占接口，定义了分布式任务调度中的三个核心操作。
// 基于 CAS（Compare-And-Swap）乐观锁实现分布式互斥，
// 通过数据库版本号保证多个调度节点不会重复抢占同一个任务。
type TaskAcquirer interface {
	// Acquire 抢占指定任务。
	// 底层通过 CAS 更新：WHERE id=taskID AND version=version
	// 只有版本号匹配时才能抢占成功，自然解决多节点竞争问题。
	Acquire(ctx context.Context, taskID, version int64, scheduleNodeID string) (domain.Task, error)
	// Release 释放指定任务的抢占锁，让任务重新进入可调度状态。
	// 在任务执行完成、执行失败需要重新调度、或优雅停机时调用。
	Release(ctx context.Context, taskID int64, scheduleNodeID string) error
	// Renew 续约当前节点抢占的所有任务。
	// 防止长时间执行的任务因超时被 InterruptCompensator 误判为卡死。
	// 续约间隔应小于任务超时时间的一半。
	Renew(ctx context.Context, scheduleNodeID string) error
}

// MySQLTaskAcquirer 基于 MySQL 实现的 TaskAcquirer。
// 利用 MySQL 的行锁和版本号（乐观锁）实现分布式任务抢占。
type MySQLTaskAcquirer struct {
	taskRepo repository.TaskRepository
}

// NewTaskAcquirer 创建TaskAcquirer实例
func NewTaskAcquirer(taskRepo repository.TaskRepository) *MySQLTaskAcquirer {
	return &MySQLTaskAcquirer{
		taskRepo: taskRepo,
	}
}

// Acquire 抢占指定任务，返回抢占后的任务信息
func (t *MySQLTaskAcquirer) Acquire(ctx context.Context, taskID, version int64, scheduleNodeID string) (domain.Task, error) {
	tk, err := t.taskRepo.Acquire(ctx, taskID, version, scheduleNodeID)
	if err != nil {
		return domain.Task{}, err
	}
	return tk, nil
}

// Release 释放指定任务
func (t *MySQLTaskAcquirer) Release(ctx context.Context, taskID int64, scheduleNodeID string) error {
	_, err := t.taskRepo.Release(ctx, taskID, scheduleNodeID)
	return err
}

// Renew 续约指定任务，返回续约后的任务信息
func (t *MySQLTaskAcquirer) Renew(ctx context.Context, scheduleNodeID string) error {
	return t.taskRepo.Renew(ctx, scheduleNodeID)
}
