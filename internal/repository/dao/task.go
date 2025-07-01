package dao

import (
	"database/sql"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"github.com/ecodeclub/ekit/sqlx"
)

// Task 任务表DAO对象
type Task struct {
	Id             int64                               `gorm:"type:bigint;primary_key;auto_increment;"`
	Name           string                              `gorm:"type:varchar(255);not null;uniqueIndex:uniq_idx_name;comment:'任务名称'"`
	CronExpr       string                              `gorm:"type:varchar(100);not null;comment:'cron表达式'"`
	ExecutorType   string                              `gorm:"type:ENUM('LOCAL', 'REMOTE');not null;default:'REMOTE';comment:'任务执行方式：LOCAL-本地执行，REMOTE-远程执行'"`
	GrpcConfig     sqlx.JsonColumn[domain.GrpcConfig]  `gorm:"type:json;comment:'gRPC配置：{\"serviceName\": \"user-service\"}'"`
	HttpConfig     sqlx.JsonColumn[domain.HttpConfig]  `gorm:"type:json;comment:'HTTP配置：{\"endpoint\": \"https://host:port/api\"}'"`
	RetryConfig    sqlx.JsonColumn[domain.RetryConfig] `gorm:"type:json;comment:'重试配置'"`
	ScheduleNodeId sql.NullString                      `gorm:"type:varchar(255);comment:'当前抢占的调度节点ID'"`
	NextTime       int64                               `gorm:"type:bigint;not null;index:idx_next_time_status;comment:'下次执行时间'"`
	Status         string                              `gorm:"type:ENUM('ACTIVE', 'PREEMPTED', 'INACTIVE');not null;default:'ACTIVE';index:idx_next_time_status;comment:'任务状态: ACTIVE-可调度, PREEMPTED-已抢占, INACTIVE-停止执行'"`
	Version        int64                               `gorm:"type:bigint;not null;default:1;comment:'版本号，用于乐观锁'"`
	Ctime          int64                               `gorm:"comment:'创建时间'"`
	Utime          int64                               `gorm:"comment:'更新时间'"`
}

// TableName 指定表名
func (Task) TableName() string {
	return "tasks"
}
