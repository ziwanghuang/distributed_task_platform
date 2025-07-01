package errs

import "errors"

var (
	// 任务相关错误
	ErrTaskNotFound         = errors.New("task not found")
	ErrTaskAlreadyPreempted = errors.New("task already preempted")
	ErrTaskNotPreempted     = errors.New("task not preempted")
	ErrTaskVersionMismatch  = errors.New("task version mismatch")

	// 任务执行相关错误
	ErrExecutionNotFound      = errors.New("execution not found")
	ErrExecutionAlreadyExists = errors.New("execution already exists")
	ErrExecutionRetryable     = errors.New("execution retryable")

	// 调度相关错误
	ErrNoSchedulableTask = errors.New("no schedulable task found")
	ErrSchedulerStopped  = errors.New("scheduler is stopped")
)
