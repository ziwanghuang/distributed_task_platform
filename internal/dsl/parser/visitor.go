//nolint:errcheck //忽略
package parser

import (
	"errors"

	"gitee.com/flycash/distributed_task_platform/internal/dsl/ast/parser"
	"github.com/antlr4-go/antlr/v4"
	"github.com/ecodeclub/ekit/mapx"
	"github.com/ecodeclub/ekit/set"
	"github.com/ecodeclub/ekit/slice"
	"github.com/ecodeclub/ekit/syncx"
)

const (
	defaultTreeLen = 16
)

// TaskOrchestrationVisitor 用于遍历语法树
type TaskOrchestrationVisitor struct {
	parser.BaseTaskOrchestrationParserVisitor
}

func NewTaskOrchestrationVisitor() *TaskOrchestrationVisitor {
	return &TaskOrchestrationVisitor{}
}

type planRes struct {
	root  []PlanNode
	end   PlanNode
	tasks *syncx.Map[string, PlanNode]
	err   error
}

// mergePlan 合并多条任务链，生成全局的 syncx.Map[string, PlanNode]
// plans: 多组 PlanNode，每组表示一条链（如 A->B->C），多组之间可能有交叉点
// 目标：将所有 PlanNode 按名字聚合，正确设置每个 PlanNode 的 Pre/Next，
//
//	多前驱/多后继时用 OrNode/AndNode 包装，最终写入 v.plan.tasks
func (v *TaskOrchestrationVisitor) mergePlan(plans [][]*PlanNode) *planRes {
	// 1. 获取所有的任务名
	taskMap := v.getAllNode(plans)
	treeMap, err := v.setAstNode(plans)
	if err != nil {
		return &planRes{
			err: err,
		}
	}
	roots := make([]PlanNode, 0, len(taskMap))
	end := make([]PlanNode, 0, len(taskMap))

	for name, task := range taskMap {
		prenames, nextnames := v.getPreAndNext(plans, name)
		pre, err := v.getPreAndNextTask(treeMap, prenames)
		if err != nil {
			return &planRes{
				err: err,
			}
		}
		next, err := v.getPreAndNextTask(treeMap, nextnames)
		if err != nil {
			return &planRes{
				err: err,
			}
		}

		task.Pre = pre
		task.Next = next
		taskMap[name] = task
		if len(prenames) == 0 {
			roots = append(roots, task)
		}
		if len(nextnames) == 0 {
			end = append(end, task)
		}
	}
	err = v.setEndNode(taskMap, end)
	if err != nil {
		return &planRes{
			err: err,
		}
	}
	ans := &syncx.Map[string, PlanNode]{}
	for name, task := range taskMap {
		ans.Store(name, task)
	}
	return &planRes{
		root:  roots,
		end:   end[0],
		tasks: ans,
	}
}

func (v *TaskOrchestrationVisitor) getAllNode(plans [][]*PlanNode) map[string]PlanNode {
	taskMap := map[string]PlanNode{}
	for idx := range plans {
		plan := plans[idx]
		for jdx := range plan {
			task := plan[jdx]
			if task.Type() == NodeTypeLoop {
				if len(task.ChildNodes()) > 0 {
					taskMap[task.ChildNodes()[0]] = PlanNode{
						Node: task.Node,
					}
				}
				continue
			}
			for kdx := range task.ChildNodes() {
				taskName := task.ChildNodes()[kdx]
				taskMap[taskName] = PlanNode{
					Node: NewSimpleTask(taskName),
				}
			}
		}
	}
	return taskMap
}

func (v *TaskOrchestrationVisitor) setAstNode(plans [][]*PlanNode) (*mapx.TreeMap[[]string, Node], error) {
	treeMap, err := mapx.NewTreeMap[[]string, Node](CompareStrList)
	if err != nil {
		return nil, err
	}
	for _, plan := range plans {
		for _, task := range plan {
			err = treeMap.Put(task.ChildNodes(), task.Node)
			if err != nil {
				return nil, err
			}
		}
	}
	return treeMap, nil
}

func (v *TaskOrchestrationVisitor) setEndNode(taskMap map[string]PlanNode, endList []PlanNode) error {
	if len(endList) != number1 {
		return errors.New("有多个结束任务")
	} else {
		endPlanNode := endList[0]
		if len(endPlanNode.ChildNodes()) > 1 {
			return errors.New("结束任务必须是单一任务")
		}
		if endPlanNode.Node.Type() == NodeTypeLoop {
			return nil
		}
		endNodeName := endPlanNode.ChildNodes()[0]
		// 重新设置结束任务
		// 如果是简单任务就设置为 结束节点
		// 如果是循环任务就返回，循环任务也可以认为是一个结束节点
		endPlanNode.Node = NewEndNode(endNodeName)
		taskMap[endPlanNode.ChildNodes()[0]] = endPlanNode
		for name, task := range taskMap {
			if v.checkIsEndNode(task.Pre, endNodeName) {
				task.Pre = endPlanNode.Node
			}
			if v.checkIsEndNode(task.Next, endNodeName) {
				task.Next = endPlanNode.Node
			}
			taskMap[name] = task
		}
	}
	return nil
}

func (v *TaskOrchestrationVisitor) checkIsEndNode(no Node, endNodeName string) bool {
	if no != nil {
		return slice.Contains[string](no.ChildNodes(), endNodeName)
	}
	return false
}

//nolint:nilnil //这里就要他返回nil
func (v *TaskOrchestrationVisitor) getPreAndNextTask(tm *mapx.TreeMap[[]string, Node], names []string) (Node, error) {
	if len(names) == 1 {
		return NewSimpleTask(names[0]), nil
	}
	if len(names) > 1 {
		v, ok := tm.Get(names)
		if ok {
			return v, nil
		}
		return nil, errors.New("任务流表达式编写错误")
	}
	return nil, nil
}

func (v *TaskOrchestrationVisitor) getPreAndNext(plans [][]*PlanNode, taskName string) (pre, next []string) {
	preSet := set.NewMapSet[string](defaultTreeLen)
	nextSet := set.NewMapSet[string](defaultTreeLen)
	for _, plan := range plans {
		for _, task := range plan {
			if slice.Contains[string](task.Node.ChildNodes(), taskName) {
				v.setPreAndNext(preSet, task.Pre)
				v.setPreAndNext(nextSet, task.Next)
			}
		}
	}
	return preSet.Keys(), nextSet.Keys()
}

func (v *TaskOrchestrationVisitor) setPreAndNext(st *set.MapSet[string], task Node) {
	if NodeIsNil(task) {
		return
	}
	for idx := range task.ChildNodes() {
		st.Add(task.ChildNodes()[idx])
	}
}

func (v *TaskOrchestrationVisitor) Visit(ctx antlr.ParseTree) any {
	return v.VisitProgram(ctx.(*parser.ProgramContext))
}

func (v *TaskOrchestrationVisitor) VisitProgram(ctx *parser.ProgramContext) interface{} {
	exps := ctx.AllExpression()
	plans := make([][]*PlanNode, 0, len(exps))
	for idx := range exps {
		ans, ok := v.VisitExpression(exps[idx].(*parser.ExpressionContext)).([]*PlanNode)
		if ok {
			plans = append(plans, ans)
		}
	}

	return v.mergePlan(plans)
}

func (v *TaskOrchestrationVisitor) VisitTask(ctx *parser.TaskContext) interface{} {
	switch {
	case ctx.TASK_NAME() != nil:
		taskName := ctx.TASK_NAME().GetText()
		task := NewSimpleTask(taskName)
		return task
	case ctx.ParallelGroup() != nil:
		return v.VisitParallelGroup(ctx.ParallelGroup().(*parser.ParallelGroupContext))
	case ctx.JoinGroup() != nil:
		return v.VisitJoinGroup(ctx.JoinGroup().(*parser.JoinGroupContext))
	default:
		return nil
	}
}

func (v *TaskOrchestrationVisitor) VisitParallelGroup(ctx *parser.ParallelGroupContext) interface{} {
	// 并行组 [A,B,C] 表示 A->[B,C]，即 A 成功后并行执行 B 和 C
	tasks := ctx.AllTask()
	if len(tasks) > 0 {
		// 创建 AND 任务
		andTasks := make([]SimpleNode, 0, len(tasks))
		for _, taskCtx := range tasks {
			if taskCtx.TASK_NAME() != nil {
				taskName := taskCtx.TASK_NAME().GetText()
				task := NewSimpleTask(taskName)
				andTasks = append(andTasks, task)
			}
		}
		return NewAndTask(andTasks...)
	}
	return nil
}

func (v *TaskOrchestrationVisitor) VisitJoinGroup(ctx *parser.JoinGroupContext) interface{} {
	// 汇聚组 {A,B,C} 表示 {A,B,C}->D，即 A、B、C 都成功后执行 D
	tasks := ctx.AllTask()
	if len(tasks) > 0 {
		orTasks := make([]SimpleNode, 0, len(tasks))
		for _, taskCtx := range tasks {
			if taskCtx.TASK_NAME() != nil {
				taskName := taskCtx.TASK_NAME().GetText()
				task := NewSimpleTask(taskName)
				orTasks = append(orTasks, task)
			}
		}
		return NewOrTask(orTasks...)
	}
	return nil
}

func (v *TaskOrchestrationVisitor) VisitSequenceExpression(ctx *parser.SequenceExpressionContext) interface{} {
	expressions := ctx.AllConditionalExpression()
	var pre *PlanNode
	plan := make([]*PlanNode, 0, len(expressions))
	if len(expressions) > 0 {
		for idx := range expressions {
			expression := expressions[idx]
			ans := v.VisitConditionalExpression(expression.(*parser.ConditionalExpressionContext))
			switch ta := ans.(type) {
			case *conditionTask:
				prePlanTask := &PlanNode{
					Node: ta.Pre,
					Next: ta.Next,
				}
				if pre != nil {
					prePlanTask.Pre = pre.Node
				}
				conTask := &PlanNode{
					Node: ta.Next,
					Pre:  ta.Pre,
				}
				if pre != nil {
					pre.Next = prePlanTask
				}
				plan = append(plan, prePlanTask, conTask)
			case []*PlanNode:
				wa := ta[len(ta)-1]
				pre = wa
				plan = append(plan, ta...)

			case Node:
				t := &PlanNode{
					Node: ta,
				}

				plan = append(plan, t)
				if pre != nil {
					t.Pre = pre.Node
					pre.Next = t.Node
				}
				pre = t
			}

		}
	}

	return plan
}

// VisitConditionalExpression 现有的分支判断只支持 单个任务
//
//nolint:mnd //忽略
func (v *TaskOrchestrationVisitor) VisitConditionalExpression(ctx *parser.ConditionalExpressionContext) any {
	if ctx.QUESTION() != nil && len(ctx.AllExpression()) >= 2 {
		pre := ctx.GetChild(0)
		preTask, ok1 := v.VisitRepetitionExpression(pre.(*parser.RepetitionExpressionContext)).(SimpleNode)
		successRes := v.VisitExpression(ctx.GetChild(2).(*parser.ExpressionContext))
		successTask, ok2 := successRes.([]*PlanNode)
		failRes := v.VisitExpression(ctx.GetChild(4).(*parser.ExpressionContext))
		failTask, ok3 := failRes.([]*PlanNode)
		if ok1 && ok2 && ok3 {
			return &conditionTask{
				Pre:  preTask,
				Next: NewConditionTask(successTask[0].Node.(SimpleNode), failTask[0].Node.(SimpleNode)),
			}
		}
	}
	return v.VisitRepetitionExpression(ctx.GetChild(0).(*parser.RepetitionExpressionContext))
}

func (v *TaskOrchestrationVisitor) VisitRepetitionExpression(ctx *parser.RepetitionExpressionContext) interface{} {
	if ctx.PrimaryExpression() != nil {
		return v.VisitPrimaryExpression(ctx.PrimaryExpression().(*parser.PrimaryExpressionContext))
	}
	return nil
}

func (v *TaskOrchestrationVisitor) VisitPrimaryExpression(ctx *parser.PrimaryExpressionContext) interface{} {
	if ctx.Task() != nil {
		return v.VisitTask(ctx.Task().(*parser.TaskContext))
	} else if ctx.Expression() != nil {
		return v.VisitExpression(ctx.Expression().(*parser.ExpressionContext))
	}
	return nil
}

func (v *TaskOrchestrationVisitor) VisitExpression(ctx *parser.ExpressionContext) interface{} {
	if ctx.OrExpression() != nil {
		return v.VisitOrExpression(ctx.OrExpression().(*parser.OrExpressionContext))
	}
	return nil
}

func (v *TaskOrchestrationVisitor) VisitOrExpression(ctx *parser.OrExpressionContext) interface{} {
	if len(ctx.AllOR()) > 0 {
		expressions := ctx.AllAndExpression()
		orTask := make([]SimpleNode, 0, len(expressions))
		for idx := range expressions {
			ans := v.VisitAndExpression(expressions[idx].(*parser.AndExpressionContext))
			if ts, ok := ans.([]*PlanNode); ok {
				if len(ts) > 0 {
					orTask = append(orTask, ts[0].Node.(SimpleNode))
				}
			}
		}
		return NewOrTask(orTask...)
	}
	return v.VisitAndExpression(ctx.GetChild(0).(*parser.AndExpressionContext))
}

func (v *TaskOrchestrationVisitor) VisitAndExpression(ctx *parser.AndExpressionContext) interface{} {
	if len(ctx.AllAND()) > 0 {
		expressions := ctx.AllSequenceExpression()
		orTask := make([]SimpleNode, 0, len(expressions))
		for idx := range expressions {
			ans := v.VisitSequenceExpression(expressions[idx].(*parser.SequenceExpressionContext))
			if ts, ok := ans.([]*PlanNode); ok {
				if len(ts) > 0 {
					orTask = append(orTask, ts[0].Node.(SimpleNode))
				}
			}
		}
		return NewAndTask(orTask...)
	}
	return v.VisitSequenceExpression(ctx.GetChild(0).(*parser.SequenceExpressionContext))
}
