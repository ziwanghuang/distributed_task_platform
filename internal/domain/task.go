package domain

import (
	"time"

	"github.com/robfig/cron/v3"
)

// TaskStatus 任务状态
type TaskStatus string

const (
	TaskStatusActive    TaskStatus = "ACTIVE"    // 可调度
	TaskStatusPreempted TaskStatus = "PREEMPTED" // 已抢占
	TaskStatusInactive  TaskStatus = "INACTIVE"  // 停止执行
)

func (t TaskStatus) String() string {
	return string(t)
}

type TaskExecutorType string

const (
	TaskExecutorTypeLocal  TaskExecutorType = "local"
	TaskExecutorTypeRemote TaskExecutorType = "remote"
)

func (t TaskExecutorType) String() string {
	return string(t)
}

func (t TaskExecutorType) IsLocal() bool {
	return t == TaskExecutorTypeLocal
}

// Task 任务领域模型
type Task struct {
	ID             int64
	Name           string
	CronExpr       string
	ExecutorType   TaskExecutorType
	GrpcConfig     *GrpcConfig
	HttpConfig     *HttpConfig
	RetryConfig    *RetryConfig
	ScheduleNodeID string
	NextTime       int64 // 下次执行时间戳
	Status         TaskStatus
	Version        int64
	Utime          int64 // 更新时间戳
	CTime          int64 // 创建时间戳
}

func (t *Task) SetNextTime() {
	p := cron.NewParser(
		cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
	)
	s, _ := p.Parse(t.CronExpr)
	t.NextTime = s.Next(time.Now()).UnixMilli()
}

// RetryConfig 重试配置
type RetryConfig struct {
	MaxRetries      int32   `json:"maxRetries"`
	InitialInterval int64   `json:"initialInterval"` // 毫秒
	MaxInterval     int64   `json:"maxInterval"`     // 毫秒
	Multiplier      float64 `json:"multiplier"`
}

// GrpcConfig gRPC配置
type GrpcConfig struct {
	ServiceName string `json:"serviceName"`
}

// HttpConfig HTTP配置
type HttpConfig struct {
	Endpoint string `json:"endpoint"`
}
