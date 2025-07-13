package domain

import executorv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/executor/v1"

// TaskExecutionStatus 任务执行状态
type TaskExecutionStatus string

const (
	TaskExecutionStatusUnknown           TaskExecutionStatus = "UNKNOWN"
	TaskExecutionStatusPrepare           TaskExecutionStatus = "PREPARE"            // 已创建，准备执行
	TaskExecutionStatusRunning           TaskExecutionStatus = "RUNNING"            // 正在执行
	TaskExecutionStatusSuccess           TaskExecutionStatus = "SUCCESS"            // 执行成功
	TaskExecutionStatusFailed            TaskExecutionStatus = "FAILED"             // 执行失败（不可重试）
	TaskExecutionStatusFailedRetryable   TaskExecutionStatus = "FAILED_RETRYABLE"   // 执行失败（可重试）
	TaskExecutionStatusFailedRescheduled TaskExecutionStatus = "FAILED_RESCHEDULED" // 执行失败（重调度）
)

func (t TaskExecutionStatus) String() string {
	return string(t)
}

func (t TaskExecutionStatus) IsValid() bool {
	switch t {
	case TaskExecutionStatusPrepare,
		TaskExecutionStatusRunning,
		TaskExecutionStatusSuccess,
		TaskExecutionStatusFailed,
		TaskExecutionStatusFailedRetryable:
		return true
	default:
		return false
	}
}

func (t TaskExecutionStatus) IsPrepare() bool {
	return t == TaskExecutionStatusPrepare
}

func (t TaskExecutionStatus) IsRunning() bool {
	return t == TaskExecutionStatusRunning
}

func (t TaskExecutionStatus) IsSuccess() bool {
	return t == TaskExecutionStatusSuccess
}

func (t TaskExecutionStatus) IsFailed() bool {
	return t == TaskExecutionStatusFailed
}

func (t TaskExecutionStatus) IsFailedRetryable() bool {
	return t == TaskExecutionStatusFailedRetryable
}

func (t TaskExecutionStatus) IsFailedRescheduled() bool {
	return t == TaskExecutionStatusFailedRescheduled
}

func (t TaskExecutionStatus) IsTerminalStatus() bool {
	return t.IsSuccess() || t.IsFailed() || t.IsFailedRetryable() || t.IsFailedRescheduled()
}

func TaskExecutionStatusFromProto(status executorv1.ExecutionStatus) TaskExecutionStatus {
	switch status {
	case executorv1.ExecutionStatus_RUNNING:
		return TaskExecutionStatusRunning
	case executorv1.ExecutionStatus_SUCCESS:
		return TaskExecutionStatusSuccess
	case executorv1.ExecutionStatus_FAILED:
		return TaskExecutionStatusFailed
	case executorv1.ExecutionStatus_FAILED_RETRYABLE:
		return TaskExecutionStatusFailedRetryable
	default:
		return TaskExecutionStatusUnknown
	}
}

func ExecutionStateFromProto(protoState *executorv1.ExecutionState) ExecutionState {
	if protoState == nil {
		return ExecutionState{}
	}
	return ExecutionState{
		ID:                protoState.GetId(),
		TaskID:            protoState.GetTaskId(),
		TaskName:          protoState.GetTaskName(),
		Status:            TaskExecutionStatusFromProto(protoState.GetStatus()),
		RunningProgress:   protoState.GetRunningProgress(),
		RequestReschedule: protoState.GetRequestReschedule(),
		RescheduleParams:  protoState.GetRescheduledParams(),
	}
}

// TaskExecution 任务执行实例领域模型
type TaskExecution struct {
	ID int64
	// 执行自身信息
	ShardingParentID int64
	StartTime        int64 // 开始时间戳
	EndTime          int64 // 结束时间戳
	RetryCount       int64
	NextRetryTime    int64 // 下次重试时间戳
	RunningProgress  int32 // 执行进度 0-100（RUNNING状态下有效）
	Status           TaskExecutionStatus
	CTime            int64 // 创建时间戳
	UTime            int64 // 更新时间戳
	// 创建时刻从Task冗余的信息
	Task       Task
}

func (te *TaskExecution) MergeTaskScheduleParams(scheduleParams map[string]string) {
	if len(scheduleParams) == 0 {
		return
	}
	if len(te.Task.ScheduleParams) == 0 {
		te.Task.ScheduleParams = scheduleParams
	} else {
		// 覆盖
		for k, v := range scheduleParams {
			te.Task.ScheduleParams[k] = v
		}
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
	if te.Task.HTTPConfig != nil && te.Task.HTTPConfig.Params != nil {
		for k, v := range te.Task.HTTPConfig.Params {
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
