// Package loadchecker 提供基于 Prometheus 指标的智能负载检查器。
//
// 本包实现了"智能调度"的核心能力：通过实时查询 Prometheus 指标，
// 判断当前节点和数据库的负载状态，动态调整调度频率。
//
// 包含两种负载检查器：
//   - ClusterLoadChecker: 集群维度的负载检查，对比当前节点与集群平均水平
//   - DatabaseLoadChecker: 数据库维度的负载检查，监控 DB 响应时间
//
// 负载检查器实现了统一的 Check 接口语义：
//
//	Check(ctx) → (sleepDuration, shouldSchedule)
//	  - shouldSchedule=true:  允许继续调度
//	  - shouldSchedule=false: 需要退避 sleepDuration 后再调度
//
// 降级策略：当 Prometheus 查询失败时，默认允许继续调度（宁可过载也不停摆）。
package loadchecker

import (
	"context"
	"fmt"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

// ClusterLoadChecker 基于集群维度的负载检查器。
//
// 核心思路：将当前节点的调度循环耗时与集群所有节点的平均耗时进行比较。
// 如果当前节点的耗时超过集群平均值的 thresholdRatio 倍（如 1.2 倍），
// 说明该节点性能低于集群平均水平，需要降低调度频率，避免拖累整体调度效率。
//
// 字段说明：
//   - nodeID:             当前节点标识，用于日志和指标区分
//   - promClient:         Prometheus API 客户端，用于执行 PromQL 查询
//   - thresholdRatio:     性能阈值比例，如 1.2 表示超过平均值 20% 则触发降频
//   - timeWindow:         PromQL 查询的时间窗口（如 5m），决定取多长时间的平均值
//   - slowdownMultiplier: 降频乘数，如 2.0 表示退避时间 = 当前耗时 × 2
//   - minBackoffDuration: 最小退避时间下限，防止退避时间过短无效
type ClusterLoadChecker struct {
	nodeID             string
	promClient         v1.API
	thresholdRatio     float64       // 性能阈值比例，1.2表示超过平均值20%
	timeWindow         time.Duration // 查询时间窗口
	slowdownMultiplier float64       // 降频乘数
	minBackoffDuration time.Duration // 最小退避时间
}

// ClusterLoadConfig 集群负载检查器的配置参数。
type ClusterLoadConfig struct {
	ThresholdRatio     float64       `yaml:"thresholdRatio"`     // 1.2表示超过平均值20%
	TimeWindow         time.Duration `yaml:"timeWindow"`         // 查询时间窗口
	SlowdownMultiplier float64       `yaml:"slowdownMultiplier"` // 降频乘数，2.0表示睡眠时间加倍
	MinBackoffDuration time.Duration `yaml:"minBackoffDuration"` // 最小退避时间
}

// NewClusterLoadChecker 创建集群负载检查器实例。
func NewClusterLoadChecker(nodeID string, promClient v1.API, config ClusterLoadConfig) *ClusterLoadChecker {
	return &ClusterLoadChecker{
		nodeID:             nodeID,
		promClient:         promClient,
		thresholdRatio:     config.ThresholdRatio,
		timeWindow:         config.TimeWindow,
		slowdownMultiplier: config.SlowdownMultiplier,
		minBackoffDuration: config.MinBackoffDuration,
	}
}

// Check 检查集群负载状态，决定是否允许继续调度。
//
// 判断流程：
//  1. 从 context 获取当前调度循环的执行耗时（由上层 Runner 写入）
//  2. 通过 PromQL 查询集群所有节点的平均调度循环耗时
//  3. 计算 ratio = 当前耗时 / 集群平均耗时
//  4. 如果 ratio > thresholdRatio，说明当前节点偏慢，返回退避时间
//
// 降级策略：执行时间获取失败或 Prometheus 查询失败时，均返回允许继续调度。
func (c *ClusterLoadChecker) Check(ctx context.Context) (sleepDuration time.Duration, shouldSchedule bool) {
	// 获取本次执行时间
	currentDuration := GetExecutionTime(ctx)
	if currentDuration == 0 {
		// 没有执行时间数据，允许继续调度
		return 0, true
	}

	// 查询集群平均执行时间
	avgDuration, err := c.queryClusterAverageTime(ctx)
	if err != nil {
		// 查询失败，降级策略：允许继续调度
		return 0, true
	}

	// 如果集群平均时间为0，允许继续调度
	if avgDuration == 0 {
		return 0, true
	}

	// 计算性能比例
	ratio := float64(currentDuration) / float64(avgDuration)

	// 检查是否超过阈值
	if ratio > c.thresholdRatio {
		// 性能低于集群平均水平，需要降频
		backoffDuration := time.Duration(float64(currentDuration) * c.slowdownMultiplier)
		if backoffDuration < c.minBackoffDuration {
			backoffDuration = c.minBackoffDuration
		}
		return backoffDuration, false
	}

	return 0, true
}

// queryClusterAverageTime 通过 PromQL 查询集群所有节点在指定时间窗口内的平均调度循环耗时。
//
// PromQL 语义：
//
//	avg(rate(scheduler_loop_duration_seconds_sum[window]) / rate(scheduler_loop_duration_seconds_count[window]))
//
// 即：先计算每个节点的平均单次循环耗时（sum 的增速 / count 的增速），再对所有节点取平均。
func (c *ClusterLoadChecker) queryClusterAverageTime(ctx context.Context) (time.Duration, error) {
	// Prometheus查询语句：查询所有节点在指定时间窗口内的平均调度循环时间
	query := fmt.Sprintf(`avg(rate(scheduler_loop_duration_seconds_sum[%s]) / rate(scheduler_loop_duration_seconds_count[%s]))`,
		c.timeWindow.String(), c.timeWindow.String())

	result, _, err := c.promClient.Query(ctx, query, time.Now())
	if err != nil {
		return 0, err
	}

	// 解析结果
	if vector, ok := result.(model.Vector); ok && len(vector) > 0 {
		avgSeconds := float64(vector[0].Value)
		return time.Duration(avgSeconds * float64(time.Second)), nil
	}

	// 没有数据，返回0
	return 0, nil
}
