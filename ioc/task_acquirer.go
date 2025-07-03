package ioc

import (
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"gitee.com/flycash/distributed_task_platform/scheduler"
)

func InitMySQLTaskAcquirer(taskSvc task.Service) scheduler.TaskAcquirer {
	return scheduler.NewTaskAcquirer(taskSvc)
}
