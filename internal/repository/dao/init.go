// Package dao 是数据访问对象层，直接操作数据库表。
//
// 本包定义了两个核心 DAO：
//   - TaskDAO: 操作 tasks 表，实现任务定义的 CRUD 和 CAS 抢占机制
//   - TaskExecutionDAO: 操作 task_executions 表，实现执行记录的生命周期管理
//
// 此外还有对应的分库分表版本（sharding_task.go / sharding_task_execution.go），
// 通过 ShardingStrategy 动态路由到不同的库表。
//
// CAS 抢占机制说明：
//   tasks 表通过 version（乐观锁）+ status（ACTIVE/PREEMPTED/INACTIVE）实现分布式任务抢占。
//   Acquire 时检查 version 匹配后将状态改为 PREEMPTED，Release 时改回 ACTIVE。
//   通过 Renew 定期更新 utime 防止被判定为僵尸任务。
package dao

import "github.com/ego-component/egorm"

// InitTables 自动迁移数据库表结构。
// 在应用启动时调用，基于 GORM AutoMigrate 自动创建或更新 tasks 和 task_executions 表。
// 注意：仅适用于非分库分表模式，分库分表模式需要手动建表。
func InitTables(db *egorm.Component) error {
	return db.AutoMigrate(
		&Task{},
		&TaskExecution{},
	)
}
