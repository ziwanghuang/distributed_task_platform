package domain

import (
	"gitee.com/flycash/distributed_task_platform/internal/dsl/parser"
	"gitee.com/flycash/distributed_task_platform/internal/errs"
)

type PlanTask struct {
	// 前置的所有节点, 这里用指针就是为了简化代码。
	PreTask       []*PlanTask
	TaskExecution TaskExecution
	// 对应ast解析的内容 PreTask + AstPlanNode 就可以校验出前置节点有没有完成
	AstPlanNode parser.PlanNode
	// 这步在 GetPlan的时候就会进行赋值，且会根据当前的执行情况进行判断，
	// 如果是当前任务没执行到 这里的NextTask，是不会有值的。
	NextTask []*PlanTask
	Task
}

// tasks是这个plan所有task
func (p *PlanTask) SetPre(tasks map[string]*PlanTask) error {
	planNode := p.AstPlanNode
	if planNode.Pre != nil {
		nodes := planNode.Pre.AllNodeName()
		for jdx := range nodes {
			if v, ok := tasks[nodes[jdx]]; ok {
				p.PreTask = append(p.PreTask, v)
			} else {
				return errs.ErrInitPlanFailed
			}
		}
	}
	return nil
}

func (p *PlanTask) SetNext(tasks map[string]*PlanTask) error {
	planNode := p.AstPlanNode
	if planNode.Next != nil {
		nodes := planNode.Next.NodeName(newAstExecution(p.TaskExecution))
		for jdx := range nodes {
			if v, ok := tasks[nodes[jdx]]; ok {
				p.NextTask = append(p.NextTask, v)
			} else {
				return errs.ErrInitPlanFailed
			}
		}
	}
	return nil
}

func (p *PlanTask) NextStep() []*PlanTask {
	return p.NextTask
}

func (p *PlanTask) CheckPre() bool {
	switch p.AstPlanNode.Pre.Type() {
	case parser.NodeTypeSingle, parser.NodeTypeAnd:
		return p.preAllSuccess()
	case parser.NodeTypeCondition, parser.NodeTypeOr:
		return p.preOneSuccess()
	default:
		// 其他未知类型，直接返回false
		return false
	}
}

func (p *PlanTask) preAllSuccess() bool {
	for idx := range p.PreTask {
		preTask := p.PreTask[idx]
		if !preTask.TaskExecution.Status.IsSuccess() {
			return false
		}
	}
	return true
}

func (p *PlanTask) preOneSuccess() bool {
	for idx := range p.PreTask {
		preTask := p.PreTask[idx]
		if preTask.TaskExecution.Status.IsSuccess() {
			return true
		}
	}
	return false
}

func newAstExecution(execution TaskExecution) parser.Execution {
	return parser.Execution{
		Status: parser.TaskExecutionStatus(execution.Status.String()),
	}
}
