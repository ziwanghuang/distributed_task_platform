// Package runner 定义了任务运行器（Runner）的抽象接口。
//
// Runner 是调度平台中负责"执行一次任务调度"的核心抽象，
// 它封装了从"创建执行记录"到"发送执行请求"再到"状态更新"的完整流程。
//
// Runner 的实现采用装饰器模式组装：
//
//	Dispatcher（路由层）
//	├── PlanRunner      —— 处理 DAG 工作流类型的任务
//	└── NormalRunner     —— 处理普通/分片类型的任务
//	    └── Invoker      —— 实际发送执行请求到执行节点
//
// Dispatcher 根据任务类型（Plan/Normal/Sharding）路由到对应的 Runner 实现。
package runner

import (
	"context"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
)

// Runner 定义了任务运行器的统一接口。
// 所有任务类型（普通任务、分片任务、DAG 工作流）的运行器都必须实现此接口。
type Runner interface {
	// Run 执行一次任务调度。
	// 完整流程：创建执行记录 → 设置执行状态为 Running → 调用 Invoker 发送执行请求 → 处理执行结果。
	// 参数 task 为任务定义（不是执行记录），Runner 会在内部创建新的执行记录。
	Run(ctx context.Context, task domain.Task) error
	// Retry 重试一次失败的任务执行。
	// 与 Run 不同，Retry 不创建新的执行记录，而是在现有执行记录上递增 RetryCount
	// 并重新发送执行请求。
	Retry(ctx context.Context, execution domain.TaskExecution) error
	// Reschedule 重新调度一次任务执行。
	// 当执行节点主动请求重调度时（如节点负载过高），创建新的执行记录并重新派发。
	// 原执行记录的状态会被更新为对应的终态。
	Reschedule(ctx context.Context, execution domain.TaskExecution) error
}
