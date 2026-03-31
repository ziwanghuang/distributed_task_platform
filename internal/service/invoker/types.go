// Package invoker 定义了任务执行调用器（Invoker）的抽象接口和多种实现。
//
// Invoker 是调度平台与执行节点之间的桥梁：调度平台决定"何时执行"，
// Invoker 负责"如何将执行请求送达执行节点"。
//
// 架构层次：
//
//	Dispatcher（路由层）
//	├── GRPCInvoker  —— 通过 gRPC 远程调用执行节点
//	├── HTTPInvoker  —— 通过 HTTP 远程调用执行节点
//	└── LocalInvoker —— 在调度进程内直接执行（测试/开发用）
//
// 每个 Invoker 实现需要支持两个操作：
//   - Run: 执行任务并返回执行状态
//   - Prepare: 在正式执行前获取业务元数据（如分片任务的总数量），
//     调度平台据此决定如何分片
package invoker

import (
	"context"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
)

// Invoker 定义了任务执行调用器的统一接口。
// 所有类型的执行调用（gRPC/HTTP/Local）都必须实现此接口。
type Invoker interface {
	// Name 返回调用器的标识名称（如 "GRPC"、"HTTP"、"LOCAL"），用于日志和调试。
	Name() string
	// Run 向执行节点发送执行请求并等待响应。
	// 参数 execution 包含完整的执行上下文（任务定义、执行参数、分片信息等）。
	// 返回值 ExecutionState 包含执行节点上报的最新状态（可能是 Running/Success/Failed）。
	// 注意：对于长时间运行的任务，Run 可能只返回"已开始执行"的状态，
	// 后续状态通过 ReporterService（gRPC 上报）或 MQ 异步上报。
	Run(ctx context.Context, execution domain.TaskExecution) (domain.ExecutionState, error)
	// Prepare 在正式执行前向执行节点查询业务元数据。
	// 主要用于分片任务场景：调用执行节点获取业务数据总量，
	// 调度平台据此按分片规则拆分子任务。
	// 返回值为 key-value 形式的元数据，如 {"totalCount": "10000"}。
	Prepare(ctx context.Context, execution domain.TaskExecution) (map[string]string, error)
}
