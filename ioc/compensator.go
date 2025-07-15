package ioc

import (
	"gitee.com/flycash/distributed_task_platform/internal/compensator"
	"gitee.com/flycash/distributed_task_platform/internal/service/acquirer"
	"gitee.com/flycash/distributed_task_platform/internal/service/runner"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"github.com/gotomicro/ego/core/econf"
)

func InitRetryCompensator(
	runner runner.Runner,
	execSvc task.ExecutionService,
) *compensator.RetryCompensator {
	var cfg compensator.RetryConfig
	err := econf.UnmarshalKey("compensator.retry", &cfg)
	if err != nil {
		panic(err)
	}
	return compensator.NewRetryCompensator(
		runner,
		execSvc,
		cfg,
	)
}

func InitRescheduleCompensator(
	runner runner.Runner,
	execSvc task.ExecutionService,
) *compensator.RescheduleCompensator {
	var cfg compensator.RescheduleConfig
	err := econf.UnmarshalKey("compensator.reschedule", &cfg)
	if err != nil {
		panic(err)
	}
	return compensator.NewRescheduleCompensator(
		runner,
		execSvc,
		cfg)
}

func InitShardingCompensator(
	nodeID string,
	taskSvc task.Service,
	execSvc task.ExecutionService,
	taskAcquirer acquirer.TaskAcquirer,
) *compensator.ShardingCompensator {
	var cfg compensator.ShardingConfig
	err := econf.UnmarshalKey("compensator.sharding", &cfg)
	if err != nil {
		panic(err)
	}
	return compensator.NewShardingCompensator(
		nodeID,
		taskSvc,
		execSvc,
		taskAcquirer,
		cfg)
}
