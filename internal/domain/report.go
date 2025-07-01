package domain

// Report 进度上报结构
type Report struct {
	TaskID      int64               // 任务ID
	ExecutionID int64               // 执行实例ID
	Status      TaskExecutionStatus // 执行状态
	Progress    int32               // 进度 0-100
}
