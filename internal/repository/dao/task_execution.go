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

const (
	TaskExecutionStatusPrepare           = "PREPARE"
	TaskExecutionStatusRunning           = "RUNNING"
	TaskExecutionStatusFailedRetryable   = "FAILED_RETRYABLE"
	TaskExecutionStatusFailedRescheduled = "FAILED_RESCHEDULED"
)

// TaskExecution 任务执行记录表DAO对象
type TaskExecution struct {
	ID int64 `gorm:"type:bigint;primaryKey;autoIncrement;"`
	// 下面都是创建当前 TaskExecution时从对应的Task直接拷贝过来的冗余信息
	TaskID              int64                               `gorm:"type:bigint;not null;comment:'任务ID'"`
	TaskName            string                              `gorm:"type:varchar(255);not null;comment:'任务名称'"`
	TaskCronExpr        string                              `gorm:"type:varchar(100);not null;comment:'cron表达式'"`
	TaskExecutionMethod string                              `gorm:"type:ENUM('LOCAL', 'REMOTE');not null;default:'REMOTE';comment:'任务执行方式：LOCAL-本地执行，REMOTE-远程执行'"`
	TaskGrpcConfig      sqlx.JsonColumn[domain.GrpcConfig]  `gorm:"type:json;comment:'gRPC配置：{\"serviceName\": \"user-service\"}'"`
	TaskHTTPConfig      sqlx.JsonColumn[domain.HTTPConfig]  `gorm:"type:json;comment:'HTTP配置：{\"endpoint\": \"https://host:port/api\"}'"`
	TaskRetryConfig     sqlx.JsonColumn[domain.RetryConfig] `gorm:"type:json;comment:'重试配置'"`
	TaskVersion         int64                               `gorm:"type:bigint;not null;comment:'创建时Task的版本号'"`
	TaskScheduleNodeID  string                              `gorm:"type:varchar(255);not null;comment:'创建此执行的调度节点ID'"`
	TaskScheduleParams  sqlx.JsonColumn[map[string]string]  `gorm:"type:json;comment:'创建时Task的调度参数快照'"`
	TaskPlanExecID          int64                               `gorm:"type:bigint;not null;comment:'对应Plan的执行计划'"`
	// 下面这些是 TaskExecution 的自身信息
	ShardingParentID int64  `gorm:"type:bigint;comment:'分片任务的父任务ID：非分片任务的ShardingParentID=NULL，分片任务的父任务的ShardingParentID=ID，分片任务的所有子任务的hardingParentID=父任务ID，使用过滤条件 ID != ShardingParentID 来选中非分片任务和分片子任务（即忽略分片父任务）'"`
	Stime            int64  `gorm:"type:bigint;comment:'开始时间'"`
	Etime            int64  `gorm:"type:bigint;comment:'结束时间'"`
	RetryCount       int64  `gorm:"type:bigint;not null;default:0;comment:'已重试次数'"`
	NextRetryTime    int64  `gorm:"type:bigint;comment:'下次重试时间'"`
	RunningProgress  int32  `gorm:"type:int;default:0;comment:'执行进度0-100，RUNNING状态下有效'"`
	Status           string `gorm:"type:ENUM('PREPARE', 'RUNNING', 'FAILED_RETRYABLE', 'FAILED_RESCHEDULED', 'FAILED', 'SUCCESS');not null;default:'PREPARE';comment:'执行状态: PREPARE-初始化(没有执行节点在执行）, RUNNING-执行中（有执行节点在执行）, FAILED_RETRYABLE-可重试失败, FAILED_RESCHEDULED-重调度失败， FAILED-失败, SUCCESS-成功'"`
	Ctime            int64  `gorm:"comment:'创建时间'"`
	Utime            int64  `gorm:"comment:'更新时间'"`
}

// TableName 指定表名
func (TaskExecution) TableName() string {
	return "task_executions"
}

type TaskExecutionDAO interface {
	// Create 创建任务执行记录
	Create(ctx context.Context, execution TaskExecution) (TaskExecution, error)
	// BatchCreate 批量创建执行记录
	BatchCreate(ctx context.Context, executions []TaskExecution) ([]TaskExecution, error)
	// GetByID 根据ID获取执行记录
	GetByID(ctx context.Context, id int64) (TaskExecution, error)
	// UpdateStatus 更新执行状态
	UpdateStatus(ctx context.Context, id int64, status string) error
	// FindRetryableExecutions 查找所有可以重试的执行记录
	// maxRetryCount: 最大重试次数限制
	// prepareTimeoutMs: PREPARE状态超时时间（毫秒），超过此时间未执行视为超时
	// limit: 查询结果数量限制
	FindRetryableExecutions(ctx context.Context, maxRetryCount, prepareTimeoutMs int64, limit int) ([]TaskExecution, error)
	// UpdateRetryResult 更新重试结果
	UpdateRetryResult(ctx context.Context, id, retryCount, nextRetryTime int64, status string, progress int32, endTime int64, scheduleParams map[string]string) error
	// SetRunningState 设置任务为运行状态并更新进度（从PREPARE状态转换）
	SetRunningState(ctx context.Context, id int64, progress int32) error
	// UpdateProgress 更新任务执行进度、开始时间（仅在RUNNING状态下有效）
	UpdateProgress(ctx context.Context, id int64, progress int32) error
	// UpdateScheduleResult 更新调度结果
	UpdateScheduleResult(ctx context.Context, id int64, status string, progress int32, endTime int64, scheduleParams map[string]string) error
	// FindReschedulableExecutions 查找所有可以重调度的执行记录
	FindReschedulableExecutions(ctx context.Context, limit int) ([]TaskExecution, error)
	// 查找对应planExecID下的所有执行计划
	FindExecutionByPlanID(ctx context.Context, planExecID int64) (map[int64]TaskExecution, error)
	FindByTaskID(ctx context.Context, taskID int64) ([]TaskExecution, error)
	FindExecutionByTaskIDAndPlanExecID(ctx context.Context, taskID int64, planExecID int64) (TaskExecution, error)
}

type GORMTaskExecutionDAO struct {
	db *egorm.Component
}

func (g *GORMTaskExecutionDAO) FindExecutionByTaskIDAndPlanExecID(ctx context.Context, taskID, planExecID int64) (TaskExecution, error) {
	var exec TaskExecution
	err := g.db.WithContext(ctx).Where("task_id = ? AND plan_exec_id = ? ", taskID, planExecID).Order("ctime DESC").First(&exec).Error
	if err != nil {
		return TaskExecution{}, fmt.Errorf("查询任务 %d 的执行记录失败: %w", taskID, err)
	}
	return exec, nil
}

func (g *GORMTaskExecutionDAO) FindByTaskID(ctx context.Context, taskID int64) ([]TaskExecution, error) {
	var executions []TaskExecution
	err := g.db.WithContext(ctx).Where("task_id = ?", taskID).Order("ctime DESC").Find(&executions).Error
	if err != nil {
		return nil, fmt.Errorf("查询任务 %d 的执行记录失败: %w", taskID, err)
	}
	return executions, nil
}

func (g *GORMTaskExecutionDAO) FindExecutionByPlanID(ctx context.Context, planExecID int64) (map[int64]TaskExecution, error) {
	var executions []TaskExecution
	err := g.db.WithContext(ctx).
		Where("plan_exec_id = ? ", planExecID).
		Order("ctime DESC").
		Find(&executions).Error
	if err != nil {
		return nil, err
	}

	result := make(map[int64]TaskExecution)
	for idx := range executions {
		execution := executions[idx]
		result[execution.TaskID] = execution
	}

	return result, nil
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
		return TaskExecution{}, fmt.Errorf("创建执行记录失败: %w", err)
	}

	// 返回包含生成ID的实体
	return execution, nil
}

func (g *GORMTaskExecutionDAO) BatchCreate(ctx context.Context, executions []TaskExecution) ([]TaskExecution, error) {
	now := time.Now().UnixMilli()
	for i := range executions {
		executions[i].Ctime, executions[i].Utime = now, now
	}
	err := g.db.WithContext(ctx).CreateInBatches(executions, len(executions)).Error
	if err != nil {
		return nil, fmt.Errorf("创建执行记录失败: %w", err)
	}
	// 返回包含生成ID的实体
	return executions, nil
}

func (g *GORMTaskExecutionDAO) GetByID(ctx context.Context, id int64) (TaskExecution, error) {
	var execution TaskExecution
	err := g.db.WithContext(ctx).Where("id = ?", id).First(&execution).Error
	if err != nil {
		return TaskExecution{}, fmt.Errorf("%w: ID=%d, %w", errs.ErrExecutionNotFound, id, err)
	}
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
		return fmt.Errorf("%w: 数据库操作失败: %w", errs.ErrUpdateExecutionStatusFailed, result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w：ID=%d", errs.ErrUpdateExecutionStatusFailed, id)
	}
	return nil
}

func (g *GORMTaskExecutionDAO) FindRetryableExecutions(ctx context.Context, maxRetryCount, prepareTimeoutMs int64, limit int) ([]TaskExecution, error) {
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

func (g *GORMTaskExecutionDAO) UpdateRetryResult(ctx context.Context, id, retryCount, nextRetryTime int64, status string, progress int32, endTime int64, scheduleParams map[string]string) error {
	result := g.db.WithContext(ctx).
		Model(&TaskExecution{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"retry_count":          retryCount,
			"next_retry_time":      nextRetryTime,
			"status":               status,
			"running_progress":     progress,
			"etime":                endTime,
			"task_schedule_params": scheduleParams,
			"utime":                time.Now().UnixMilli(),
		})

	if result.Error != nil {
		return fmt.Errorf("%w: 数据库操作失败: %w", errs.ErrUpdateExecutionRetryResultFailed, result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: ID=%d", errs.ErrUpdateExecutionRetryResultFailed, id)
	}
	return nil
}

func (g *GORMTaskExecutionDAO) SetRunningState(ctx context.Context, id int64, progress int32) error {
	now := time.Now().UnixMilli()
	result := g.db.WithContext(ctx).
		Model(&TaskExecution{}).
		Where("id = ? AND (status = ? OR status = ?) ", id, TaskExecutionStatusPrepare, TaskExecutionStatusFailedRetryable).
		Updates(map[string]any{
			"status":           TaskExecutionStatusRunning,
			"running_progress": progress,
			"stime":            now,
			"utime":            now,
		})

	if result.Error != nil {
		return fmt.Errorf("%w: 数据库操作失败: %w", errs.ErrSetExecutionStateRunningFailed, result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: 任务不在PREPARE/FAILED_RETRYABLE状态或不存在, ID=%d", errs.ErrSetExecutionStateRunningFailed, id)
	}
	return nil
}

func (g *GORMTaskExecutionDAO) UpdateProgress(ctx context.Context, id int64, progress int32) error {
	result := g.db.WithContext(ctx).
		Model(&TaskExecution{}).
		Where("id = ? AND status = ?", id, TaskExecutionStatusRunning).
		Updates(map[string]any{
			"running_progress": progress,
			"utime":            time.Now().UnixMilli(),
		})

	if result.Error != nil {
		return fmt.Errorf("%w: 数据库操作失败: %w", errs.ErrUpdateExecutionRunningProgressFailed, result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: 任务不在RUNNING状态或不存在，ID=%d", errs.ErrUpdateExecutionRunningProgressFailed, id)
	}
	return nil
}

func (g *GORMTaskExecutionDAO) UpdateScheduleResult(ctx context.Context, id int64, status string, progress int32, endTime int64, scheduleParams map[string]string) error {
	result := g.db.WithContext(ctx).
		Model(&TaskExecution{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":               status,
			"running_progress":     progress,
			"etime":                endTime,
			"task_schedule_params": sqlx.JsonColumn[map[string]string]{Val: scheduleParams, Valid: scheduleParams != nil},
			"utime":                time.Now().UnixMilli(),
		})
	if result.Error != nil {
		return fmt.Errorf("%w: 数据库操作失败: %w", errs.ErrUpdateExecutionStatusAndEndTimeFailed, result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: ID=%d", errs.ErrUpdateExecutionStatusAndEndTimeFailed, id)
	}
	return nil
}

func (g *GORMTaskExecutionDAO) FindReschedulableExecutions(ctx context.Context, limit int) ([]TaskExecution, error) {
	var executions []TaskExecution
	// 查找可重调度的执行记录
	err := g.db.WithContext(ctx).
		Where("status = ?", TaskExecutionStatusFailedRescheduled).
		Order("utime ASC").
		Limit(limit).
		Find(&executions).Error
	return executions, err
}
