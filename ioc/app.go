package ioc

import (
	"context"

	"gitee.com/flycash/distributed_task_platform/internal/service/scheduler"

	"github.com/gotomicro/ego/server/egrpc"
)

// Task 调度平台上的长任务接口。
// 所有补偿器（Retry/Reschedule/Interrupt/Sharding）和 MQ 消费者都实现该接口。
// 通过 SchedulerApp.StartTasks 在独立 goroutine 中启动。
type Task interface {
	Start(ctx context.Context)
}

// SchedulerApp 调度器应用的顶层容器，聚合了调度器的所有组件。
type SchedulerApp struct {
	GRPC      *egrpc.Component       // gRPC 服务端组件，提供管理接口
	Scheduler *scheduler.Scheduler   // 调度器核心
	Tasks     []Task                 // 长任务列表（补偿器 + MQ 消费者）
}

// StartTasks 启动所有长任务，每个任务在独立 goroutine 中运行。
// 这些任务会一直运行直到 ctx 被取消（优雅停机时）。
func (a *SchedulerApp) StartTasks(ctx context.Context) {
	for _, t := range a.Tasks {
		go func(t Task) {
			t.Start(ctx)
		}(t)
	}
}
