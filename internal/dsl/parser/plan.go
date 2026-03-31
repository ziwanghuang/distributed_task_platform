package parser

import (
	"errors"

	"gitee.com/flycash/distributed_task_platform/internal/dsl/ast/parser"
	"github.com/antlr4-go/antlr/v4"
	"github.com/ecodeclub/ekit/syncx"
)

const number1 = 1

// PlanNode 表示 DAG 图中一个任务节点及其邻接关系。
// 每个 PlanNode 包含：
//   - Node: 节点自身（SimpleNode/EndNode 等）
//   - Pre: 前驱节点（可以是 SimpleNode/AndNode/OrNode，表示需要等待的前驱任务）
//   - Next: 后继节点（可以是 SimpleNode/AndNode/OrNode/ConditionNode，表示完成后触发的后续任务）
type PlanNode struct {
	Pre  Node // 前驱节点（nil 表示无前驱，即根节点）
	Next Node // 后继节点（nil 表示无后继，即叶子节点）
	Node      // 嵌入 Node 接口，表示节点自身
}

// conditionTask 是条件分支的中间表示。
// 在 Visitor 遍历语法树时，条件表达式 "A ? B : C" 会先解析为 conditionTask，
// 然后在组装 PlanNode 链时转换为 PlanNode。
type conditionTask struct {
	Pre  SimpleNode    // 条件判定的前驱任务
	Next ConditionNode // 条件分支（包含 successTask 和 failureTask）
}

// AstPlan 是 TaskPlan 接口的 ANTLR4 实现。
// 它持有解析后的 ANTLR4 语法树，通过 Build 方法将语法树转换为 DAG 图结构。
//
// 内部数据结构：
//   - tasks: 任务名 → PlanNode 的线程安全映射
//   - root: DAG 图的入口节点列表（无前驱的节点）
//   - end: DAG 图的终止节点
type AstPlan struct {
	pro   parser.IProgramContext        // ANTLR4 解析后的语法树根节点
	end   PlanNode                      // DAG 终止节点
	tasks *syncx.Map[string, PlanNode]  // 任务名 → PlanNode 映射（线程安全）
	root  []PlanNode                    // DAG 入口节点列表
}

// RootNode 返回 DAG 图的所有根节点（无前驱依赖的入口节点）。
func (a *AstPlan) RootNode() []PlanNode {
	return a.root
}

// AdjoiningNode 根据任务名查找对应的 PlanNode（包含前驱后继信息）。
// 返回值 bool 表示是否找到该任务名对应的节点。
func (a *AstPlan) AdjoiningNode(name string) (PlanNode, bool) {
	if task, ok := a.tasks.Load(name); ok {
		return task, true
	}
	return PlanNode{}, false
}

// Build 将 ANTLR4 语法树转换为 DAG 图结构。
// 流程：
//  1. 创建 TaskOrchestrationVisitor 遍历语法树
//  2. Visitor 将每条表达式解析为 PlanNode 链
//  3. mergePlan 合并所有链，建立全局的前驱/后继关系
//  4. 将结果存入 AstPlan 的 tasks/root/end 字段
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

// NewAstPlan 解析 DSL 表达式并构建 DAG 图。
// 完整流程：
//  1. ANTLR4 词法分析（Lexer）：将 DSL 文本拆分为 token
//  2. ANTLR4 语法分析（Parser）：根据语法规则将 token 组织为语法树
//  3. Build：通过 Visitor 遍历语法树，构建 DAG 图
//
// DSL 语法由 internal/dsl/ast/ 下的 .g4 文件定义。
func NewAstPlan(query string) (*AstPlan, error) {
	// Step 1: 词法分析
	lexer := parser.NewTaskOrchestrationLexer(antlr.NewInputStream(query))
	// Step 2: 语法分析
	tokens := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	p := parser.NewTaskOrchestrationParser(tokens)
	pro := p.Program()
	// Step 3: 构建 DAG 图
	a := &AstPlan{
		pro: pro,
	}
	err := a.Build()
	if err != nil {
		return nil, err
	}
	return a, nil
}
