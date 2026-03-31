package ioc

import (
	"gitee.com/flycash/distributed_task_platform/internal/repository"
	"gitee.com/flycash/distributed_task_platform/internal/service/acquirer"
)

// InitMySQLTaskAcquirer 初始化基于 MySQL 的任务获取器。
// TaskAcquirer 负责从 MySQL 中获取到期待调度的任务，
// 使用 CAS（Compare-And-Swap）乐观锁机制实现多节点竞争抢占，
// 确保同一任务只被一个调度节点获取。
func InitMySQLTaskAcquirer(taskRepo repository.TaskRepository) acquirer.TaskAcquirer {
	return acquirer.NewTaskAcquirer(taskRepo)
}
