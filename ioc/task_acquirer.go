package ioc

import (
	"gitee.com/flycash/distributed_task_platform/internal/service/acquirer"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
)

func InitMySQLTaskAcquirer(taskSvc task.Service) acquirer.TaskAcquirer {
	return acquirer.NewTaskAcquirer(taskSvc)
}
