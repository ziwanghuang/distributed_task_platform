package domain

// TaskExecutionStatus 任务执行状态
type TaskExecutionStatus string

const (
	TaskExecutionStatusPrepare         TaskExecutionStatus = "PREPARE"          // 已创建，准备执行
	TaskExecutionStatusRunning         TaskExecutionStatus = "RUNNING"          // 正在执行
	TaskExecutionStatusSuccess         TaskExecutionStatus = "SUCCESS"          // 执行成功
	TaskExecutionStatusFailed          TaskExecutionStatus = "FAILED"           // 执行失败（不可重试）
	TaskExecutionStatusFailedRetryable TaskExecutionStatus = "FAILED_RETRYABLE" // 执行失败（可重试）
	TaskExecutionStatusFailedPreempted TaskExecutionStatus = "FAILED_PREEMPTED" // 因续约失败导致的抢占失败
)

// TaskExecution 任务执行实例领域模型
type TaskExecution struct {
	ID int64
	// Task快照信息
	TaskID             int64
	TaskName           string
	TaskCronExpr       string
	TaskExecutorType   TaskExecutorType
	TaskGrpcConfig     *GrpcConfig
	TaskHttpConfig     *HttpConfig
	TaskRetryConfig    *RetryConfig
	TaskVersion        int64
	TaskScheduleNodeID string
	// 执行自身信息
	StartTime     int64 // 开始时间戳
	EndTime       int64 // 结束时间戳
	RetryCount    int32
	NextRetryTime int64 // 下次重试时间戳
	Status        TaskExecutionStatus
	CTime         int64 // 创建时间戳
	UTime         int64 // 更新时间戳
}

// IsTerminal 判断是否为终态
func (te *TaskExecution) IsTerminal() bool {
	switch te.Status {
	case TaskExecutionStatusSuccess, TaskExecutionStatusFailed,
		TaskExecutionStatusFailedPreempted:
		return true
	default:
		return false
	}
}
