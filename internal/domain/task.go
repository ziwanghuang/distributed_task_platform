package domain

// TaskStatus 任务状态
type TaskStatus string

const (
	TaskStatusActive    TaskStatus = "ACTIVE"    // 可调度
	TaskStatusPreempted TaskStatus = "PREEMPTED" // 已抢占
	TaskStatusInactive  TaskStatus = "INACTIVE"  // 停止执行
)

// Task 任务领域模型
type Task struct {
	ID             int64
	Name           string
	CronExpr       string
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
