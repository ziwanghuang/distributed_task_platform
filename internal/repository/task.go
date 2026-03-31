// Package repository 是数据仓库层，封装了领域模型（domain）与数据访问对象（DAO）之间的转换逻辑。
//
// 本包遵循 DDD（领域驱动设计）的仓库模式：
//   - 上层（Service）只接触 domain 领域模型
//   - 下层（DAO）只接触数据库表模型
//   - Repository 负责两者之间的双向转换（toEntity / toDomain）
//
// 主要仓库接口：
//   - TaskRepository: 任务定义的持久化操作（CRUD、CAS 抢占、续约、释放等）
//   - TaskExecutionRepository: 执行记录的持久化操作（创建、状态更新、各类查询等）
package repository

import (
	"context"
	"database/sql"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/repository/dao"
	"gitee.com/flycash/distributed_task_platform/pkg/sqlx"
	"github.com/ecodeclub/ekit/slice"
)

// TaskRepository 定义了任务定义层面的数据仓库接口。
//
// 所有方法的返回值都是 domain.Task 领域模型，调用方无需感知底层数据库表结构。
// 关键操作说明：
//   - Acquire/Release/Renew: 实现了基于 CAS（Compare-And-Swap）的分布式任务抢占机制
//   - UpdateNextTime/UpdateScheduleParams: 通过乐观锁（Version）保证并发安全
type TaskRepository interface {
	Create(ctx context.Context, task domain.Task) (domain.Task, error)
	GetByID(ctx context.Context, id int64) (domain.Task, error)
	// SchedulableTasks 查询可调度任务。条件：NextTime 已到 + 状态为 ACTIVE，
	// 或状态为 PREEMPTED 但超时未续约（疑似僵尸任务）。
	SchedulableTasks(ctx context.Context, preemptedTimeoutMs int64, limit int) ([]domain.Task, error)
	// Acquire CAS 抢占任务。通过 version 乐观锁将状态从 ACTIVE 改为 PREEMPTED，
	// 同时记录 scheduleNodeID。失败返回 ErrTaskPreemptFailed。
	Acquire(ctx context.Context, id, version int64, scheduleNodeID string) (domain.Task, error)
	// Release 释放任务抢占。将状态从 PREEMPTED 改回 ACTIVE，清除 scheduleNodeID。
	Release(ctx context.Context, id int64, scheduleNodeID string) (domain.Task, error)
	// Renew 批量续约。更新当前节点抢占的所有任务的 utime 和 version，
	// 防止被其他节点判定为僵尸任务。
	Renew(ctx context.Context, scheduleNodeID string) error
	// UpdateNextTime 更新下次执行时间（乐观锁 CAS）。
	UpdateNextTime(ctx context.Context, id, version, nextTime int64) (domain.Task, error)
	// UpdateScheduleParams 更新调度参数（乐观锁 CAS）。
	UpdateScheduleParams(ctx context.Context, id, version int64, scheduleParams map[string]string) (domain.Task, error)
	// FindByPlanID 查询指定 Plan 下的所有子任务。
	FindByPlanID(ctx context.Context, planID int64) ([]domain.Task, error)
}

// taskRepository 是 TaskRepository 接口的默认实现。
// 内部持有 TaskDAO，负责 domain.Task ↔ dao.Task 的双向转换。
type taskRepository struct {
	dao dao.TaskDAO
}

// NewTaskRepository 创建任务仓库实例。
func NewTaskRepository(taskDAO dao.TaskDAO) TaskRepository {
	return &taskRepository{dao: taskDAO}
}

func (r *taskRepository) FindByPlanID(ctx context.Context, planID int64) ([]domain.Task, error) {
	daoTasks, err := r.dao.FindByPlanID(ctx, planID)
	if err != nil {
		return nil, err
	}
	return slice.Map(daoTasks, func(_ int, src *dao.Task) domain.Task {
		return r.toDomain(src)
	}), nil
}

func (r *taskRepository) Create(ctx context.Context, task domain.Task) (domain.Task, error) {
	created, err := r.dao.Create(ctx, r.toEntity(task))
	if err != nil {
		return domain.Task{}, err
	}
	return r.toDomain(created), nil
}

func (r *taskRepository) GetByID(ctx context.Context, id int64) (domain.Task, error) {
	daoTask, err := r.dao.GetByID(ctx, id)
	if err != nil {
		return domain.Task{}, err
	}
	return r.toDomain(daoTask), nil
}

func (r *taskRepository) SchedulableTasks(ctx context.Context, preemptedTimeoutMs int64, limit int) ([]domain.Task, error) {
	tasks, err := r.dao.FindSchedulableTasks(ctx, preemptedTimeoutMs, limit)
	if err != nil {
		return nil, err
	}
	return slice.Map(tasks, func(_ int, src *dao.Task) domain.Task {
		return r.toDomain(src)
	}), nil
}

func (r *taskRepository) Acquire(ctx context.Context, id, version int64, scheduleNodeID string) (domain.Task, error) {
	task, err := r.dao.Acquire(ctx, id, version, scheduleNodeID)
	if err != nil {
		return domain.Task{}, err
	}
	return r.toDomain(task), nil
}

func (r *taskRepository) Release(ctx context.Context, id int64, scheduleNodeID string) (domain.Task, error) {
	task, err := r.dao.Release(ctx, id, scheduleNodeID)
	if err != nil {
		return domain.Task{}, err
	}
	return r.toDomain(task), nil
}

func (r *taskRepository) Renew(ctx context.Context, scheduleNodeID string) error {
	return r.dao.Renew(ctx, scheduleNodeID)
}

func (r *taskRepository) UpdateNextTime(ctx context.Context, id, version, nextTime int64) (domain.Task, error) {
	task, err := r.dao.UpdateNextTime(ctx, id, version, nextTime)
	if err != nil {
		return domain.Task{}, err
	}
	return r.toDomain(task), nil
}

func (r *taskRepository) UpdateScheduleParams(ctx context.Context, id, version int64, scheduleParams map[string]string) (domain.Task, error) {
	task, err := r.dao.UpdateScheduleParams(ctx, id, version, scheduleParams)
	if err != nil {
		return domain.Task{}, err
	}
	return r.toDomain(task), nil
}

// toEntity 将 domain.Task 领域模型转换为 dao.Task 数据库模型。
// 转换重点：
//   - 可选字段（GrpcConfig/HTTPConfig/RetryConfig 等指针类型）转为 sqlx.JSONColumn（带 Valid 标记）
//   - ScheduleNodeID 从 string 转为 sql.NullString
//   - 枚举类型（ExecutionMethod/SchedulingStrategy/Status）转为字符串
func (r *taskRepository) toEntity(task domain.Task) dao.Task {
	var scheduleNodeID sql.NullString
	if task.ScheduleNodeID != "" {
		scheduleNodeID = sql.NullString{String: task.ScheduleNodeID, Valid: true}
	}

	var grpcConfig sqlx.JSONColumn[domain.GrpcConfig]
	if task.GrpcConfig != nil {
		grpcConfig = sqlx.JSONColumn[domain.GrpcConfig]{Val: *task.GrpcConfig, Valid: true}
	}

	var httpConfig sqlx.JSONColumn[domain.HTTPConfig]
	if task.HTTPConfig != nil {
		httpConfig = sqlx.JSONColumn[domain.HTTPConfig]{Val: *task.HTTPConfig, Valid: true}
	}

	var retryConfig sqlx.JSONColumn[domain.RetryConfig]
	if task.RetryConfig != nil {
		retryConfig = sqlx.JSONColumn[domain.RetryConfig]{Val: *task.RetryConfig, Valid: true}
	}

	var scheduleParams sqlx.JSONColumn[map[string]string]
	if task.ScheduleParams != nil {
		scheduleParams = sqlx.JSONColumn[map[string]string]{Val: task.ScheduleParams, Valid: true}
	}

	var shardingRule sqlx.JSONColumn[domain.ShardingRule]
	if task.ShardingRule != nil {
		shardingRule = sqlx.JSONColumn[domain.ShardingRule]{Val: *task.ShardingRule, Valid: true}
	}

	return dao.Task{
		ID:                  task.ID,
		Name:                task.Name,
		CronExpr:            task.CronExpr,
		PlanID:              task.PlanID,
		ExecutionMethod:     task.ExecutionMethod.String(),
		SchedulingStrategy:  task.SchedulingStrategy.String(),
		GrpcConfig:          grpcConfig,
		HTTPConfig:          httpConfig,
		RetryConfig:         retryConfig,
		ScheduleParams:      scheduleParams,
		ShardingRule:        shardingRule,
		MaxExecutionSeconds: task.MaxExecutionSeconds,
		ScheduleNodeID:      scheduleNodeID,
		NextTime:            task.NextTime,
		Status:              task.Status.String(),
		Version:             task.Version,
		Ctime:               task.CTime,
		Utime:               task.UTime,
	}
}

// toDomain 将 dao.Task 数据库模型转换为 domain.Task 领域模型。
// 转换重点：
//   - sqlx.JSONColumn（带 Valid 标记）转为 Go 指针类型（nil 表示未配置）
//   - sql.NullString 转为普通 string
//   - 字符串枚举转为 domain 包中的强类型枚举
func (r *taskRepository) toDomain(daoTask *dao.Task) domain.Task {
	var scheduleNodeID string
	if daoTask.ScheduleNodeID.Valid {
		scheduleNodeID = daoTask.ScheduleNodeID.String
	}

	var grpcConfig *domain.GrpcConfig
	if daoTask.GrpcConfig.Valid {
		grpcConfig = &daoTask.GrpcConfig.Val
	}

	var httpConfig *domain.HTTPConfig
	if daoTask.HTTPConfig.Valid {
		httpConfig = &daoTask.HTTPConfig.Val
	}

	var retryConfig *domain.RetryConfig
	if daoTask.RetryConfig.Valid {
		retryConfig = &daoTask.RetryConfig.Val
	}

	var scheduleParams map[string]string
	if daoTask.ScheduleParams.Valid {
		scheduleParams = daoTask.ScheduleParams.Val
	}
	var shardingRule *domain.ShardingRule
	if daoTask.ShardingRule.Valid {
		shardingRule = &daoTask.ShardingRule.Val
	}

	return domain.Task{
		ID:                  daoTask.ID,
		Name:                daoTask.Name,
		CronExpr:            daoTask.CronExpr,
		PlanID:              daoTask.PlanID,
		ExecExpr:            daoTask.ExecExpr,
		Type:                domain.TaskType(daoTask.Type),
		ExecutionMethod:     domain.TaskExecutionMethod(daoTask.ExecutionMethod),
		SchedulingStrategy:  domain.SchedulingStrategy(daoTask.SchedulingStrategy),
		GrpcConfig:          grpcConfig,
		HTTPConfig:          httpConfig,
		RetryConfig:         retryConfig,
		MaxExecutionSeconds: daoTask.MaxExecutionSeconds,
		ScheduleParams:      scheduleParams,
		ShardingRule:        shardingRule,
		ScheduleNodeID:      scheduleNodeID,
		NextTime:            daoTask.NextTime,
		Status:              domain.TaskStatus(daoTask.Status),
		Version:             daoTask.Version,
		UTime:               daoTask.Utime,
		CTime:               daoTask.Ctime,
	}
}
