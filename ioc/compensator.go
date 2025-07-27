package ioc

import (
	executorv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/executor/v1"
	"gitee.com/flycash/distributed_task_platform/internal/compensator"
	"gitee.com/flycash/distributed_task_platform/internal/service/acquirer"
	"gitee.com/flycash/distributed_task_platform/internal/service/runner"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"gitee.com/flycash/distributed_task_platform/pkg/grpc"
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

func InitInterruptCompensator(
	grpcClients *grpc.ClientsV2[executorv1.ExecutorServiceClient],
	execSvc task.ExecutionService,
) *compensator.InterruptCompensator {
	var cfg compensator.InterruptConfig
	err := econf.UnmarshalKey("compensator.interrupt", &cfg)
	if err != nil {
		panic(err)
	}
	return compensator.NewInterruptCompensator(
		grpcClients,
		execSvc,
		cfg,
	)
}
