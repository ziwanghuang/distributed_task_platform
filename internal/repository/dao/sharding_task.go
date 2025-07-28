package dao

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ecodeclub/ekit/sqlx"

	"gitee.com/flycash/distributed_task_platform/internal/errs"
	"gitee.com/flycash/distributed_task_platform/pkg/sharding"

	idPkg "gitee.com/flycash/distributed_task_platform/pkg/id_generator"
	"github.com/ego-component/egorm"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
)

const (
	dbNumber   = 4
	tabNumber  = 4
	dbNamePre  = "task_db"
	tabNamePre = "task"
)

type ShardingTaskDAO struct {
	dbs              map[string]*egorm.Component
	idGen            *idPkg.Generator
	shardingStrategy sharding.ShardingStrategy
}

func NewShardingTaskDAO(dbs map[string]*egorm.Component, idGen *idPkg.Generator, str sharding.ShardingStrategy) ShardingTaskDAO {
	return ShardingTaskDAO{
		dbs:              dbs,
		idGen:            idGen,
		shardingStrategy: str,
	}
}

func (s ShardingTaskDAO) FindSchedulableTasks(ctx context.Context, preemptedTimeoutMs int64, limit int) ([]*Task, error) {
	// 每个库独立的从ctx里读取库，表
	var tasks []*Task
	dst, ok := sharding.DstFromCtx(ctx)
	if !ok {
		return nil, fmt.Errorf("ctx中没有包含库名表名相关信息")
	}
	db := s.dbs[dst.DB]
	now := time.Now().UnixMilli()
	err := db.WithContext(ctx).
		Where("next_time <= ? AND (status = ? OR (status = ? AND utime <= ?))",
			now, StatusActive, StatusPreempted, now-preemptedTimeoutMs).
		Order("next_time ASC").
		Limit(limit).
		Find(&tasks).Error
	if err != nil {
		return nil, err
	}
	return tasks, nil
}

func (s ShardingTaskDAO) Create(ctx context.Context, task Task) (*Task, error) {
	newID := s.idGen.GenerateID(task.BizID)
	task.ID = newID

	dst := s.shardingStrategy.Shard(task.BizID)
	db, ok := s.dbs[dst.DB]
	if !ok {
		return nil, fmt.Errorf("database %s not found", dst.DB)
	}

	if err := db.Table(dst.Table).Create(&task).Error; err != nil {
		return nil, err
	}

	return &task, nil
}

func (s ShardingTaskDAO) GetByID(ctx context.Context, id int64) (*Task, error) {
	bizID := idPkg.ExtractShardingID(id)
	dst := s.shardingStrategy.Shard(bizID)
	db, ok := s.dbs[dst.DB]
	if !ok {
		return nil, fmt.Errorf("database %s not found", dst.DB)
	}

	var task Task
	if err := db.Table(dst.Table).Where("id = ?", id).First(&task).Error; err != nil {
		return nil, err
	}

	return &task, nil
}

func (s ShardingTaskDAO) FindByPlanID(ctx context.Context, planID int64) ([]*Task, error) {
	bizID := idPkg.ExtractShardingID(planID)
	dst := s.shardingStrategy.Shard(bizID)
	db, ok := s.dbs[dst.DB]
	if !ok {
		return nil, fmt.Errorf("database %s not found", dst.DB)
	}
	var tasks []*Task
	err := db.WithContext(ctx).
		Table(dst.Table).
		Where("plan_id = ?", planID).Find(&tasks).Error
	if err != nil {
		return nil, err
	}
	return tasks, nil

}

func (s ShardingTaskDAO) Acquire(ctx context.Context, id int64, scheduleNodeID string) (*Task, error) {
	bizID := idPkg.ExtractShardingID(id)
	dst := s.shardingStrategy.Shard(bizID)
	db, ok := s.dbs[dst.DB]
	if !ok {
		return nil, fmt.Errorf("database %s not found", dst.DB)
	}
	var acquiredTask *Task
	err := db.WithContext(ctx).
		Transaction(func(tx *gorm.DB) error {
			// 1. 在事务中执行更新
			result := tx.Model(&Task{}).
				Table(dst.Table).
				Where("id = ? AND status = ?", id, StatusActive).
				Updates(map[string]any{
					"status":           StatusPreempted,
					"schedule_node_id": scheduleNodeID,
					"version":          gorm.Expr("version + 1"),
					"utime":            time.Now().UnixMilli(),
				})
			if result.Error != nil {
				return result.Error // 事务将自动回滚
			}
			if result.RowsAffected == 0 {
				// 可能是任务已被其他节点抢占，返回特定错误以便上层识别
				return errs.ErrTaskPreemptFailed
			}

			// 2. 在同一个事务中查询，保证读取到的是刚刚更新的数据快照
			var task Task
			if err := tx.
				Where("id = ?", id).
				Table(dst.Table).
				First(&task).Error; err != nil {
				return err // 事务将自动回滚
			}
			acquiredTask = &task
			return nil // 提交事务
		})
	if err != nil {
		return nil, err
	}

	return acquiredTask, nil
}

func (s ShardingTaskDAO) Renew(ctx context.Context, scheduleNodeID string) error {
	dsts := s.shardingStrategy.Broadcast()
	now := time.Now().UnixMilli()

	dbTables := make(map[string][]string)
	for _, dst := range dsts {
		dbTables[dst.DB] = append(dbTables[dst.DB], dst.Table)
	}

	var g errgroup.Group
	for dbName, tables := range dbTables {
		dbName := dbName
		tables := tables

		g.Go(func() error {
			db, ok := s.dbs[dbName]
			if !ok {
				return fmt.Errorf("database %s not found", dbName)
			}

			var sqlStatements []string
			for _, table := range tables {
				sqlStatements = append(sqlStatements, fmt.Sprintf(
					"UPDATE %s SET version = version + 1, utime = %d WHERE schedule_node_id = '%s' AND status = '%s'",
					table, now, scheduleNodeID, StatusPreempted))
			}

			combinedSQL := strings.Join(sqlStatements, "; ")

			if err := db.WithContext(ctx).Exec(combinedSQL).Error; err != nil {
				return fmt.Errorf("%w: 批量续约数据库操作失败 (DB: %s): %w", errs.ErrTaskRenewFailed, dbName, err)
			}
			return nil
		})
	}

	return g.Wait()
}

func (s ShardingTaskDAO) Release(ctx context.Context, id int64, scheduleNodeID string) (*Task, error) {
	bizID := idPkg.ExtractShardingID(id)
	dst := s.shardingStrategy.Shard(bizID)
	db, ok := s.dbs[dst.DB]
	if !ok {
		return nil, fmt.Errorf("database %s not found", dst.DB)
	}

	var releasedTask *Task
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Table(dst.Table).
			Where("id = ? AND status = ? AND schedule_node_id = ?", id, StatusPreempted, scheduleNodeID).
			Updates(map[string]any{
				"status":           StatusActive,
				"schedule_node_id": gorm.Expr("NULL"),
				"version":          gorm.Expr("version + 1"),
				"utime":            time.Now().UnixMilli(),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errs.ErrTaskReleaseFailed
		}

		var task Task
		if err := tx.Table(dst.Table).Where("id = ?", id).First(&task).Error; err != nil {
			return err
		}
		releasedTask = &task
		return nil
	})
	if err != nil {
		return nil, err
	}
	return releasedTask, nil
}

func (s ShardingTaskDAO) UpdateNextTime(ctx context.Context, id, version, nextTime int64) (*Task, error) {
	bizID := idPkg.ExtractShardingID(id)
	dst := s.shardingStrategy.Shard(bizID)
	db, ok := s.dbs[dst.DB]
	if !ok {
		return nil, fmt.Errorf("database %s not found", dst.DB)
	}

	var updatedTask *Task
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Table(dst.Table).
			Where("id = ? AND version = ?", id, version).
			Updates(map[string]any{
				"next_time": nextTime,
				"version":   gorm.Expr("version + 1"),
				"utime":     time.Now().UnixMilli(),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errs.ErrTaskUpdateNextTimeFailed
		}

		var task Task
		if err := tx.Table(dst.Table).Where("id = ?", id).First(&task).Error; err != nil {
			return err
		}
		updatedTask = &task
		return nil
	})

	if err != nil {
		return nil, err
	}

	return updatedTask, nil
}

func (s ShardingTaskDAO) UpdateScheduleParams(ctx context.Context, id, version int64, scheduleParams map[string]string) (*Task, error) {
	bizID := idPkg.ExtractShardingID(id)
	dst := s.shardingStrategy.Shard(bizID)
	db, ok := s.dbs[dst.DB]
	if !ok {
		return nil, fmt.Errorf("database %s not found", dst.DB)
	}

	var updatedTask *Task
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Table(dst.Table).
			Where("id = ? AND version = ?", id, version).
			Updates(map[string]any{
				"schedule_params": sqlx.JsonColumn[map[string]string]{Val: scheduleParams, Valid: scheduleParams != nil},
				"version":         gorm.Expr("version + 1"),
				"utime":           time.Now().UnixMilli(),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errs.ErrTaskUpdateScheduleParamsFailed
		}

		var task Task
		if err := tx.Table(dst.Table).Where("id = ?", id).First(&task).Error; err != nil {
			return err
		}
		updatedTask = &task
		return nil
	})

	if err != nil {
		return nil, err
	}

	return updatedTask, nil
}

// GetShardingStrategy returns the sharding strategy used by this DAO
func (s ShardingTaskDAO) GetShardingStrategy() sharding.ShardingStrategy {
	return s.shardingStrategy
}
