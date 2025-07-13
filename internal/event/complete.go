package event

import "gitee.com/flycash/distributed_task_platform/internal/domain"

type Event struct {
	// 任务对应的 planid
	PlanID         int64                      `json:"planId"`
	ExecID         int64                      `json:"execId"`
	TaskID         int64                      `json:"taskId"`
	Version        int64                      `json:"version"`
	ScheduleNodeID string                     `json:"scheduleNodeId"`
	Type           domain.TaskType            `json:"type"`
	ExecStatus     domain.TaskExecutionStatus `json:"execStatus"`
	Name           string                     `json:"name"`
}
