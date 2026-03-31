// Package event 定义了任务调度平台内部使用的异步事件模型。
//
// 本包中的事件通过消息队列（Kafka）在调度平台的各组件之间异步传递，
// 实现了调度核心逻辑与事件处理逻辑的解耦。
//
// 主要事件类型：
//   - Event（完成事件）：任务执行完成后发布，触发 DAG 工作流的后续节点调度
package event

import "gitee.com/flycash/distributed_task_platform/internal/domain"

// Event 表示任务执行完成事件，当一个任务执行到达终态（成功/失败）时发布到 Kafka。
//
// 该事件的主要消费场景：
//   - DAG 工作流（Plan）中，当一个节点任务完成后，消费者检查后续节点的前驱依赖
//     是否全部满足，若满足则触发后续节点的调度执行
//   - 分片任务中，子任务完成事件可触发父任务状态的汇总更新
//
// 字段说明：
//   - PlanID: 任务所属的 DAG 工作流 ID，如果为 0 表示非工作流任务
//   - ExecID: 任务执行记录 ID，全局唯一
//   - TaskID: 任务定义 ID
//   - Version: 任务版本号，用于乐观锁校验
//   - ScheduleNodeID: 调度该任务的节点标识
//   - Type: 任务类型（Normal/Sharding/Plan 等）
//   - ExecStatus: 执行终态（Success/Failed）
//   - Name: 任务名称，在 DAG 中用于节点匹配
type Event struct {
	PlanID         int64                      `json:"planId"`
	ExecID         int64                      `json:"execId"`
	TaskID         int64                      `json:"taskId"`
	Version        int64                      `json:"version"`
	ScheduleNodeID string                     `json:"scheduleNodeId"`
	Type           domain.TaskType            `json:"type"`
	ExecStatus     domain.TaskExecutionStatus `json:"execStatus"`
	Name           string                     `json:"name"`
}
