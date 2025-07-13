package ioc

import (
	"gitee.com/flycash/distributed_task_platform/internal/repository"
	"gitee.com/flycash/distributed_task_platform/internal/service/acquirer"
)

func InitMySQLTaskAcquirer(repo repository.TaskRepository) acquirer.TaskAcquirer {
	return acquirer.NewTaskAcquirer(repo)
}
