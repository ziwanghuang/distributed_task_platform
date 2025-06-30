package scheduler

import "gitee.com/flycash/distributed_task_platform/internal/domain"

// ProgressReport 进度上报结构
type ProgressReport struct {
	TaskID      int64                      // 任务ID
	ExecutionID int64                      // 执行实例ID
	Status      domain.TaskExecutionStatus // 执行状态
	Progress    int32                      // 进度 0-100
	IsTerminal  bool                       // 是否为终态
	Error       string                     // 错误信息
	Message     string                     // 额外消息
}
