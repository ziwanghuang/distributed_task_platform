package domain

// Report 进度上报结构
type Report struct {
	ID              int64               // 执行实例ID
	TaskID          int64               // 任务ID
	TaskName        string              // 任务名称
	Status          TaskExecutionStatus // 执行状态
	RunningProgress int32               // 进度 0-100，RUNNING 状态才有意义
	// 执行节点请求调度节点执行重调度，调度节点无需调interrupt，调度节点可以直接重调度，因为此时执行节点必然已经停止了
	RequestReschedule bool
	RescheduleParams  map[string]string
}
