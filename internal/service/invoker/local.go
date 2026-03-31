package invoker

import (
	"context"
	"fmt"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
)

// 编译期断言 LocalInvoker 实现了 Invoker 接口
var _ Invoker = &LocalInvoker{}

// LocalExecuteFunc 是本地任务执行函数的类型定义。
// 接收执行上下文和执行记录，返回执行状态。
type (
	LocalExecuteFunc func(ctx context.Context, execution domain.TaskExecution) (domain.ExecutionState, error)
	// LocalPrepareFunc 是本地任务准备函数的类型定义。
	// 接收执行上下文和执行记录，返回业务元数据（如分片总数）。
	LocalPrepareFunc func(ctx context.Context, execution domain.TaskExecution) (map[string]string, error)
)

// LocalInvoker 本地调用器，在调度进程内直接执行任务。
//
// 适用场景：
//   - 开发/测试环境中，无需部署独立的执行节点
//   - 用户自部署 scheduler 且希望在同一进程内运行任务逻辑
//   - 轻量级任务（如数据清理、状态检查）不值得走远程调用
//
// 使用方式：通过 executeFuncs 和 prepareFuncs 两个 map 注册任务名对应的处理函数，
// Run/Prepare 时按任务名查找并直接调用。
type LocalInvoker struct {
	executeFuncs map[string]LocalExecuteFunc // 任务名 → 执行函数的映射
	prepareFuncs map[string]LocalPrepareFunc // 任务名 → 准备函数的映射
}

// NewLocalInvoker 创建本地调用器实例。
// 参数 executeFuncs 和 prepareFuncs 分别注册任务名对应的执行和准备函数。
func NewLocalInvoker(executeFuncs map[string]LocalExecuteFunc, prepareFuncs map[string]LocalPrepareFunc) *LocalInvoker {
	return &LocalInvoker{executeFuncs: executeFuncs, prepareFuncs: prepareFuncs}
}

func (l *LocalInvoker) Name() string {
	return "LOCAL"
}

// Run 在本地进程内直接执行任务。
// 根据任务名查找注册的 LocalExecuteFunc 并调用，未注册则返回错误。
func (l *LocalInvoker) Run(ctx context.Context, execution domain.TaskExecution) (domain.ExecutionState, error) {
	fn, ok := l.executeFuncs[execution.Task.Name]
	if !ok {
		return domain.ExecutionState{}, fmt.Errorf("未注册方法：%s", execution.Task.Name)
	}
	return fn(ctx, execution)
}

// Prepare 在本地进程内直接获取业务元数据。
// 根据任务名查找注册的 LocalPrepareFunc 并调用，未注册则返回错误。
func (l *LocalInvoker) Prepare(ctx context.Context, execution domain.TaskExecution) (map[string]string, error) {
	fn, ok := l.prepareFuncs[execution.Task.Name]
	if !ok {
		return nil, fmt.Errorf("未注册方法：%s", execution.Task.Name)
	}
	return fn(ctx, execution)
}
