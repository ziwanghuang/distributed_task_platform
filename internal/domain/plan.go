// Package domain 定义分布式任务调度平台的核心领域模型。
// 包含 Task（任务）、TaskExecution（执行记录）、Plan（DAG 工作流）、
// PlanTask（工作流节点）、Report（状态上报）等领域对象。
// 领域模型是业务逻辑的载体，不依赖任何基础设施（数据库、消息队列等）。
package domain

import (
	"gitee.com/flycash/distributed_task_platform/internal/dsl/parser"
)

// Plan DAG 工作流领域模型。
// 一个 Plan 由多个 Task 组成，通过 DSL 表达式（ExecExpr）定义执行依赖关系。
// 运行时先将 ExecExpr 解析为 AST（ExecPlan），再构建出 DAG 图（Steps + Root）。
//
// 生命周期：
//  1. 创建 Plan 时解析 ExecExpr → 生成 TaskPlan（AST）
//  2. 调度时根据 AST 构建 PlanTask DAG 图，设置前驱/后继关系
//  3. 从 Root 节点开始按拓扑序逐步执行
//  4. 所有节点执行完成后，Plan 状态变为 SUCCESS
type Plan struct {
	ID        int64
	Name      string
	CronExpr  string        // cron 表达式，定义 Plan 的周期性调度规则
	ExecExpr  string        // DSL 执行表达式，定义 Task 之间的依赖关系（如 "A -> B & C -> D"）
	Execution TaskExecution // Plan 整体的执行状态（RUNNING/SUCCESS/FAILED），重试委托给各子任务

	// ExecPlan 是 ExecExpr 解析后的 AST 结果，包含每个 Task 的前驱/后继节点定义
	ExecPlan parser.TaskPlan

	ScheduleNodeID string // 当前抢占此 Plan 的调度节点 ID

	Steps []*PlanTask // 此 Plan 包含的所有任务节点（平铺列表）
	Root  []*PlanTask // DAG 的根节点（没有前驱依赖的任务），调度从这些节点开始

	ScheduleParams map[string]string // 调度参数（如分页偏移量、处理进度等）
	NextTime       int64             // 下次执行时间戳
	Status         TaskStatus
	Version        int64 // 版本号，用于乐观锁
	CTime          int64 // 创建时间戳
	UTime          int64 // 更新时间戳
}

// RootTask 返回 DAG 图的根节点列表。
// 根节点是没有前驱依赖的任务，是 Plan 执行的起点。
func (p Plan) RootTask() []*PlanTask {
	return p.Root
}

// GetTask 根据任务名称查找 Plan 中的任务节点。
// 返回找到的 PlanTask 指针和是否存在的布尔值。
func (p Plan) GetTask(name string) (*PlanTask, bool) {
	for idx := range p.Steps {
		if p.Steps[idx].Name == name {
			return p.Steps[idx], true
		}
	}
	return nil, false
}
