package parser

import "reflect"

type Node interface {
	// NextNodes 根据上一个任务的执行情况返回，下一步需要执行的任务名
	// 主要是给复合节点使用
	NextNodes(execution Execution) []string
	Type() NodeType
	// 获取该节点的所有的后续节点
	ChildNodes() []string
}

func NodeIsNil(n Node) bool {
	if n == nil {
		return true
	}
	rv := reflect.ValueOf(n)
	if rv.Kind() == reflect.Ptr && rv.IsNil() {
		return true
	}
	return false
}

// Execution 防止循环引用另外定义了一个Execution
type Execution struct {
	Status TaskExecutionStatus
}
type TaskExecutionStatus string

const (
	TaskExecutionStatusUnknown         TaskExecutionStatus = "UNKNOWN"
	TaskExecutionStatusPrepare         TaskExecutionStatus = "PREPARE"          // 已创建，准备执行
	TaskExecutionStatusRunning         TaskExecutionStatus = "RUNNING"          // 正在执行
	TaskExecutionStatusSuccess         TaskExecutionStatus = "SUCCESS"          // 执行成功
	TaskExecutionStatusFailed          TaskExecutionStatus = "FAILED"           // 执行失败（不可重试）
	TaskExecutionStatusFailedRetryable TaskExecutionStatus = "FAILED_RETRYABLE" // 执行失败（可重试）
	TaskExecutionStatusFailedPreempted TaskExecutionStatus = "FAILED_PREEMPTED" // 因续约失败导致的抢占失败
)

func (e TaskExecutionStatus) IsSuccess() bool {
	return e == TaskExecutionStatusSuccess
}

type NodeType string

const (
	NodeTypeSingle    NodeType = "single"
	NodeTypeAnd       NodeType = "and"
	NodeTypeOr        NodeType = "or"
	NodeTypeEnd       NodeType = "end"
	NodeTypeCondition NodeType = "condition"
	NodeTypeLoop      NodeType = "loop"
)

type TaskPlan interface {
	AdjoiningNode(name string) (PlanNode, bool)
	RootNode() []PlanNode
}
