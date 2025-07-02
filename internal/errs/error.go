package errs

import "errors"

var (
	// 任务相关错误 - 基础错误类型，用于上层判断和处理
	ErrTaskPreemptFailed              = errors.New("任务抢占失败")
	ErrTaskRenewFailed                = errors.New("任务续约失败")
	ErrTaskReleaseFailed              = errors.New("任务释放失败")
	ErrTaskUpdateNextTimeFailed       = errors.New("任务更新下次执行时间失败")
	ErrTaskUpdateScheduleParamsFailed = errors.New("任务更新调度参数失败")

	// 任务执行相关错误
	ErrExecutionNotFound  = errors.New("执行记录不存在")
	ErrExecutionRetryable = errors.New("执行失败可重试")
	// 业务逻辑错误
	ErrInvalidTaskCronExpr        = errors.New("无效的cron表达式")
	ErrInvalidTaskScheduleNodeID  = errors.New("无效的调度节点ID")
	ErrInvalidTaskExecutionMethod = errors.New("任务执行方式非法")
)
