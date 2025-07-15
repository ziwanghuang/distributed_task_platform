package ioc

import (
	"gitee.com/flycash/distributed_task_platform/internal/repository"
	"gitee.com/flycash/distributed_task_platform/internal/service/acquirer"
)

func InitMySQLTaskAcquirer(taskRepo repository.TaskRepository) acquirer.TaskAcquirer {
	return acquirer.NewTaskAcquirer(taskRepo)
}
