package dao

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/errs"
	"github.com/ecodeclub/ekit/sqlx"
	"github.com/ego-component/egorm"
)

// Task 任务表DAO对象
type Task struct {
	Id             int64                               `gorm:"type:bigint;primaryKey;autoIncrement;"`
	Name           string                              `gorm:"type:varchar(255);not null;uniqueIndex:uniq_idx_name;comment:'任务名称'"`
	CronExpr       string                              `gorm:"type:varchar(100);not null;comment:'cron表达式'"`
	ExecutorType   string                              `gorm:"type:ENUM('LOCAL', 'REMOTE');not null;default:'REMOTE';comment:'任务执行方式：LOCAL-本地执行，REMOTE-远程执行'"`
	GrpcConfig     sqlx.JsonColumn[domain.GrpcConfig]  `gorm:"type:json;comment:'gRPC配置：{\"serviceName\": \"user-service\"}'"`
	HttpConfig     sqlx.JsonColumn[domain.HttpConfig]  `gorm:"type:json;comment:'HTTP配置：{\"endpoint\": \"https://host:port/api\"}'"`
	RetryConfig    sqlx.JsonColumn[domain.RetryConfig] `gorm:"type:json;comment:'重试配置'"`
	ScheduleParams sqlx.JsonColumn[map[string]string]  `gorm:"type:json;comment:'调度参数'"`
	ScheduleNodeId sql.NullString                      `gorm:"type:varchar(255);comment:'当前抢占的调度节点ID'"`
	NextTime       int64                               `gorm:"type:bigint;not null;index:idx_next_time_status;comment:'下次执行时间'"`
	Status         string                              `gorm:"type:ENUM('ACTIVE', 'PREEMPTED', 'INACTIVE');not null;default:'ACTIVE';index:idx_next_time_status;comment:'任务状态: ACTIVE-可调度, PREEMPTED-已抢占, INACTIVE-停止执行。处于INACTIVE也可以被再次 ACTIVE'"`
	Version        int64                               `gorm:"type:bigint;not null;default:1;comment:'版本号，用于乐观锁'"`
	Ctime          int64                               `gorm:"comment:'创建时间'"`
	Utime          int64                               `gorm:"comment:'更新时间'"`
}

// TableName 指定表名
func (Task) TableName() string {
	return "tasks"
}

type TaskDAO interface {
	// Create 创建任务
	Create(ctx context.Context, task Task) (Task, error)
	// GetByID 根据ID获取任务
	GetByID(ctx context.Context, id int64) (Task, error)
	// FindSchedulableTasks 查询可调度的任务列表
	// preemptedTimeoutMs: PREEMPTED状态任务的超时时间（毫秒），超过此时间未续约的任务可被重新抢占
	FindSchedulableTasks(ctx context.Context, preemptedTimeoutMs int64, limit int) ([]Task, error)
	// Preempt 抢占任务（CAS操作）
	Preempt(ctx context.Context, id, version int64, scheduleNodeId string) error
	// Renew 续约任务（CAS操作）
	Renew(ctx context.Context, id, version int64, scheduleNodeId string) error
	// Release 释放任务，更新状态为ACTIVE
	Release(ctx context.Context, id, version int64, scheduleNodeId string) error
	// UpdateNextTime 更新下一次执行时间
	UpdateNextTime(ctx context.Context, id, version, nextTime int64) error
	// UpdateScheduleParams 更新调度参数（CAS操作）
	UpdateScheduleParams(ctx context.Context, id, version int64, scheduleParams map[string]string) error
}

type GORMTaskDAO struct {
	db *egorm.Component
}

func NewGORMTaskDAO(db *egorm.Component) TaskDAO {
	return &GORMTaskDAO{db: db}
}

func (g *GORMTaskDAO) Create(ctx context.Context, task Task) (Task, error) {
	now := time.Now().UnixMilli()
	task.Utime, task.Ctime = now, now
	// GORM的Create会自动填充ID到结构体中
	err := g.db.WithContext(ctx).Create(&task).Error
	if err != nil {
		return Task{}, fmt.Errorf("创建任务失败: %v", err)
	}
	return task, nil
}

func (g *GORMTaskDAO) GetByID(ctx context.Context, id int64) (Task, error) {
	var task Task
	err := g.db.WithContext(ctx).Where("id = ?", id).First(&task).Error
	return task, err
}

func (g *GORMTaskDAO) FindSchedulableTasks(ctx context.Context, preemptedTimeoutMs int64, limit int) ([]Task, error) {
	var tasks []Task
	now := time.Now().UnixMilli()
	// 获取所有可调度的任务
	// 1. ACTIVE 状态且到了执行时间的任务
	// 2. PREEMPTED 状态但超时未续约的任务（疑似僵尸任务）
	err := g.db.WithContext(ctx).
		Where("next_time <= ? AND (status = ? OR (status = ? AND utime <= ?))",
			now, "ACTIVE", "PREEMPTED", now-preemptedTimeoutMs).
		Limit(limit).
		Find(&tasks).Error
	return tasks, err
}

func (g *GORMTaskDAO) Preempt(ctx context.Context, id, version int64, scheduleNodeId string) error {
	now := time.Now().UnixMilli()
	result := g.db.WithContext(ctx).
		Model(&Task{}).
		Where("id = ? AND version = ?", id, version).
		Updates(map[string]any{
			"status":           "PREEMPTED",
			"schedule_node_id": scheduleNodeId,
			"version":          version + 1,
			"utime":            now,
		})

	if result.Error != nil {
		return fmt.Errorf("%w: 数据库操作失败: %v", errs.ErrTaskPreemptFailed, result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: 版本不匹配或任务不存在", errs.ErrTaskPreemptFailed)
	}

	return nil
}

func (g *GORMTaskDAO) Renew(ctx context.Context, id, version int64, scheduleNodeId string) error {
	result := g.db.WithContext(ctx).
		Model(&Task{}).
		Where("id = ? AND version = ? AND schedule_node_id = ?", id, version, scheduleNodeId).
		Updates(map[string]any{
			"version": version + 1,
			"utime":   time.Now().UnixMilli(),
		})
	if result.Error != nil {
		return fmt.Errorf("%w: 数据库操作失败: %v", errs.ErrTaskRenewFailed, result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: 版本不匹配、节点不匹配或任务不存在", errs.ErrTaskRenewFailed)
	}

	return nil
}

func (g *GORMTaskDAO) Release(ctx context.Context, id, version int64, scheduleNodeId string) error {
	result := g.db.WithContext(ctx).
		Model(&Task{}).
		Where("id = ? AND version = ? AND schedule_node_id = ?", id, version, scheduleNodeId).
		Updates(map[string]any{
			"status":           "ACTIVE",
			"schedule_node_id": nil,
			"version":          version + 1,
			"utime":            time.Now().UnixMilli(),
		})
	if result.Error != nil {
		return fmt.Errorf("%w: 数据库操作失败: %v", errs.ErrTaskReleaseFailed, result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: 版本不匹配或任务不存在", errs.ErrTaskReleaseFailed)
	}
	return nil
}

func (g *GORMTaskDAO) UpdateNextTime(ctx context.Context, id, version, nextTime int64) error {
	result := g.db.WithContext(ctx).
		Model(&Task{}).
		Where("id = ? AND version = ?", id, version).
		Updates(map[string]any{
			"next_time": nextTime,
			"version":   version + 1,
			"utime":     time.Now().UnixMilli(),
		})
	if result.Error != nil {
		return fmt.Errorf("%w: 数据库操作失败: %v", errs.ErrTaskUpdateNextTimeFailed, result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: 版本不匹配或任务不存在", errs.ErrTaskUpdateNextTimeFailed)
	}
	return nil
}

func (g *GORMTaskDAO) UpdateScheduleParams(ctx context.Context, id, version int64, scheduleParams map[string]string) error {
	result := g.db.WithContext(ctx).
		Model(&Task{}).
		Where("id = ? AND version = ?", id, version).
		Updates(map[string]any{
			"schedule_params": sqlx.JsonColumn[map[string]string]{Val: scheduleParams, Valid: scheduleParams != nil},
			"version":         version + 1,
			"utime":           time.Now().UnixMilli(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: 版本不匹配或任务不存在", errs.ErrTaskUpdateScheduleParamsFailed)
	}
	return nil
}
