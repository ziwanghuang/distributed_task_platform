package parser

import (
	"errors"

	"gitee.com/flycash/distributed_task_platform/internal/dsl/ast/parser"
	"github.com/antlr4-go/antlr/v4"
	"github.com/ecodeclub/ekit/syncx"
)

const number1 = 1

type PlanNode struct {
	Pre  AstNode
	Next AstNode
	AstNode
}

type conditionTask struct {
	Pre  SimpleNode
	Next ConditionNode
}

type AstPlan struct {
	pro   parser.IProgramContext
	end   PlanNode
	tasks *syncx.Map[string, PlanNode]
	root  []PlanNode
}

func (a *AstPlan) RootNode() []PlanNode {
	return a.root
}

func (a *AstPlan) AdjoiningNode(name string) PlanNode {
	if task, ok := a.tasks.Load(name); ok {
		return task
	}
	return PlanNode{}
}

func (a *AstPlan) Build() error {
	res := NewTaskOrchestrationVisitor().Visit(a.pro)
	v, ok := res.(*planRes)
	if !ok {
		return errors.New("解析失败")
	}
	if v.err != nil {
		return v.err
	}
	a.root = v.root
	a.end = v.end
	a.tasks = v.tasks
	return nil
}

func NewAstPlan(query string) *AstPlan {
	lexer := parser.NewTaskOrchestrationLexer(antlr.NewInputStream(query))
	tokens := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	p := parser.NewTaskOrchestrationParser(tokens)
	pro := p.Program()

	return &AstPlan{
		pro: pro,
	}
}
