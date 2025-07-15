package ioc

import (
	"time"

	"gitee.com/flycash/distributed_task_platform/internal/compensator"
	"gitee.com/flycash/distributed_task_platform/internal/service/runner"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
)

func InitRetryCompensator(
	execSvc task.ExecutionService,
	runner runner.Runner,
) *compensator.RetryCompensator {
	const maxRetryCount = 3
	const prepareTimeoutWindowMs = 10000000000
	const batchSize = 10
	const minDuration = 10 * time.Second
	cfg := compensator.RetryConfig{
		MaxRetryCount:          maxRetryCount,
		PrepareTimeoutWindowMs: prepareTimeoutWindowMs,
		BatchSize:              batchSize,
		MinDuration:            minDuration,
	}
	return compensator.NewRetryCompensator(
		runner,
		execSvc,
		cfg,
	)
}
