// Package parser 实现了 DAG 工作流的 DSL（Domain Specific Language）解析器。
//
// 本包将 DAG 工作流的文本表达式（ExecExpr）解析为可执行的图结构（TaskPlan）。
//
// DSL 语法示例：
//   - "A -> B -> C"                  简单串行：A 完成后执行 B，B 完成后执行 C
//   - "A -> (B && C)"                并行分支：A 完成后同时执行 B 和 C
//   - "A -> (B || C)"                OR 分支：A 完成后 B 和 C 都启动，任一成功即可
//   - "A -> (B ? C : D)"            条件分支：A 成功执行 C，A 失败执行 D
//
// 解析流程：
//  1. 使用 ANTLR4 生成的词法/语法分析器解析 ExecExpr 文本
//  2. Visitor 遍历语法树（AST），将每个节点转换为 Node 接口的实现
//  3. 组装 PlanNode 列表，建立前驱/后继的邻接关系
//  4. 返回 TaskPlan 接口，供上层 PlanService 使用
package parser

import "reflect"

// Node 是 DAG 图中节点的统一接口。
// 每种节点类型（Simple/And/Or/Condition/End）实现不同的后继计算逻辑。
type Node interface {
	// NextNodes 根据前驱任务的执行结果（execution），计算出下一步需要执行的任务名列表。
	// 对于 SimpleNode/AndNode/OrNode：前驱成功则返回后继任务名，前驱失败则返回空列表。
	// 对于 ConditionNode：前驱成功返回 successTask，前驱失败返回 failureTask。
	NextNodes(execution Execution) []string
	// Type 返回节点类型标识。
	Type() NodeType
	// ChildNodes 返回该节点包含的所有后继任务名（不考虑条件判断）。
	// 用于构建图结构时的邻接关系。
	ChildNodes() []string
}

// NodeIsNil 检查 Node 接口值是否为 nil。
// 由于 Go 的接口 nil 判断问题（接口值非 nil 但底层指针为 nil），
// 需要通过反射进行深层检查。
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

// Execution 表示前驱任务的执行结果，用于 Node.NextNodes 的条件判定。
// 独立定义此类型是为了避免与 domain 包的循环引用。
type Execution struct {
	Status TaskExecutionStatus // 前驱任务的执行状态
}

// TaskExecutionStatus 是 DSL parser 包内部定义的执行状态类型。
// 与 domain.TaskExecutionStatus 值相同但类型独立，避免循环依赖。
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

// IsSuccess 判断执行状态是否为成功。
func (e TaskExecutionStatus) IsSuccess() bool {
	return e == TaskExecutionStatusSuccess
}

// NodeType 定义了 DAG 图中节点的类型标识。
type NodeType string

const (
	NodeTypeSingle    NodeType = "single"    // 单任务节点
	NodeTypeAnd       NodeType = "and"       // AND 并行节点（前驱成功后同时启动所有子任务）
	NodeTypeOr        NodeType = "or"        // OR 并行节点（前驱成功后启动所有子任务，任一成功即可）
	NodeTypeEnd       NodeType = "end"       // 结束节点（DAG 图的终止标记）
	NodeTypeCondition NodeType = "condition" // 条件分支节点（根据前驱成功/失败走不同分支）
	NodeTypeLoop      NodeType = "loop"      // 循环节点（预留，尚未实现）
)

// TaskPlan 是解析后的 DAG 图结构接口。
// 提供按任务名查询邻接节点和获取根节点的能力。
type TaskPlan interface {
	// AdjoiningNode 根据任务名查找其在 DAG 图中的邻接节点信息（前驱、后继关系）。
	AdjoiningNode(name string) (PlanNode, bool)
	// RootNode 返回 DAG 图的根节点列表（没有前驱依赖的入口节点）。
	RootNode() []PlanNode
}
