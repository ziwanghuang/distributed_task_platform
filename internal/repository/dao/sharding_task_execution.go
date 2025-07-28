package dao

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"gitee.com/flycash/distributed_task_platform/internal/errs"
	idPkg "gitee.com/flycash/distributed_task_platform/pkg/id_generator"
	"gitee.com/flycash/distributed_task_platform/pkg/sharding"
	"github.com/ecodeclub/ekit/list"
	"github.com/ecodeclub/ekit/mapx"
	"github.com/ecodeclub/ekit/sqlx"
	"github.com/ego-component/egorm"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
)

const (
	shardingNumber = 1024
)

type ShardingTaskExecutionDAO struct {
	dbs              map[string]*egorm.Component
	idGen            *idPkg.Generator
	shardingStrategy sharding.ShardingStrategy
}

func NewShardingTaskExecution(dbs map[string]*egorm.Component, idGen *idPkg.Generator, str sharding.ShardingStrategy) ShardingTaskExecutionDAO {
	return ShardingTaskExecutionDAO{
		dbs:              dbs,
		idGen:            idGen,
		shardingStrategy: str,
	}
}

func (s ShardingTaskExecutionDAO) Create(ctx context.Context, execution TaskExecution) (TaskExecution, error) {
	if execution.TaskPlanID > 0 {
		execution.ID = s.idGen.GenerateID(execution.TaskPlanID % shardingNumber)
	} else {
		execution.ID = s.idGen.GenerateID(execution.TaskID % shardingNumber)
	}

	dst := s.shardingStrategy.Shard(execution.TaskID)
	now := time.Now().UnixMilli()
	execution.Utime, execution.Ctime = now, now
	// 计算deadline
	execution.Deadline = now + execution.TaskMaxExecutionSeconds*milliseconds
	db, ok := s.dbs[dst.DB]
	if !ok {
		return TaskExecution{}, fmt.Errorf("database %s not found", dst.DB)
	}
	// GORM的Create会自动填充ID到结构体中
	err := db.WithContext(ctx).
		Table(dst.Table).
		Create(&execution).Error
	if err != nil {
		return TaskExecution{}, fmt.Errorf("创建执行记录失败: %w", err)
	}
	return execution, nil
}

func (s ShardingTaskExecutionDAO) CreateShardingParent(ctx context.Context, execution TaskExecution) (TaskExecution, error) {
	execution.ID = s.idGen.GenerateID(execution.TaskID % shardingNumber)
	dst := s.shardingStrategy.Shard(execution.TaskID)
	db, ok := s.dbs[dst.DB]
	if !ok {
		return TaskExecution{}, fmt.Errorf("database %s not found", dst.DB)
	}
	now := time.Now().UnixMilli()
	execution.Utime, execution.Ctime = now, now

	// 显式设置 ShardingParentID 为 0
	execution.ShardingParentID = sql.NullInt64{Int64: 0, Valid: true}

	err := db.WithContext(ctx).
		Table(dst.Table).
		Create(&execution).Error
	if err != nil {
		return TaskExecution{}, fmt.Errorf("创建分片父任务失败: %w", err)
	}
	return execution, nil
}

func (s ShardingTaskExecutionDAO) BatchCreate(ctx context.Context, executions []TaskExecution) ([]TaskExecution, error) {
	execMap := s.getTaskExecutionMap(executions)
	var eg errgroup.Group
	dbNames := execMap.Keys()

	res := list.NewArrayList[TaskExecution](len(executions))
	curList := list.ConcurrentList[TaskExecution]{
		List: res,
	}
	for _, dbName := range dbNames {
		// 不可能不存在
		execs, _ := execMap.Get(dbName)
		gormDB, ok := s.dbs[dbName]
		if !ok {
			return nil, fmt.Errorf("库名%s没找到", dbName)
		}
		eg.Go(func() error {
			for {
				sqls, args, execs := s.genSQLs(gormDB, execs)
				if len(sqls) > 0 {
					combinedSQL := strings.Join(sqls, "; ")
					err := gormDB.WithContext(ctx).Exec(combinedSQL, args...).Error
					if err != nil {
						if errors.Is(err, gorm.ErrDuplicatedKey) && checkTaskExecutionIds(execs, err) {
							continue
						}
						return err
					}
				}
				return curList.Append(execs...)
			}
		})
	}
	err := eg.Wait()
	return curList.AsSlice(), err
}

func checkTaskExecutionIds(executions []TaskExecution, err error) bool {
	for idx := range executions {
		execution := executions[idx]
		if !CheckErrIsIDDuplicate(execution.ID, err) {
			return true
		}
	}
	return false
}

// CheckErrIsIDDuplicate 判断是否是主键冲突
func CheckErrIsIDDuplicate(id int64, err error) bool {
	return strings.Contains(err.Error(), fmt.Sprintf("%d", id))
}

func (s ShardingTaskExecutionDAO) genSQLs(db *egorm.Component, exections []TaskExecution) (sqls []string, args []any, executions []TaskExecution) {
	now := time.Now().UnixMilli()
	sessionDB := db.Session(&gorm.Session{DryRun: true})
	const sqlRate = 2
	sqls = make([]string, 0, len(exections))
	// notification 的字段数量 + callback log 的字段数量
	const paramsRate = 25
	args = make([]any, 0, len(exections)*paramsRate)
	for idx := range exections {
		exection := exections[idx]
		var (
			id  int64
			dst sharding.Dst
		)
		if exection.TaskID > 0 {
			taskIDHash := exection.TaskID % shardingNumber
			id = s.idGen.GenerateID(taskIDHash)
			dst = s.shardingStrategy.Shard(taskIDHash)
		}
		if exection.ShardingParentID.Int64 > 0 {
			taskIDHash := exection.ShardingParentID.Int64 % shardingNumber
			id = s.idGen.GenerateID(exection.ShardingParentID.Int64 % shardingNumber)
			dst = s.shardingStrategy.Shard(taskIDHash)
		}
		exection.Ctime = now
		exection.Utime = now
		exection.ID = id
		exections[idx] = exection
		stmt := sessionDB.Table(dst.Table).Create(&exection).Statement
		sqls = append(sqls, stmt.SQL.String())
		args = append(args, stmt.Vars...)
	}
	return sqls, args, exections
}

func (s ShardingTaskExecutionDAO) getTaskExecutionMap(executions []TaskExecution) *mapx.MultiMap[string, TaskExecution] {
	// 最多就是 32 个 DB
	const maxDB = 32
	execMap := mapx.NewMultiBuiltinMap[string, TaskExecution](maxDB)
	for idx := range executions {
		execution := executions[idx]
		var dst sharding.Dst
		if execution.TaskID > 0 {
			dst = s.shardingStrategy.Shard(execution.TaskID % shardingNumber)
		}
		if execution.ShardingParentID.Valid {
			dst = s.shardingStrategy.Shard(execution.ShardingParentID.Int64 % shardingNumber)
		}
		_ = execMap.Put(dst.DB, execution)
	}
	return execMap
}

func (s ShardingTaskExecutionDAO) GetByID(ctx context.Context, id int64) (TaskExecution, error) {
	shardingID := idPkg.ExtractShardingID(id)
	dst := s.shardingStrategy.Shard(shardingID)
	db, ok := s.dbs[dst.DB]
	if !ok {
		return TaskExecution{}, fmt.Errorf("database %s not found", dst.DB)
	}
	var execution TaskExecution
	err := db.WithContext(ctx).
		Table(dst.Table).
		Where("id = ?", id).First(&execution).Error
	if err != nil {
		return TaskExecution{}, fmt.Errorf("%w: ID=%d, %w", errs.ErrExecutionNotFound, id, err)
	}
	return execution, err
}

func (s ShardingTaskExecutionDAO) UpdateStatus(ctx context.Context, id int64, status string) error {
	shardingID := idPkg.ExtractShardingID(id)
	dst := s.shardingStrategy.Shard(shardingID)
	db, ok := s.dbs[dst.DB]
	if !ok {
		return fmt.Errorf("database %s not found", dst.DB)
	}
	result := db.WithContext(ctx).
		Table(dst.Table).
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

func (s ShardingTaskExecutionDAO) FindRetryableExecutions(ctx context.Context, limit int) ([]TaskExecution, error) {
	var executions []TaskExecution
	now := time.Now().UnixMilli()
	dst, ok := sharding.DstFromCtx(ctx)
	if !ok {
		return nil, fmt.Errorf("ctx中没有包含库名表名相关信息")
	}
	db, ok := s.dbs[dst.DB]
	if !ok {
		return nil, fmt.Errorf("库名%s没找到", dst.DB)
	}
	// 复杂查询：查找可重试的执行记录
	err := db.WithContext(ctx).
		Table(dst.Table).
		// 过滤掉已达最大重试次数的记录
		// FAILED_RETRYABLE状态 - 执行失败但可重试
		Where(`status=? AND next_retry_time <= ?`, TaskExecutionStatusFailedRetryable, now).
		// 确保到了可以执行的时间
		Where(" next_retry_time <= ?", now).
		Limit(limit).
		Find(&executions).Error
	return executions, err
}

func (s ShardingTaskExecutionDAO) FindShardingParents(ctx context.Context, offset, batchSize int) ([]TaskExecution, error) {
	var executions []TaskExecution
	dst, ok := sharding.DstFromCtx(ctx)
	if !ok {
		return nil, fmt.Errorf("ctx中没有包含库名表名相关信息")
	}
	gormDB, ok := s.dbs[dst.DB]
	if !ok {
		return nil, fmt.Errorf("库名%s没找到", dst.DB)
	}
	err := gormDB.WithContext(ctx).
		Table(dst.Table).
		Where("sharding_parent_id = 0").
		// 状态为RUNNING，表示任务还在进行中，需要检查
		Where("status = ?", TaskExecutionStatusRunning).
		// 按更新时间排序，优先处理最久没有变化的，更公平
		Order("utime ASC").
		Offset(offset).
		Limit(batchSize).
		Find(&executions).Error
	if err != nil {
		return nil, fmt.Errorf("查询分片父任务失败: %w", err)
	}
	return executions, nil
}

func (s ShardingTaskExecutionDAO) FindShardingChildren(ctx context.Context, parentID int64) ([]TaskExecution, error) {
	shardingID := idPkg.ExtractShardingID(parentID)
	dst := s.shardingStrategy.Shard(shardingID)
	db, ok := s.dbs[dst.DB]
	if !ok {
		return nil, fmt.Errorf("database %s not found", dst.DB)
	}
	var executions []TaskExecution
	err := db.WithContext(ctx).
		Table(dst.Table).
		Where("sharding_parent_id = ?", parentID).
		Find(&executions).Error
	return executions, err
}

func (s ShardingTaskExecutionDAO) UpdateRetryResult(ctx context.Context, id, retryCount, nextRetryTime int64, status string, progress int32, endTime int64, scheduleParams map[string]string, executorNodeID string) error {
	shardingID := idPkg.ExtractShardingID(id)
	dst := s.shardingStrategy.Shard(shardingID)
	db, ok := s.dbs[dst.DB]
	if !ok {
		return fmt.Errorf("database %s not found", dst.DB)
	}
	result := db.WithContext(ctx).
		Table(dst.Table).
		Model(&TaskExecution{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"retry_count":      retryCount,
			"next_retry_time":  nextRetryTime,
			"status":           status,
			"running_progress": progress,
			"etime":            endTime,
			"task_schedule_params": sqlx.JsonColumn[map[string]string]{
				Val:   scheduleParams,
				Valid: scheduleParams != nil,
			},
			"executor_node_id": sql.NullString{String: executorNodeID, Valid: executorNodeID != ""},
			"utime":            time.Now().UnixMilli(),
		})

	if result.Error != nil {
		return fmt.Errorf("%w: 数据库操作失败: %w", errs.ErrUpdateExecutionRetryResultFailed, result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: ID=%d", errs.ErrUpdateExecutionRetryResultFailed, id)
	}
	return nil
}

func (s ShardingTaskExecutionDAO) SetRunningState(ctx context.Context, id int64, progress int32, executorNodeID string) error {
	shardingID := idPkg.ExtractShardingID(id)
	dst := s.shardingStrategy.Shard(shardingID)
	db, ok := s.dbs[dst.DB]
	if !ok {
		return fmt.Errorf("database %s not found", dst.DB)
	}
	now := time.Now().UnixMilli()

	// 首先查询任务执行记录
	var execution TaskExecution
	err := db.WithContext(ctx).
		Table(dst.Table).
		Model(&TaskExecution{}).
		Where("id = ?", id).
		First(&execution).Error
	if err != nil {
		return fmt.Errorf("%w: 查询执行记录失败: %w", errs.ErrSetExecutionStateRunningFailed, err)
	}

	// 重新计算deadline
	newDeadline := now + execution.TaskMaxExecutionSeconds*milliseconds

	result := db.WithContext(ctx).
		Table(dst.Table).
		Model(&TaskExecution{}).
		Where("id = ? AND (status = ? OR status = ? OR status = ?) ",
			id, TaskExecutionStatusPrepare, TaskExecutionStatusFailedRetryable, TaskExecutionStatusFailedRescheduled).
		Updates(map[string]any{
			"status":           TaskExecutionStatusRunning,
			"running_progress": progress,
			"stime":            now,
			"deadline":         newDeadline,
			"utime":            now,
			"executor_node_id": sql.NullString{String: executorNodeID, Valid: executorNodeID != ""},
		})

	if result.Error != nil {
		return fmt.Errorf("%w: 数据库操作失败: %w", errs.ErrSetExecutionStateRunningFailed, result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: 任务不在PREPARE/FAILED_RETRYABLE状态或不存在, ID=%d", errs.ErrSetExecutionStateRunningFailed, id)
	}
	return nil
}

func (s ShardingTaskExecutionDAO) UpdateProgress(ctx context.Context, id int64, progress int32) error {
	shardingID := idPkg.ExtractShardingID(id)
	dst := s.shardingStrategy.Shard(shardingID)
	db, ok := s.dbs[dst.DB]
	if !ok {
		return fmt.Errorf("database %s not found", dst.DB)
	}
	result := db.WithContext(ctx).
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

func (s ShardingTaskExecutionDAO) UpdateScheduleResult(ctx context.Context, id int64, status string, progress int32, endTime int64, scheduleParams map[string]string, executorNodeID string) error {
	shardingID := idPkg.ExtractShardingID(id)
	dst := s.shardingStrategy.Shard(shardingID)
	db, ok := s.dbs[dst.DB]
	if !ok {
		return fmt.Errorf("database %s not found", dst.DB)
	}
	result := db.WithContext(ctx).
		Table(dst.Table).
		Model(&TaskExecution{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":               status,
			"running_progress":     progress,
			"etime":                endTime,
			"task_schedule_params": sqlx.JsonColumn[map[string]string]{Val: scheduleParams, Valid: scheduleParams != nil},
			"executor_node_id":     sql.NullString{String: executorNodeID, Valid: executorNodeID != ""},
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

func (s ShardingTaskExecutionDAO) FindReschedulableExecutions(ctx context.Context, limit int) ([]TaskExecution, error) {
	var executions []TaskExecution
	dst, ok := sharding.DstFromCtx(ctx)
	if !ok {
		return nil, fmt.Errorf("ctx中没有包含库名表名相关信息")
	}
	db, ok := s.dbs[dst.DB]
	if !ok {
		return nil, fmt.Errorf("库名%s没找到", dst.DB)
	}
	// 查找可重调度的执行记录
	err := db.WithContext(ctx).
		Table(dst.Table).
		Where("status = ? AND (sharding_parent_id IS NULL OR sharding_parent_id > 0) ", TaskExecutionStatusFailedRescheduled).
		Order("utime ASC").
		Limit(limit).
		Find(&executions).Error
	return executions, err
}

func (s ShardingTaskExecutionDAO) FindExecutionByPlanID(ctx context.Context, planExecID int64) (map[int64]TaskExecution, error) {
	// plan和plan对应的任务的执行计划的id都是将planid%1024取余后的值编码进exection的id所以plan所有任务的执行计划都在同一张表里
	shardingID := idPkg.ExtractShardingID(planExecID)
	dst := s.shardingStrategy.Shard(shardingID)
	db, ok := s.dbs[dst.DB]
	if !ok {
		return nil, fmt.Errorf("database %s not found", dst.DB)
	}
	var executions []TaskExecution
	err := db.WithContext(ctx).
		Table(dst.Table).
		Where("task_plan_exec_id = ? ", planExecID).
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

func (s ShardingTaskExecutionDAO) FindByTaskID(ctx context.Context, taskID int64) ([]TaskExecution, error) {
	dst := s.shardingStrategy.Shard(taskID % shardingNumber)
	db, ok := s.dbs[dst.DB]
	if !ok {
		return nil, fmt.Errorf("database %s not found", dst.DB)
	}
	var executions []TaskExecution
	err := db.WithContext(ctx).
		Table(dst.Table).
		Where("task_id = ?", taskID).
		Order("ctime DESC").Find(&executions).
		Error
	if err != nil {
		return nil, fmt.Errorf("查询任务 %d 的执行记录失败: %w", taskID, err)
	}
	return executions, nil
}

func (s ShardingTaskExecutionDAO) FindExecutionByTaskIDAndPlanExecID(ctx context.Context, taskID int64, planExecID int64) (TaskExecution, error) {
	//TODO implement me
	panic("implement me")
}

func (s ShardingTaskExecutionDAO) FindTimeoutExecutions(ctx context.Context, limit int) ([]TaskExecution, error) {
	dst, ok := sharding.DstFromCtx(ctx)
	if !ok {
		return nil, fmt.Errorf("ctx中没有包含库名表名相关信息")
	}
	db, ok := s.dbs[dst.DB]
	if !ok {
		return nil, fmt.Errorf("database %s not found", dst.DB)
	}
	var executions []TaskExecution
	now := time.Now().UnixMilli()
	err := db.WithContext(ctx).
		Table(dst.Table).
		Where("deadline <= ? AND status = ?", now, TaskExecutionStatusRunning).
		Order("deadline ASC").
		Limit(limit).
		Find(&executions).Error

	return executions, err
}

// GetShardingStrategy returns the sharding strategy used by this DAO
func (s ShardingTaskExecutionDAO) GetShardingStrategy() sharding.ShardingStrategy {
	return s.shardingStrategy
}
