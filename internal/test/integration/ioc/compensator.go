package ioc

import (
	"gitee.com/flycash/distributed_task_platform/internal/compensator"
	"gitee.com/flycash/distributed_task_platform/internal/service/runner"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"time"
)

func InitRetryCompensator(
	taskSvc task.Service,
	execSvc task.ExecutionService,
	runner runner.Runner,
) *compensator.RetryCompensator {
	cfg := compensator.RetryConfig{
		MaxRetryCount:          3,
		PrepareTimeoutWindowMs: 10000000000,
		BatchSize:              10,
		MinDuration:            10 * time.Second,
	}
	return compensator.NewRetryCompensator(
		taskSvc,
		execSvc,
		runner,
		cfg,
	)
}
