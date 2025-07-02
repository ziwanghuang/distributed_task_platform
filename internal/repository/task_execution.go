package repository

import (
	"context"
	"errors"
	"fmt"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/repository/dao"
	"github.com/ecodeclub/ekit/slice"
	"github.com/ecodeclub/ekit/sqlx"
)

type TaskExecutionRepository interface {
	// Create 创建任务执行实例
	Create(ctx context.Context, execution domain.TaskExecution) (domain.TaskExecution, error)
	// UpdateStatus 更新执行状态
	UpdateStatus(ctx context.Context, id int64, status domain.TaskExecutionStatus) error
	// GetByID 根据ID获取执行实例
	GetByID(ctx context.Context, id int64) (domain.TaskExecution, error)
	// FindRetryableExecutions 查找所有可以重试的执行记录
	// maxRetryCount: 最大重试次数限制
	// prepareTimeoutMs: PREPARE状态超时时间（毫秒），超过此时间未执行视为超时
	// limit: 查询结果数量限制
	FindRetryableExecutions(ctx context.Context, maxRetryCount int64, prepareTimeoutMs int64, limit int) ([]domain.TaskExecution, error)
	// UpdateRetryResult 更新重试结果
	UpdateRetryResult(ctx context.Context, id, retryCount, nextRetryTime, endTime int64, status domain.TaskExecutionStatus) error
}

type taskExecutionRepository struct {
	dao      dao.TaskExecutionDAO
	taskRepo TaskRepository
}

func NewTaskExecutionRepository(executionDAO dao.TaskExecutionDAO, taskRepo TaskRepository) TaskExecutionRepository {
	return &taskExecutionRepository{
		dao:      executionDAO,
		taskRepo: taskRepo,
	}
}

func (r *taskExecutionRepository) Create(ctx context.Context, execution domain.TaskExecution) (domain.TaskExecution, error) {
	// 验证必填字段
	if execution.Task.ID == 0 {
		return domain.TaskExecution{}, errors.New("Task.ID不能为空")
	}
	// 获取完整的Task信息
	completeTask, err := r.taskRepo.GetByID(ctx, execution.Task.ID)
	if err != nil {
		return domain.TaskExecution{}, fmt.Errorf("获取Task信息失败: %w", err)
	}
	// 创建TaskExecution
	execution.Task = completeTask
	created, err := r.dao.Create(ctx, r.toEntity(execution))
	if err != nil {
		return domain.TaskExecution{}, err
	}
	return r.toDomain(created), nil
}

func (r *taskExecutionRepository) UpdateStatus(ctx context.Context, id int64, status domain.TaskExecutionStatus) error {
	return r.dao.UpdateStatus(ctx, id, status.String())
}

func (r *taskExecutionRepository) GetByID(ctx context.Context, id int64) (domain.TaskExecution, error) {
	daoExecution, err := r.dao.GetByID(ctx, id)
	if err != nil {
		return domain.TaskExecution{}, err
	}
	return r.toDomain(daoExecution), nil
}

func (r *taskExecutionRepository) FindRetryableExecutions(ctx context.Context, maxRetryCount int64, prepareTimeoutMs int64, limit int) ([]domain.TaskExecution, error) {
	daoExecutions, err := r.dao.FindRetryableExecutions(ctx, maxRetryCount, prepareTimeoutMs, limit)
	if err != nil {
		return nil, err
	}
	return slice.Map(daoExecutions, func(idx int, src dao.TaskExecution) domain.TaskExecution {
		return r.toDomain(src)
	}), nil
}

func (r *taskExecutionRepository) UpdateRetryResult(ctx context.Context, id, retryCount, nextRetryTime, endTime int64, status domain.TaskExecutionStatus) error {
	return r.dao.UpdateRetryResult(ctx, id, retryCount, nextRetryTime, endTime, status.String())
}

// toEntity 将领域模型转换为DAO模型
func (r *taskExecutionRepository) toEntity(execution domain.TaskExecution) dao.TaskExecution {
	var grpcConfig sqlx.JsonColumn[domain.GrpcConfig]
	if execution.Task.GrpcConfig != nil {
		grpcConfig = sqlx.JsonColumn[domain.GrpcConfig]{Val: *execution.Task.GrpcConfig, Valid: true}
	}

	var httpConfig sqlx.JsonColumn[domain.HttpConfig]
	if execution.Task.HttpConfig != nil {
		httpConfig = sqlx.JsonColumn[domain.HttpConfig]{Val: *execution.Task.HttpConfig, Valid: true}
	}

	var retryConfig sqlx.JsonColumn[domain.RetryConfig]
	if execution.Task.RetryConfig != nil {
		retryConfig = sqlx.JsonColumn[domain.RetryConfig]{Val: *execution.Task.RetryConfig, Valid: true}
	}

	var taskScheduleParams sqlx.JsonColumn[map[string]string]
	if execution.Task.ScheduleParams != nil {
		taskScheduleParams = sqlx.JsonColumn[map[string]string]{Val: execution.Task.ScheduleParams, Valid: true}
	}

	return dao.TaskExecution{
		Id: execution.ID,
		// 从Task展开的冗余字段
		TaskId:             execution.Task.ID,
		TaskName:           execution.Task.Name,
		TaskCronExpr:       execution.Task.CronExpr,
		TaskExecutorType:   execution.Task.ExecutorType.String(),
		TaskGrpcConfig:     grpcConfig,
		TaskHttpConfig:     httpConfig,
		TaskRetryConfig:    retryConfig,
		TaskVersion:        execution.Task.Version,
		TaskScheduleNodeId: execution.Task.ScheduleNodeID,
		TaskScheduleParams: taskScheduleParams,
		// TaskExecution自身字段
		Stime:         execution.StartTime,
		Etime:         execution.EndTime,
		RetryCount:    execution.RetryCount,
		NextRetryTime: execution.NextRetryTime,
		Status:        execution.Status.String(),
		Ctime:         execution.CTime,
		Utime:         execution.UTime,
	}
}

// toDomain 将DAO模型转换为领域模型
func (r *taskExecutionRepository) toDomain(daoExecution dao.TaskExecution) domain.TaskExecution {
	var taskGrpcConfig *domain.GrpcConfig
	if daoExecution.TaskGrpcConfig.Valid {
		taskGrpcConfig = &daoExecution.TaskGrpcConfig.Val
	}

	var taskHttpConfig *domain.HttpConfig
	if daoExecution.TaskHttpConfig.Valid {
		taskHttpConfig = &daoExecution.TaskHttpConfig.Val
	}

	var taskRetryConfig *domain.RetryConfig
	if daoExecution.TaskRetryConfig.Valid {
		taskRetryConfig = &daoExecution.TaskRetryConfig.Val
	}

	var taskScheduleParams map[string]string
	if daoExecution.TaskScheduleParams.Valid {
		taskScheduleParams = daoExecution.TaskScheduleParams.Val
	}

	return domain.TaskExecution{
		ID: daoExecution.Id,
		Task: domain.Task{
			ID:             daoExecution.TaskId,
			Name:           daoExecution.TaskName,
			CronExpr:       daoExecution.TaskCronExpr,
			ExecutorType:   domain.TaskExecutorType(daoExecution.TaskExecutorType),
			GrpcConfig:     taskGrpcConfig,
			HttpConfig:     taskHttpConfig,
			RetryConfig:    taskRetryConfig,
			ScheduleParams: taskScheduleParams,
			ScheduleNodeID: daoExecution.TaskScheduleNodeId,
			Version:        daoExecution.TaskVersion,
		},
		StartTime:     daoExecution.Stime,
		EndTime:       daoExecution.Etime,
		RetryCount:    daoExecution.RetryCount,
		NextRetryTime: daoExecution.NextRetryTime,
		Status:        domain.TaskExecutionStatus(daoExecution.Status),
		CTime:         daoExecution.Ctime,
		UTime:         daoExecution.Utime,
	}
}
