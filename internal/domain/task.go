package domain

import (
	"time"

	"gitee.com/flycash/distributed_task_platform/pkg/retry"
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
	TaskExecutorTypeLocal  TaskExecutorType = "LOCAL"
	TaskExecutorTypeRemote TaskExecutorType = "REMOTE"
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
	ScheduleParams map[string]string // 调度参数（如分页偏移量、处理进度等）
	NextTime       int64             // 下次执行时间戳
	Status         TaskStatus
	Version        int64 // 版本号，用于乐观锁
	CTime          int64 // 创建时间戳
	UTime          int64 // 更新时间戳
}

func (t *Task) CalculateNextTime() time.Time {
	p := cron.NewParser(
		cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
	)
	s, _ := p.Parse(t.CronExpr)
	return s.Next(time.Now())
}

// RetryConfig 重试配置
type RetryConfig struct {
	MaxRetries      int32 `json:"maxRetries"`
	InitialInterval int64 `json:"initialInterval"` // 毫秒
	MaxInterval     int64 `json:"maxInterval"`     // 毫秒
}

func (r *RetryConfig) ToRetryConfig() retry.Config {
	return retry.Config{
		Type: "exponential",
		ExponentialBackoff: &retry.ExponentialBackoffConfig{
			InitialInterval: time.Duration(r.InitialInterval) * time.Millisecond,
			MaxInterval:     time.Duration(r.MaxInterval) * time.Millisecond,
			MaxRetries:      r.MaxRetries,
		},
	}
}

// GrpcConfig gRPC配置
type GrpcConfig struct {
	ServiceName string            `json:"serviceName"`
	Params      map[string]string `json:"params"`
}

// HttpConfig HTTP配置
type HttpConfig struct {
	Endpoint string            `json:"endpoint"`
	Params   map[string]string `json:"params"`
}
