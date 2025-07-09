package parser

type AstNode interface {
	GetNodeName(err error) []string
	Type() NodeType
	AllNodeName() []string
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
	AdjoiningNode(name string) PlanNode
	RootNode() []PlanNode
}
