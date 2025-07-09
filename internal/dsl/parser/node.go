package parser

import "github.com/ecodeclub/ekit/slice"

type SimpleNode struct {
	name string
}

func NewSimpleTask(name string) SimpleNode {
	return SimpleNode{name: name}
}

func (s SimpleNode) GetNodeName(_ error) []string {
	return []string{s.name}
}

func (s SimpleNode) Type() NodeType {
	return NodeTypeSingle
}

func (s SimpleNode) AllNodeName() []string {
	return s.GetNodeName(nil)
}

// 简化语法 && || 只能是单体任务不能是组合
type AndNode struct {
	tasks []SimpleNode
}

func NewAndTask(tasks ...SimpleNode) AndNode {
	return AndNode{tasks: tasks}
}

func (a AndNode) GetNodeName(_ error) []string {
	return slice.Map(a.tasks, func(_ int, src SimpleNode) string {
		return src.name
	})
}

func (a AndNode) Type() NodeType {
	return NodeTypeAnd
}

func (a AndNode) AllNodeName() []string {
	return a.GetNodeName(nil)
}

type OrNode struct {
	tasks []SimpleNode
}

func NewOrTask(tasks ...SimpleNode) OrNode {
	return OrNode{tasks: tasks}
}

func (o OrNode) GetNodeName(_ error) []string {
	return slice.Map(o.tasks, func(_ int, src SimpleNode) string {
		return src.name
	})
}

func (o OrNode) Type() NodeType {
	return NodeTypeOr
}

func (o OrNode) AllNodeName() []string {
	return o.GetNodeName(nil)
}

type EndNode struct {
	name string
}

func NewEndNode(name string) EndNode {
	return EndNode{
		name: name,
	}
}

func (e EndNode) GetNodeName(_ error) []string {
	return []string{
		e.name,
	}
}

func (e EndNode) Type() NodeType {
	return NodeTypeEnd
}

func (e EndNode) AllNodeName() []string {
	return e.GetNodeName(nil)
}

type ConditionNode struct {
	successTask SimpleNode
	failureTask SimpleNode
}

func NewConditionTask(successTask, failureTask SimpleNode) ConditionNode {
	return ConditionNode{successTask: successTask, failureTask: failureTask}
}

func (e ConditionNode) GetNodeName(err error) []string {
	if err != nil {
		return e.failureTask.GetNodeName(err)
	}
	return e.successTask.GetNodeName(nil)
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

type LoopNode struct {
	name string
}

func NewLoopTask(task string) LoopNode {
	return LoopNode{
		name: task,
	}
}

func (e LoopNode) GetNodeName(_ error) []string {
	return []string{e.name}
}

func (e LoopNode) Type() NodeType {
	return NodeTypeLoop
}

func (e LoopNode) AllNodeName() []string {
	return e.GetNodeName(nil)
}
