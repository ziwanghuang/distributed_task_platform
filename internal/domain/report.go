package domain

import (
	executorv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/executor/v1"
	reporterv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/reporter/v1"
)

// Report 进度上报结构
type Report struct {
	ExecutionState ExecutionState `json:"executionState"`
}

type ExecutionState struct {
	ID              int64               `json:"id"`              // 执行实例ID
	TaskID          int64               `json:"taskId"`          // 任务ID
	TaskName        string              `json:"taskName"`        // 任务名称
	Status          TaskExecutionStatus `json:"status"`          // 执行状态
	RunningProgress int32               `json:"runningProgress"` // 进度 0-100，RUNNING 状态才有意义
	// 执行节点请求调度节点执行重调度，调度节点无需调interrupt，调度节点可以直接重调度，因为此时执行节点必然已经停止了
	RequestReschedule bool              `json:"requestReschedule"`
	RescheduleParams  map[string]string `json:"rescheduleParams"`

	// 它将从 scheduler_context 中解析而来
	Type ExecutionType `json:"type"`
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

func ExecutionStateFromReportRequestProto(req *reporterv1.ReportRequest) ExecutionState {
	// 先处理已有的 state 部分
	protoState := req.GetExecutionState()
	if protoState == nil {
		return ExecutionState{}
	}
	state := ExecutionState{
		ID:                protoState.GetId(),
		TaskID:            protoState.GetTaskId(),
		TaskName:          protoState.GetTaskName(),
		Status:            TaskExecutionStatusFromProto(protoState.GetStatus()),
		RunningProgress:   protoState.GetRunningProgress(),
		RequestReschedule: protoState.GetRequestReschedule(),
		RescheduleParams:  protoState.GetRescheduledParams(),
	}

	// 解析 scheduler_context，并填充 Type 字段
	context := req.GetSchedulerContext()
	if context != nil {
		// 从 map 中安全地获取类型字符串
		typeStr := context["type"]
		state.Type = ExecutionType(typeStr)
	}

	// 设置一个安全的默认值
	// 如果 context 中没有提供 type，我们默认它是 NORMAL 类型
	if state.Type == "" {
		state.Type = ExecutionTypeNormal
	}
	return state
}

type BatchReport struct {
	Reports []*Report `json:"reports"`
}
