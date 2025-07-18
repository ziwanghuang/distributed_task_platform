package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/errs"
	"gitee.com/flycash/distributed_task_platform/internal/repository/dao"
	"github.com/ecodeclub/ekit/slice"
	"github.com/ecodeclub/ekit/sqlx"
)

type TaskExecutionRepository interface {
	// Create 创建任务执行实例
	Create(ctx context.Context, execution domain.TaskExecution) (domain.TaskExecution, error)
	// CreateShardingChildren 创建分片子任务执行实例
	CreateShardingChildren(ctx context.Context, parent domain.TaskExecution) ([]domain.TaskExecution, error)
	// UpdateStatus 更新执行状态
	UpdateStatus(ctx context.Context, id int64, status domain.TaskExecutionStatus) error
	// GetByID 根据ID获取执行实例
	GetByID(ctx context.Context, id int64) (domain.TaskExecution, error)
	// FindRetryableExecutions 查找所有可以重试的执行记录
	// maxRetryCount: 最大重试次数限制
	// prepareTimeoutMs: PREPARE状态超时时间（毫秒），超过此时间未执行视为超时
	// limit: 查询结果数量限制
	FindRetryableExecutions(ctx context.Context, maxRetryCount, prepareTimeoutMs int64, limit int) ([]domain.TaskExecution, error)
	// FindShardingParents 查找分片父任务
	FindShardingParents(ctx context.Context, batchSize int) ([]domain.TaskExecution, error)
	// FindShardingChildren 查找分片子任务
	FindShardingChildren(ctx context.Context, parentID int64) ([]domain.TaskExecution, error)
	// UpdateRetryResult 更新重试结果
	UpdateRetryResult(ctx context.Context, id, retryCount, nextRetryTime int64, status domain.TaskExecutionStatus, progress int32, endTime int64, scheduleParams map[string]string, executorNodeID string) error
	// SetRunningState 设置任务为运行状态并更新进度
	SetRunningState(ctx context.Context, id int64, progress int32, executorNodeID string) error
	// UpdateRunningProgress 更新任务执行进度（仅在RUNNING状态下有效）
	UpdateRunningProgress(ctx context.Context, id int64, progress int32) error
	// UpdateScheduleResult 更新调度结果
	UpdateScheduleResult(ctx context.Context, id int64, status domain.TaskExecutionStatus, progress int32, endTime int64, scheduleParams map[string]string, executorNodeID string) error
	// FindReschedulableExecutions 查找所有可以重调度的执行记录
	FindReschedulableExecutions(ctx context.Context, limit int) ([]domain.TaskExecution, error)

	FindExecutionsByPlanExecID(ctx context.Context, planExecID int64) (map[int64]domain.TaskExecution, error)
	FindByTaskID(ctx context.Context, taskID int64) ([]domain.TaskExecution, error)
	FindExecutionByTaskIDAndPlanExecID(ctx context.Context, taskID int64, planExecID int64) (domain.TaskExecution, error)
	// FindTimeoutExecutions 查找超时的执行记录
	FindTimeoutExecutions(ctx context.Context, limit int) ([]domain.TaskExecution, error)
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

func (r *taskExecutionRepository) FindExecutionByTaskIDAndPlanExecID(ctx context.Context, taskID, planExecID int64) (domain.TaskExecution, error) {
	daoExec, err := r.dao.FindExecutionByTaskIDAndPlanExecID(ctx, taskID, planExecID)
	if err != nil {
		return domain.TaskExecution{}, err
	}
	return r.toDomain(daoExec), nil
}

func (r *taskExecutionRepository) FindByTaskID(ctx context.Context, taskID int64) ([]domain.TaskExecution, error) {
	daoExecutions, err := r.dao.FindByTaskID(ctx, taskID)
	if err != nil {
		return nil, err
	}
	return slice.Map(daoExecutions, func(_ int, src dao.TaskExecution) domain.TaskExecution {
		return r.toDomain(src)
	}), nil
}

func (r *taskExecutionRepository) FindExecutionsByPlanExecID(ctx context.Context, planExecID int64) (map[int64]domain.TaskExecution, error) {
	daoExecutions, err := r.dao.FindExecutionByPlanID(ctx, planExecID)
	if err != nil {
		return nil, err
	}

	// 将DAO模型转换为领域模型
	result := make(map[int64]domain.TaskExecution)
	for taskID := range daoExecutions {
		daoExecution := daoExecutions[taskID]
		result[taskID] = r.toDomain(daoExecution)
	}

	return result, nil
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
	completeTask.PlanExecID = execution.Task.PlanExecID
	execution.Task = completeTask
	created, err := r.dao.Create(ctx, r.toEntity(execution))
	if err != nil {
		return domain.TaskExecution{}, err
	}
	return r.toDomain(created), nil
}

func (r *taskExecutionRepository) CreateShardingChildren(ctx context.Context, parent domain.TaskExecution) ([]domain.TaskExecution, error) {
	if parent.Task.ID == 0 {
		return nil, errors.New("Task.ID不能为空")
	}

	if parent.Task.ShardingRule == nil {
		return nil, errs.ErrTaskShardingRuleNotFound
	}
	// 计算分片任务需要的分片调度参数
	scheduleParams := parent.Task.ShardingRule.ToScheduleParams()
	if len(scheduleParams) == 0 {
		return nil, errs.ErrInvalidTaskShardingRule
	}
	parent.Status = domain.TaskExecutionStatusRunning
	// 创建父任务执行记录
	p, err := r.dao.CreateShardingParent(ctx, r.toEntity(parent))
	if err != nil {
		return nil, err
	}
	createdParent := r.toDomain(p)
	// 填充子任务执行记录信息
	children := make([]domain.TaskExecution, 0, len(scheduleParams))
	for i := range scheduleParams {
		// 值拷贝，复用父任务执行中的信息来创建子任务
		child := createdParent
		child.ID = 0
		// 子任务要保持预备状态
		child.Status = domain.TaskExecutionStatusPrepare
		// 必须为每个子任务设置其父任务的ID
		parentID := createdParent.ID
		child.ShardingParentID = &parentID
		// 覆盖或添加父任务中的基础调度信息
		child.MergeTaskScheduleParams(scheduleParams[i])
		children = append(children, child)
	}
	// 创建子任务
	createdExecutions, err := r.dao.BatchCreate(ctx, slice.Map(children, func(_ int, src domain.TaskExecution) dao.TaskExecution {
		return r.toEntity(src)
	}))
	if err != nil {
		return nil, err
	}
	return slice.Map(createdExecutions, func(_ int, src dao.TaskExecution) domain.TaskExecution {
		return r.toDomain(src)
	}), nil
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

func (r *taskExecutionRepository) FindRetryableExecutions(ctx context.Context, maxRetryCount, prepareTimeoutMs int64, limit int) ([]domain.TaskExecution, error) {
	daoExecutions, err := r.dao.FindRetryableExecutions(ctx, maxRetryCount, prepareTimeoutMs, limit)
	if err != nil {
		return nil, err
	}
	return slice.Map(daoExecutions, func(_ int, src dao.TaskExecution) domain.TaskExecution {
		return r.toDomain(src)
	}), nil
}

func (r *taskExecutionRepository) UpdateRetryResult(ctx context.Context, id, retryCount, nextRetryTime int64, status domain.TaskExecutionStatus, progress int32, endTime int64, scheduleParams map[string]string, executorNodeID string) error {
	return r.dao.UpdateRetryResult(ctx, id, retryCount, nextRetryTime, status.String(), progress, endTime, scheduleParams, executorNodeID)
}

func (r *taskExecutionRepository) SetRunningState(ctx context.Context, id int64, progress int32, executorNodeID string) error {
	return r.dao.SetRunningState(ctx, id, progress, executorNodeID)
}

func (r *taskExecutionRepository) UpdateRunningProgress(ctx context.Context, id int64, progress int32) error {
	return r.dao.UpdateProgress(ctx, id, progress)
}

func (r *taskExecutionRepository) UpdateScheduleResult(ctx context.Context, id int64, status domain.TaskExecutionStatus, progress int32, endTime int64, scheduleParams map[string]string, executorNodeID string) error {
	return r.dao.UpdateScheduleResult(ctx, id, status.String(), progress, endTime, scheduleParams, executorNodeID)
}

func (r *taskExecutionRepository) FindReschedulableExecutions(ctx context.Context, limit int) ([]domain.TaskExecution, error) {
	daoExecutions, err := r.dao.FindReschedulableExecutions(ctx, limit)
	if err != nil {
		return nil, err
	}
	return slice.Map(daoExecutions, func(_ int, src dao.TaskExecution) domain.TaskExecution {
		return r.toDomain(src)
	}), nil
}

func (r *taskExecutionRepository) FindShardingParents(ctx context.Context, batchSize int) ([]domain.TaskExecution, error) {
	daoExecutions, err := r.dao.FindShardingParents(ctx, batchSize)
	if err != nil {
		return nil, err
	}
	return slice.Map(daoExecutions, func(_ int, src dao.TaskExecution) domain.TaskExecution {
		return r.toDomain(src)
	}), nil
}

func (r *taskExecutionRepository) FindShardingChildren(ctx context.Context, parentID int64) ([]domain.TaskExecution, error) {
	daoExecutions, err := r.dao.FindShardingChildren(ctx, parentID)
	if err != nil {
		return nil, err
	}
	return slice.Map(daoExecutions, func(_ int, src dao.TaskExecution) domain.TaskExecution {
		return r.toDomain(src)
	}), nil
}

func (r *taskExecutionRepository) FindTimeoutExecutions(ctx context.Context, limit int) ([]domain.TaskExecution, error) {
	daoExecutions, err := r.dao.FindTimeoutExecutions(ctx, limit)
	if err != nil {
		return nil, err
	}
	return slice.Map(daoExecutions, func(_ int, src dao.TaskExecution) domain.TaskExecution {
		return r.toDomain(src)
	}), nil
}

// toEntity 将领域模型转换为DAO模型
func (r *taskExecutionRepository) toEntity(execution domain.TaskExecution) dao.TaskExecution {
	var grpcConfig sqlx.JsonColumn[domain.GrpcConfig]
	if execution.Task.GrpcConfig != nil {
		grpcConfig = sqlx.JsonColumn[domain.GrpcConfig]{Val: *execution.Task.GrpcConfig, Valid: true}
	}

	var httpConfig sqlx.JsonColumn[domain.HTTPConfig]
	if execution.Task.HTTPConfig != nil {
		httpConfig = sqlx.JsonColumn[domain.HTTPConfig]{Val: *execution.Task.HTTPConfig, Valid: true}
	}

	var retryConfig sqlx.JsonColumn[domain.RetryConfig]
	if execution.Task.RetryConfig != nil {
		retryConfig = sqlx.JsonColumn[domain.RetryConfig]{Val: *execution.Task.RetryConfig, Valid: true}
	}

	var taskScheduleParams sqlx.JsonColumn[map[string]string]
	if execution.Task.ScheduleParams != nil {
		taskScheduleParams = sqlx.JsonColumn[map[string]string]{Val: execution.Task.ScheduleParams, Valid: true}
	}

	var shardingParentID sql.NullInt64
	if execution.ShardingParentID != nil {
		// 指针非nil，说明DB中应有值
		shardingParentID = sql.NullInt64{Int64: *execution.ShardingParentID, Valid: true}
	}

	var executorNodeID sql.NullString
	if execution.ExecutorNodeID != "" {
		executorNodeID = sql.NullString{String: execution.ExecutorNodeID, Valid: true}
	}

	return dao.TaskExecution{
		ID: execution.ID,
		// 从Task展开的冗余字段
		TaskID:                  execution.Task.ID,
		TaskName:                execution.Task.Name,
		TaskCronExpr:            execution.Task.CronExpr,
		TaskExecutionMethod:     execution.Task.ExecutionMethod.String(),
		TaskGrpcConfig:          grpcConfig,
		TaskHTTPConfig:          httpConfig,
		TaskRetryConfig:         retryConfig,
		TaskMaxExecutionSeconds: execution.Task.MaxExecutionSeconds,
		TaskVersion:             execution.Task.Version,
		TaskScheduleNodeID:      execution.Task.ScheduleNodeID,
		TaskScheduleParams:      taskScheduleParams,
		TaskPlanExecID:          execution.Task.PlanExecID,
		TaskPlanID:              execution.Task.PlanID,
		// TaskExecution自身字段
		ShardingParentID: shardingParentID,
		ExecutorNodeID:   executorNodeID,
		Deadline:         execution.Deadline,
		Stime:            execution.StartTime,
		Etime:            execution.EndTime,
		RetryCount:       execution.RetryCount,
		NextRetryTime:    execution.NextRetryTime,
		RunningProgress:  execution.RunningProgress,
		Status:           execution.Status.String(),
		Ctime:            execution.CTime,
		Utime:            execution.UTime,
	}
}

// toDomain 将DAO模型转换为领域模型
func (r *taskExecutionRepository) toDomain(daoExecution dao.TaskExecution) domain.TaskExecution {
	var taskGrpcConfig *domain.GrpcConfig
	if daoExecution.TaskGrpcConfig.Valid {
		taskGrpcConfig = &daoExecution.TaskGrpcConfig.Val
	}

	var taskHTTPConfig *domain.HTTPConfig
	if daoExecution.TaskHTTPConfig.Valid {
		taskHTTPConfig = &daoExecution.TaskHTTPConfig.Val
	}

	var taskRetryConfig *domain.RetryConfig
	if daoExecution.TaskRetryConfig.Valid {
		taskRetryConfig = &daoExecution.TaskRetryConfig.Val
	}

	var taskScheduleParams map[string]string
	if daoExecution.TaskScheduleParams.Valid {
		taskScheduleParams = daoExecution.TaskScheduleParams.Val
	}

	var shardingParentID *int64
	if daoExecution.ShardingParentID.Valid {
		val := daoExecution.ShardingParentID.Int64
		shardingParentID = &val
	}

	var executorNodeID string
	if daoExecution.ExecutorNodeID.Valid {
		executorNodeID = daoExecution.ExecutorNodeID.String
	}

	return domain.TaskExecution{
		ID: daoExecution.ID,
		Task: domain.Task{
			ID:                  daoExecution.TaskID,
			Name:                daoExecution.TaskName,
			CronExpr:            daoExecution.TaskCronExpr,
			ExecutionMethod:     domain.TaskExecutionMethod(daoExecution.TaskExecutionMethod),
			GrpcConfig:          taskGrpcConfig,
			HTTPConfig:          taskHTTPConfig,
			RetryConfig:         taskRetryConfig,
			MaxExecutionSeconds: daoExecution.TaskMaxExecutionSeconds,
			ScheduleParams:      taskScheduleParams,
			ScheduleNodeID:      daoExecution.TaskScheduleNodeID,
			Version:             daoExecution.TaskVersion,
			PlanID:              daoExecution.TaskPlanID,
		},
		ShardingParentID: shardingParentID,
		Deadline:         daoExecution.Deadline,
		ExecutorNodeID:   executorNodeID,
		StartTime:        daoExecution.Stime,
		EndTime:          daoExecution.Etime,
		RetryCount:       daoExecution.RetryCount,
		NextRetryTime:    daoExecution.NextRetryTime,
		RunningProgress:  daoExecution.RunningProgress,
		Status:           domain.TaskExecutionStatus(daoExecution.Status),
		CTime:            daoExecution.Ctime,
		UTime:            daoExecution.Utime,
	}
}
