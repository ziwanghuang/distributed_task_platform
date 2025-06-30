package dao

import (
	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"github.com/ecodeclub/ekit/sqlx"
)

// TaskExecution 任务执行记录表DAO对象
type TaskExecution struct {
	Id int64 `gorm:"type:bigint;primary_key;auto_increment"`
	// 下面都是创建当前 TaskExecution时从对应的Task直接拷贝过来的冗余信息
	TaskId           int64                               `gorm:"type:bigint;not null;comment:'任务ID'"`
	TaskName         string                              `gorm:"type:varchar(255);not null;comment:'任务名称'"`
	TaskCronExpr     string                              `gorm:"type:varchar(100);not null;comment:'cron表达式'"`
	TaskGrpcConfig   sqlx.JsonColumn[domain.GrpcConfig]  `gorm:"type:json;comment:'gRPC配置：{\"serviceName\": \"user-service\"}'"`
	TaskHttpConfig   sqlx.JsonColumn[domain.HttpConfig]  `gorm:"type:json;comment:'HTTP配置：{\"endpoint\": \"https://host:port/api\"}'"`
	TaskRetryConfig  sqlx.JsonColumn[domain.RetryConfig] `gorm:"type:json;comment:'重试配置'"`
	TaskVersion      int64                               `gorm:"type:bigint;not null;comment:'创建时Task的版本号'"`
	TaskScheduleNode string                              `gorm:"type:varchar(255);not null;comment:'创建此执行的调度节点ID'"`

	// 下面这些是 TaskExecution 的自身信息
	Stime         int64  `gorm:"type:bigint;comment:'开始时间'"`
	Etime         int64  `gorm:"type:bigint;comment:'结束时间'"`
	RetryCnt      int    `gorm:"type:int;not null;default:0;comment:'重试次数'"`
	NextRetryTime int64  `gorm:"type:bigint;comment:'下次重试时间'"`
	Status        string `gorm:"type:ENUM('PREPARE', 'RUNNING', 'FAILED_RETRYABLE', 'FAILED_PREEMPTED', 'FAILED', 'SUCCESS');not null;default:'PREPARE';comment:'执行状态: PREPARE-初始化, RUNNING-执行中, FAILED_RETRYABLE-可重试失败, FAILED_PREEMPTED-因续约失败导致的抢占失败， FAILED-失败, SUCCESS-成功'"`
	Ctime         int64  `gorm:"comment:'创建时间'"`
	Utime         int64  `gorm:"comment:'更新时间'"`
}

// TableName 指定表名
func (TaskExecution) TableName() string {
	return "task_executions"
}
