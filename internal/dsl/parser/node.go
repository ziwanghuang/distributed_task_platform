package parser

import "github.com/ecodeclub/ekit/slice"

type SimpleNode struct {
	name string
}

func NewSimpleTask(name string) SimpleNode {
	return SimpleNode{name: name}
}

func (s SimpleNode) NodeName(execution Execution) []string {
	// 前一个任务执行成功返回所有任务节点
	if execution.Status.IsSuccess() {
		return []string{s.name}
	}
	return []string{}
}

func (s SimpleNode) Type() NodeType {
	return NodeTypeSingle
}

func (s SimpleNode) AllNodeName() []string {
	return []string{s.name}
}

// 简化语法 && || 只能是单体任务不能是组合
type AndNode struct {
	tasks []SimpleNode
}

func NewAndTask(tasks ...SimpleNode) AndNode {
	return AndNode{tasks: tasks}
}

func (a AndNode) NodeName(execution Execution) []string {
	if execution.Status.IsSuccess() {
		return slice.Map(a.tasks, func(_ int, src SimpleNode) string {
			return src.name
		})
	}
	// 前一个任务执行失败下面就不执行了
	return []string{}
}

func (a AndNode) Type() NodeType {
	return NodeTypeAnd
}

func (a AndNode) AllNodeName() []string {
	return slice.Map(a.tasks, func(_ int, src SimpleNode) string {
		return src.name
	})
}

type OrNode struct {
	tasks []SimpleNode
}

func NewOrTask(tasks ...SimpleNode) OrNode {
	return OrNode{tasks: tasks}
}

func (o OrNode) NodeName(execution Execution) []string {
	if execution.Status.IsSuccess() {
		return slice.Map(o.tasks, func(_ int, src SimpleNode) string {
			return src.name
		})
	}
	return []string{}
}

func (o OrNode) Type() NodeType {
	return NodeTypeOr
}

func (o OrNode) AllNodeName() []string {
	return slice.Map(o.tasks, func(_ int, src SimpleNode) string {
		return src.name
	})
}

type EndNode struct {
	name string
}

func NewEndNode(name string) EndNode {
	return EndNode{
		name: name,
	}
}

func (e EndNode) NodeName(execution Execution) []string {
	if execution.Status.IsSuccess() {
		return []string{
			e.name,
		}
	}
	return []string{}
}

func (e EndNode) Type() NodeType {
	return NodeTypeEnd
}

func (e EndNode) AllNodeName() []string {
	return []string{
		e.name,
	}
}

type ConditionNode struct {
	successTask SimpleNode
	failureTask SimpleNode
}

func NewConditionTask(successTask, failureTask SimpleNode) ConditionNode {
	return ConditionNode{successTask: successTask, failureTask: failureTask}
}

func (e ConditionNode) NodeName(execution Execution) []string {
	if execution.Status.IsSuccess() {
		return []string{
			e.successTask.name,
		}
	}
	return []string{
		e.failureTask.name,
	}
}

func (e ConditionNode) Type() NodeType {
	return NodeTypeCondition
}

func (e ConditionNode) AllNodeName() []string {
	return []string{
		e.successTask.name,
		e.failureTask.name,
	}
}
