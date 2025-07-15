package balancer

import "context"

// ExcludeRoundRobinName 是排除式轮询负载均衡器的名称
const ExcludeRoundRobinName = "exclude_round_robin"

// contextKey 是用于在 context 中传递排除节点信息的 key 类型
type contextKey string

// ExcludedNodeIDContextKey 是在 context 中存储要排除的节点 ID 的 key
const ExcludedNodeIDContextKey contextKey = "excluded_node_id"

// WithExcludedNodeID 在 context 中设置要排除的节点 ID
func WithExcludedNodeID(ctx context.Context, nodeID string) context.Context {
	if nodeID == "" {
		return ctx
	}
	return context.WithValue(ctx, ExcludedNodeIDContextKey, nodeID)
}

// GetExcludeNode 从 context 中获取要排除的节点 ID
func GetExcludeNode(ctx context.Context) (string, bool) {
	nodeID, ok := ctx.Value(ExcludedNodeIDContextKey).(string)
	return nodeID, ok && nodeID != ""
}
