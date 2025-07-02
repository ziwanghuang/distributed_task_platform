package dao

import (
	"context"
	"fmt"
	"time"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/errs"
	"github.com/ecodeclub/ekit/sqlx"
	"github.com/ego-component/egorm"
)

// TaskExecution 任务执行记录表DAO对象
type TaskExecution struct {
	Id int64 `gorm:"type:bigint;primaryKey;autoIncrement;"`
	// 下面都是创建当前 TaskExecution时从对应的Task直接拷贝过来的冗余信息
	TaskId             int64                               `gorm:"type:bigint;not null;comment:'任务ID'"`
	TaskName           string                              `gorm:"type:varchar(255);not null;comment:'任务名称'"`
	TaskCronExpr       string                              `gorm:"type:varchar(100);not null;comment:'cron表达式'"`
	TaskExecutorType   string                              `gorm:"type:ENUM('LOCAL', 'REMOTE');not null;default:'REMOTE';comment:'任务执行方式：LOCAL-本地执行，REMOTE-远程执行'"`
	TaskGrpcConfig     sqlx.JsonColumn[domain.GrpcConfig]  `gorm:"type:json;comment:'gRPC配置：{\"serviceName\": \"user-service\"}'"`
	TaskHttpConfig     sqlx.JsonColumn[domain.HttpConfig]  `gorm:"type:json;comment:'HTTP配置：{\"endpoint\": \"https://host:port/api\"}'"`
	TaskRetryConfig    sqlx.JsonColumn[domain.RetryConfig] `gorm:"type:json;comment:'重试配置'"`
	TaskVersion        int64                               `gorm:"type:bigint;not null;comment:'创建时Task的版本号'"`
	TaskScheduleNodeId string                              `gorm:"type:varchar(255);not null;comment:'创建此执行的调度节点ID'"`
	TaskScheduleParams sqlx.JsonColumn[map[string]string]  `gorm:"type:json;comment:'创建时Task的调度参数快照'"`

	// 下面这些是 TaskExecution 的自身信息
	Stime         int64  `gorm:"type:bigint;comment:'开始时间'"`
	Etime         int64  `gorm:"type:bigint;comment:'结束时间'"`
	RetryCount    int64  `gorm:"type:bigint;not null;default:0;comment:'已重试次数'"`
	NextRetryTime int64  `gorm:"type:bigint;comment:'下次重试时间'"`
	Status        string `gorm:"type:ENUM('PREPARE', 'RUNNING', 'FAILED_RETRYABLE', 'FAILED_PREEMPTED', 'FAILED', 'SUCCESS');not null;default:'PREPARE';comment:'执行状态: PREPARE-初始化(没有执行节点在执行）, RUNNING-执行中（有执行节点在执行）, FAILED_RETRYABLE-可重试失败, FAILED_PREEMPTED-因续约失败导致的抢占失败， FAILED-失败, SUCCESS-成功'"`
	Ctime         int64  `gorm:"comment:'创建时间'"`
	Utime         int64  `gorm:"comment:'更新时间'"`
}

// TableName 指定表名
func (TaskExecution) TableName() string {
	return "task_executions"
}

type TaskExecutionDAO interface {
	Create(ctx context.Context, execution TaskExecution) (TaskExecution, error)
	// GetByID 根据ID获取执行记录
	GetByID(ctx context.Context, id int64) (TaskExecution, error)
	// UpdateStatus 更新执行状态
	UpdateStatus(ctx context.Context, id int64, status string) error
	// FindRetryableExecutions 查找所有可以重试的执行记录
	// maxRetryCount: 最大重试次数限制
	// prepareTimeoutMs: PREPARE状态超时时间（毫秒），超过此时间未执行视为超时
	// limit: 查询结果数量限制
	FindRetryableExecutions(ctx context.Context, maxRetryCount int64, prepareTimeoutMs int64, limit int) ([]TaskExecution, error)
	// UpdateRetryResult 更新重试结果
	UpdateRetryResult(ctx context.Context, id, retryCount, nextRetryTime, endTime int64, status string) error
}

type GORMTaskExecutionDAO struct {
	db *egorm.Component
}

func NewGORMTaskExecutionDAO(db *egorm.Component) TaskExecutionDAO {
	return &GORMTaskExecutionDAO{db: db}
}

func (g *GORMTaskExecutionDAO) Create(ctx context.Context, execution TaskExecution) (TaskExecution, error) {
	now := time.Now().UnixMilli()
	execution.Utime, execution.Ctime = now, now

	// GORM的Create会自动填充ID到结构体中
	err := g.db.WithContext(ctx).Create(&execution).Error
	if err != nil {
		return TaskExecution{}, fmt.Errorf("创建执行记录失败: %v", err)
	}

	// 返回包含生成ID的实体
	return execution, nil
}

func (g *GORMTaskExecutionDAO) GetByID(ctx context.Context, id int64) (TaskExecution, error) {
	var execution TaskExecution
	err := g.db.WithContext(ctx).Where("id = ?", id).First(&execution).Error
	return execution, err
}

func (g *GORMTaskExecutionDAO) UpdateStatus(ctx context.Context, id int64, status string) error {
	result := g.db.WithContext(ctx).
		Model(&TaskExecution{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status": status,
			"utime":  time.Now().UnixMilli(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: ID=%d", errs.ErrExecutionNotFound, id)
	}
	return nil
}

func (g *GORMTaskExecutionDAO) FindRetryableExecutions(ctx context.Context, maxRetryCount int64, prepareTimeoutMs int64, limit int) ([]TaskExecution, error) {
	var executions []TaskExecution
	now := time.Now().UnixMilli()
	prepareTimeoutThreshold := now - prepareTimeoutMs

	// 复杂查询：查找可重试的执行记录
	// 场景1：PREPARE状态超时 - 调度失败需要重试
	// 场景2：FAILED_RETRYABLE状态 - 执行失败但可重试
	err := g.db.WithContext(ctx).
		Where("retry_count < ?", maxRetryCount).
		Where(`
			(status = 'PREPARE' 
			 AND ctime <= ? 
			 AND (next_retry_time IS NULL OR next_retry_time <= ?))
			OR 
			(status = 'FAILED_RETRYABLE' 
			 AND (next_retry_time IS NULL OR next_retry_time <= ?))
		`, prepareTimeoutThreshold, now, now).
		Order(`
			CASE 
				WHEN status = 'PREPARE' THEN ctime 
				ELSE COALESCE(next_retry_time, ctime)
			END ASC
		`).
		// 简化版排序（备选方案）：
		// Order("COALESCE(next_retry_time, ctime) ASC").
		Limit(limit).
		Find(&executions).Error

	return executions, err
}

func (g *GORMTaskExecutionDAO) UpdateRetryResult(ctx context.Context, id, retryCount, nextRetryTime, endTime int64, status string) error {
	result := g.db.WithContext(ctx).
		Model(&TaskExecution{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"retry_count":     retryCount,
			"next_retry_time": nextRetryTime,
			"status":          status,
			"etime":           endTime,
			"utime":           time.Now().UnixMilli(),
		})

	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: ID=%d", errs.ErrExecutionNotFound, id)
	}
	return nil
}
