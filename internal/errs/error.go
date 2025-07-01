package errs

import "errors"

var (
	// 任务相关错误 - 基础错误类型，用于上层判断和处理
	ErrTaskPreemptFailed         = errors.New("任务抢占失败")
	ErrTaskRenewFailed           = errors.New("任务续约失败")
	ErrTaskReleaseFailed         = errors.New("任务释放失败")
	ErrInvalidTaskScheduleNodeID = errors.New("任务调度节点ID非法，不能为空")

	// 任务执行相关错误
	ErrExecutionNotFound      = errors.New("执行记录不存在")
	ErrExecutionAlreadyExists = errors.New("执行记录已存在")
	ErrExecutionRetryable     = errors.New("执行失败可重试")
)
