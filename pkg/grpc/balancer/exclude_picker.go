package balancer

import (
	"sync/atomic"

	"google.golang.org/grpc/balancer"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// excludePicker 实现排除式轮询的 Picker
type excludePicker struct {
	// subConns 是所有可用的子连接
	subConns []balancer.SubConn
	// nodeIDs 是与 subConns 对应的节点 ID
	nodeIDs []string
	// next 用于轮询的计数器
	next uint32
}

// newExcludePicker 创建新的 excludePicker
func newExcludePicker(subConns []balancer.SubConn, nodeIDs []string) *excludePicker {
	return &excludePicker{
		subConns: subConns,
		nodeIDs:  nodeIDs,
	}
}

// Pick 实现 balancer.Picker 接口，选择一个可用的连接
func (p *excludePicker) Pick(info balancer.PickInfo) (balancer.PickResult, error) {
	if len(p.subConns) == 0 {
		return balancer.PickResult{}, balancer.ErrNoSubConnAvailable
	}

	// 从 context 中获取要排除的节点 ID
	excludeNodeID, hasExclude := GetExcludeNode(info.Ctx)

	// 如果没有排除节点，或者只有一个连接，直接使用轮询
	if !hasExclude || len(p.subConns) == 1 {
		return p.pickRoundRobin(), nil
	}

	// 找出所有可用的 candidate 的索引
	candidateIndexes := make([]int, 0, len(p.subConns))
	for i, nodeID := range p.nodeIDs {
		if nodeID != excludeNodeID {
			candidateIndexes = append(candidateIndexes, i)
		}
	}

	// 如果过滤后没有可用连接
	if len(candidateIndexes) == 0 {
		// 如果所有连接都被排除了，作为最后的手段，可以忽略排除规则，任选一个
		// 或者返回错误，取决于业务需求
		if len(p.subConns) > 0 {
			return p.pickRoundRobin(), nil
		}
		return balancer.PickResult{}, status.Errorf(codes.Unavailable,
			"所有可用节点都被排除，排除节点: %s", excludeNodeID)
	}

	// 在可用的索引中进行轮询
	next := atomic.AddUint32(&p.next, 1)
	selectedIdxInCandidates := int(next-1) % len(candidateIndexes)
	selectedConn := p.subConns[candidateIndexes[selectedIdxInCandidates]]
	return balancer.PickResult{SubConn: selectedConn, Done: nil}, nil
}

// pickRoundRobin 执行标准的轮询选择
func (p *excludePicker) pickRoundRobin() balancer.PickResult {
	next := atomic.AddUint32(&p.next, 1)
	idx := int(next-1) % len(p.subConns)

	return balancer.PickResult{
		SubConn: p.subConns[idx],
		Done: func(_ balancer.DoneInfo) {
			// 可以在这里添加请求完成后的处理逻辑
		},
	}
}
