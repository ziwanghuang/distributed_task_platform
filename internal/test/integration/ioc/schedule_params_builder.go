package ioc

import (
	"gitee.com/flycash/distributed_task_platform/internal/service/invoker"
	"gitee.com/flycash/distributed_task_platform/pkg/scheduleparams"
)

func InitShardingRuleScheduleParamBuilder(
	invoker invoker.Invoker,
) scheduleparams.Builder {
	return scheduleparams.NewDispatcherBuilder(
		scheduleparams.NewRangeBuilder(),
		scheduleparams.NewWeightedDynamicRangeBuilder(invoker),
	)
}
