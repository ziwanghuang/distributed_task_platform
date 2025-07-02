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

func (t TaskExecutionStatus) String() string {
	return string(t)
}

// TaskExecution 任务执行实例领域模型
type TaskExecution struct {
	ID int64
	// 执行自身信息
	StartTime       int64 // 开始时间戳
	EndTime         int64 // 结束时间戳
	RetryCount      int64
	NextRetryTime   int64 // 下次重试时间戳
	RunningProgress int32 // 执行进度 0-100（RUNNING状态下有效）
	Status          TaskExecutionStatus
	CTime           int64 // 创建时间戳
	UTime           int64 // 更新时间戳
	// 创建时刻从Task冗余的信息
	Task Task
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

// GRPCParams 获取gRPC执行参数（业务参数 + 调度参数）
// 调度参数优先级更高，会覆盖同名的业务参数
func (te *TaskExecution) GRPCParams() map[string]string {
	result := make(map[string]string)

	// 1. 先添加业务参数
	if te.Task.GrpcConfig != nil && te.Task.GrpcConfig.Params != nil {
		for k, v := range te.Task.GrpcConfig.Params {
			result[k] = v
		}
	}

	// 2. 添加/覆盖调度参数（优先级更高）
	if te.Task.ScheduleParams != nil {
		for k, v := range te.Task.ScheduleParams {
			result[k] = v
		}
	}

	return result
}

// HTTPParams 获取HTTP执行参数（业务参数 + 调度参数）
// 调度参数优先级更高，会覆盖同名的业务参数
func (te *TaskExecution) HTTPParams() map[string]string {
	result := make(map[string]string)

	// 1. 先添加业务参数
	if te.Task.HttpConfig != nil && te.Task.HttpConfig.Params != nil {
		for k, v := range te.Task.HttpConfig.Params {
			result[k] = v
		}
	}

	// 2. 添加/覆盖调度参数（优先级更高）
	if te.Task.ScheduleParams != nil {
		for k, v := range te.Task.ScheduleParams {
			result[k] = v
		}
	}

	return result
}
