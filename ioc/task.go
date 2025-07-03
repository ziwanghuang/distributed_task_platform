package ioc

import (
	"gitee.com/flycash/distributed_task_platform/internal/compensator"
)

func InitTasks(t1 *compensator.RetryCompensator,
) []Task {
	return []Task{
		t1,
	}
}
