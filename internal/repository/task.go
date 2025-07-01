package repository

import (
	"context"
	"database/sql"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/repository/dao"
	"github.com/ecodeclub/ekit/slice"
	"github.com/ecodeclub/ekit/sqlx"
)

type TaskRepository interface {
	// Create 创建任务
	Create(ctx context.Context, task domain.Task) (domain.Task, error)
	// SchedulableTasks 获取可调度的任务列表，preemptedTimeoutMs 表示处于 PREEMPTED 状态任务的超时时间（毫秒）
	SchedulableTasks(ctx context.Context, preemptedTimeoutMs int64, limit int) ([]domain.Task, error)
	// Acquire 抢占任务
	Acquire(ctx context.Context, task domain.Task) error
	// Release 释放任务
	Release(ctx context.Context, task domain.Task) error
	// Renew 续约任务
	Renew(ctx context.Context, task domain.Task) error
	// UpdateNextTime 更新任务的下次执行时间
	UpdateNextTime(ctx context.Context, id, version, nextTime int64) error
}

type taskRepository struct {
	dao dao.TaskDAO
}

func NewTaskRepository(taskDAO dao.TaskDAO) TaskRepository {
	return &taskRepository{dao: taskDAO}
}

func (r *taskRepository) Create(ctx context.Context, task domain.Task) (domain.Task, error) {
	created, err := r.dao.Create(ctx, r.toEntity(task))
	if err != nil {
		return domain.Task{}, err
	}
	return r.toDomain(created), nil
}

func (r *taskRepository) SchedulableTasks(ctx context.Context, preemptedTimeoutMs int64, limit int) ([]domain.Task, error) {
	tasks, err := r.dao.FindSchedulableTasks(ctx, preemptedTimeoutMs, limit)
	if err != nil {
		return nil, err
	}
	return slice.Map(tasks, func(idx int, src dao.Task) domain.Task {
		return r.toDomain(src)
	}), nil
}

func (r *taskRepository) Acquire(ctx context.Context, task domain.Task) error {
	return r.dao.Preempt(ctx, task.ID, task.Version, task.ScheduleNodeID)
}

func (r *taskRepository) Release(ctx context.Context, task domain.Task) error {
	return r.dao.Release(ctx, task.ID, task.Version, task.ScheduleNodeID)
}

func (r *taskRepository) Renew(ctx context.Context, task domain.Task) error {
	return r.dao.Renew(ctx, task.ID, task.Version, task.ScheduleNodeID)
}

func (r *taskRepository) UpdateNextTime(ctx context.Context, id, version, nextTime int64) error {
	return r.dao.UpdateNextTime(ctx, id, version, nextTime)
}

// toEntity 将领域模型转换为DAO模型
func (r *taskRepository) toEntity(task domain.Task) dao.Task {
	var scheduleNodeId sql.NullString
	if task.ScheduleNodeID != "" {
		scheduleNodeId = sql.NullString{String: task.ScheduleNodeID, Valid: true}
	}

	var grpcConfig sqlx.JsonColumn[domain.GrpcConfig]
	if task.GrpcConfig != nil {
		grpcConfig = sqlx.JsonColumn[domain.GrpcConfig]{Val: *task.GrpcConfig, Valid: true}
	}

	var httpConfig sqlx.JsonColumn[domain.HttpConfig]
	if task.HttpConfig != nil {
		httpConfig = sqlx.JsonColumn[domain.HttpConfig]{Val: *task.HttpConfig, Valid: true}
	}

	var retryConfig sqlx.JsonColumn[domain.RetryConfig]
	if task.RetryConfig != nil {
		retryConfig = sqlx.JsonColumn[domain.RetryConfig]{Val: *task.RetryConfig, Valid: true}
	}

	return dao.Task{
		Id:             task.ID,
		Name:           task.Name,
		CronExpr:       task.CronExpr,
		ExecutorType:   task.ExecutorType.String(),
		GrpcConfig:     grpcConfig,
		HttpConfig:     httpConfig,
		RetryConfig:    retryConfig,
		ScheduleNodeId: scheduleNodeId,
		NextTime:       task.NextTime,
		Status:         task.Status.String(),
		Version:        task.Version,
		Ctime:          task.CTime,
		Utime:          task.Utime,
	}
}

// toDomain 将DAO模型转换为领域模型
func (r *taskRepository) toDomain(daoTask dao.Task) domain.Task {
	var scheduleNodeId string
	if daoTask.ScheduleNodeId.Valid {
		scheduleNodeId = daoTask.ScheduleNodeId.String
	}

	var grpcConfig *domain.GrpcConfig
	if daoTask.GrpcConfig.Valid {
		grpcConfig = &daoTask.GrpcConfig.Val
	}

	var httpConfig *domain.HttpConfig
	if daoTask.HttpConfig.Valid {
		httpConfig = &daoTask.HttpConfig.Val
	}

	var retryConfig *domain.RetryConfig
	if daoTask.RetryConfig.Valid {
		retryConfig = &daoTask.RetryConfig.Val
	}

	return domain.Task{
		ID:             daoTask.Id,
		Name:           daoTask.Name,
		CronExpr:       daoTask.CronExpr,
		ExecutorType:   domain.TaskExecutorType(daoTask.ExecutorType),
		GrpcConfig:     grpcConfig,
		HttpConfig:     httpConfig,
		RetryConfig:    retryConfig,
		ScheduleNodeID: scheduleNodeId,
		NextTime:       daoTask.NextTime,
		Status:         domain.TaskStatus(daoTask.Status),
		Version:        daoTask.Version,
		Utime:          daoTask.Utime,
		CTime:          daoTask.Ctime,
	}
}
