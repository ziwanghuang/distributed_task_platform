package ioc

import (
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"gitee.com/flycash/distributed_task_platform/pkg/acquirer"
)

func InitMySQLTaskAcquirer(taskSvc task.Service) acquirer.TaskAcquirer {
	return acquirer.NewTaskAcquirer(taskSvc)
}
