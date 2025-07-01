package repository

import (
	"context"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/repository/dao"
	"github.com/ecodeclub/ekit/sqlx"
)

type TaskExecutionRepository interface {
	// Create 创建任务执行实例
	Create(ctx context.Context, execution domain.TaskExecution) (domain.TaskExecution, error)
	// UpdateStatus 更新执行状态
	UpdateStatus(ctx context.Context, id int64, status domain.TaskExecutionStatus) error
	// GetByID 根据ID获取执行实例
	GetByID(ctx context.Context, id int64) (domain.TaskExecution, error)
}

type taskExecutionRepository struct {
	dao dao.TaskExecutionDAO
}

func NewTaskExecutionRepository(executionDAO dao.TaskExecutionDAO) TaskExecutionRepository {
	return &taskExecutionRepository{dao: executionDAO}
}

func (r *taskExecutionRepository) Create(ctx context.Context, execution domain.TaskExecution) (domain.TaskExecution, error) {
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

// toEntity 将领域模型转换为DAO模型
func (r *taskExecutionRepository) toEntity(execution domain.TaskExecution) dao.TaskExecution {
	var grpcConfig sqlx.JsonColumn[domain.GrpcConfig]
	if execution.TaskGrpcConfig != nil {
		grpcConfig = sqlx.JsonColumn[domain.GrpcConfig]{Val: *execution.TaskGrpcConfig, Valid: true}
	}

	var httpConfig sqlx.JsonColumn[domain.HttpConfig]
	if execution.TaskHttpConfig != nil {
		httpConfig = sqlx.JsonColumn[domain.HttpConfig]{Val: *execution.TaskHttpConfig, Valid: true}
	}

	var retryConfig sqlx.JsonColumn[domain.RetryConfig]
	if execution.TaskRetryConfig != nil {
		retryConfig = sqlx.JsonColumn[domain.RetryConfig]{Val: *execution.TaskRetryConfig, Valid: true}
	}

	return dao.TaskExecution{
		Id:                 execution.ID,
		TaskId:             execution.TaskID,
		TaskName:           execution.TaskName,
		TaskCronExpr:       execution.TaskCronExpr,
		TaskExecutorType:   execution.TaskExecutorType.String(),
		TaskGrpcConfig:     grpcConfig,
		TaskHttpConfig:     httpConfig,
		TaskRetryConfig:    retryConfig,
		TaskVersion:        execution.TaskVersion,
		TaskScheduleNodeId: execution.TaskScheduleNodeID,
		Stime:              execution.StartTime,
		Etime:              execution.EndTime,
		RetryCnt:           int(execution.RetryCount),
		NextRetryTime:      execution.NextRetryTime,
		Status:             execution.Status.String(),
		Ctime:              execution.CTime,
		Utime:              execution.UTime,
	}
}

// toDomain 将DAO模型转换为领域模型
func (r *taskExecutionRepository) toDomain(daoExecution dao.TaskExecution) domain.TaskExecution {
	var grpcConfig *domain.GrpcConfig
	if daoExecution.TaskGrpcConfig.Valid {
		grpcConfig = &daoExecution.TaskGrpcConfig.Val
	}

	var httpConfig *domain.HttpConfig
	if daoExecution.TaskHttpConfig.Valid {
		httpConfig = &daoExecution.TaskHttpConfig.Val
	}

	var retryConfig *domain.RetryConfig
	if daoExecution.TaskRetryConfig.Valid {
		retryConfig = &daoExecution.TaskRetryConfig.Val
	}

	return domain.TaskExecution{
		ID:                 daoExecution.Id,
		TaskID:             daoExecution.TaskId,
		TaskName:           daoExecution.TaskName,
		TaskCronExpr:       daoExecution.TaskCronExpr,
		TaskExecutorType:   domain.TaskExecutorType(daoExecution.TaskExecutorType),
		TaskGrpcConfig:     grpcConfig,
		TaskHttpConfig:     httpConfig,
		TaskRetryConfig:    retryConfig,
		TaskVersion:        daoExecution.TaskVersion,
		TaskScheduleNodeID: daoExecution.TaskScheduleNodeId,
		StartTime:          daoExecution.Stime,
		EndTime:            daoExecution.Etime,
		RetryCount:         int32(daoExecution.RetryCnt),
		NextRetryTime:      daoExecution.NextRetryTime,
		Status:             domain.TaskExecutionStatus(daoExecution.Status),
		CTime:              daoExecution.Ctime,
		UTime:              daoExecution.Utime,
	}
}
