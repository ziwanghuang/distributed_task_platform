package domain

import (
	executorv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/executor/v1"
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
	// 执行节点的 nodeID，用于记录是哪个节点处理了任务
	ExecutorNodeID string `json:"executorNodeId"`
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
		ExecutorNodeID:    protoState.GetExecutorNodeId(),
	}
}

type BatchReport struct {
	Reports []*Report `json:"reports"`
}
