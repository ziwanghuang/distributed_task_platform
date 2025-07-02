package domain

// Report 进度上报结构
type Report struct {
	TaskID      int64               // 任务ID（通过ExecutionID查询获取）
	TaskName    string              // 任务名称（从protobuf直接获取）
	ExecutionID int64               // 执行实例ID（从protobuf直接获取）
	Status      TaskExecutionStatus // 执行状态（转换获取）
	Progress    int32               // 进度 0-100（从protobuf直接获取）
}
