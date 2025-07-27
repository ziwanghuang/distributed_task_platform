package picker

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/gotomicro/ego/core/elog"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

// prometheusPicker 是所有基于Prometheus查询的选择器的通用基础。
// 它封装了执行PromQL查询、解析结果和从中随机选择一个节点的通用逻辑。
type prometheusPicker struct {
	promClient v1.API
	config     Config
	logger     *elog.Component
}

// newPrometheusPicker 创建一个Prometheus基础选择器。
// 注意：这是一个内部构造函数，由更具体的选择器（如Dispatcher）调用。
func newPrometheusPicker(promClient v1.API, config Config) *prometheusPicker {
	return &prometheusPicker{
		promClient: promClient,
		config:     config,
		logger:     elog.DefaultLogger.With(elog.FieldComponentName("picker.prometheus")),
	}
}

// pickByMetric 根据指定的指标名称选择最优执行节点。
func (p *prometheusPicker) pickByMetric(ctx context.Context, metricName string) (string, error) {
	queryCtx, cancel := context.WithTimeout(ctx, p.config.QueryTimeout)
	defer cancel()

	// 1. `up{job=%q} == 1` 确保只选择当前存活的节点。
	// 2. `... AND ON(instance)` 将存活节点与业务指标通过`instance`标签关联起来。
	// 3. `avg_over_time(...[%s])` 对存活节点的数据在指定时间窗口内取平均，以平滑抖动。
	// 4. `topk(%d, ...)` 从结果中选出最优的前N个候选者。
	query := fmt.Sprintf(
		`topk(%d, avg_over_time(%s[%s]) AND ON(instance) up{job=%q} == 1)`,
		p.config.TopNCandidates,
		metricName,
		p.config.TimeWindow.String(),
		p.config.JobName,
	)

	p.logger.Info("执行Prometheus指标查询",
		elog.String("query", query),
		elog.String("metricName", metricName),
	)

	result, _, err := p.promClient.Query(queryCtx, query, time.Now())
	if err != nil {
		p.logger.Error("查询Prometheus指标失败", elog.String("metricName", metricName), elog.FieldErr(err))
		return "", fmt.Errorf("查询 %s 指标失败: %w", metricName, err)
	}

	nodeIDs, err := p.parseQueryResult(result)
	if err != nil {
		p.logger.Error("解析Prometheus查询结果失败", elog.FieldErr(err))
		return "", err
	}

	if len(nodeIDs) == 0 {
		p.logger.Warn("未找到可用的执行节点", elog.String("metricName", metricName))
		return "", fmt.Errorf("未找到可用的执行节点")
	}

	// 使用全局rand，避免在短时间内因相同的种子导致伪随机失效。
	// #nosec G404 -- 这里只是用于负载均衡的随机选择，不需要加密级别的随机数
	selectedNodeID := nodeIDs[rand.Intn(len(nodeIDs))]

	p.logger.Info("节点选择结果",
		elog.String("selectedNodeID", selectedNodeID),
		elog.String("metricName", metricName),
		elog.Int("candidateCount", len(nodeIDs)),
		elog.Any("allCandidates", nodeIDs))

	return selectedNodeID, nil
}

// parseQueryResult 解析Prometheus查询结果并提取所有候选节点的ID。
func (p *prometheusPicker) parseQueryResult(result model.Value) ([]string, error) {
	vector, ok := result.(model.Vector)
	if !ok {
		return nil, fmt.Errorf("无效的查询结果类型, 需要 model.Vector, 得到 %T", result)
	}

	if len(vector) == 0 {
		return nil, nil
	}

	nodeIDs := make([]string, 0, len(vector))
	for _, sample := range vector {
		if nodeID, exists := sample.Metric["node_id"]; exists {
			nodeIDs = append(nodeIDs, string(nodeID))
		}
	}
	return nodeIDs, nil
}
