package ioc

import (
	"gitee.com/flycash/distributed_task_platform/internal/compensator"
	"gitee.com/flycash/distributed_task_platform/internal/event/reportevt"
)

func InitTasks(
	t1 *compensator.RetryCompensator,
	t2 *compensator.RescheduleCompensator,
	t3 *compensator.ShardingCompensator,
	t4 *reportevt.BatchReportEventConsumer,
	t5 *reportevt.ReportEventConsumer,
) []Task {
	return []Task{
		t1,
		t2,
		t3,
		t4,
		t5,
	}
}
