package ioc

import (
	"gitee.com/flycash/distributed_task_platform/internal/compensator"
	"gitee.com/flycash/distributed_task_platform/internal/service/scheduler"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"github.com/gotomicro/ego/core/econf"
)

func InitRetryCompensator(
	taskSvc task.Service,
	execSvc task.ExecutionService,
	scheduler *scheduler.Scheduler,
) *compensator.RetryCompensator {
	var cfg compensator.Config
	err := econf.UnmarshalKey("compensator.retry", &cfg)
	if err != nil {
		panic(err)
	}
	return compensator.NewRetryCompensator(
		taskSvc,
		execSvc,
		scheduler,
		cfg,
	)
}
