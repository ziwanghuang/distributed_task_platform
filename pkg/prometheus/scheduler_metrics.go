// Package prometheus 提供调度器的 Prometheus 监控指标。
//
// 本包定义了调度平台的核心可观测性指标，这些指标不仅用于监控告警，
// 还被 loadchecker 包的负载检查器实时查询，驱动智能调度决策。
//
// 指标列表：
//   - scheduler_loop_duration_seconds: 调度循环执行耗时（Histogram），
//     被 ClusterLoadChecker 查询用于对比节点性能
//   - load_checker_sleep_duration_seconds: 负载检查器建议的退避时间（Histogram），
//     用于监控负载检查器的触发频率和退避力度
package prometheus

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// SchedulerMetrics 调度器 Prometheus 指标收集器。
//
// 每个 scheduler 节点创建一个实例，通过 nodeID 标签区分不同节点的指标数据。
// 使用 promauto 自动注册到默认的 Prometheus 注册表。
//
// 字段说明：
//   - nodeID:        当前 scheduler 节点的唯一标识
//   - loopDuration:  调度循环耗时直方图，标签：node_id
//   - sleepDuration: 负载检查器退避时间直方图，标签：node_id, checker_type
type SchedulerMetrics struct {
	nodeID        string
	loopDuration  *prometheus.HistogramVec
	sleepDuration *prometheus.HistogramVec
}

// NewSchedulerMetrics 创建调度器指标收集器。
// nodeID 用于区分集群中不同 scheduler 节点的指标数据。
func NewSchedulerMetrics(nodeID string) *SchedulerMetrics {
	return &SchedulerMetrics{
		nodeID: nodeID,
		loopDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "scheduler_loop_duration_seconds",
				Help:    "调度循环执行耗时（秒）",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"node_id"},
		),
		sleepDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "load_checker_sleep_duration_seconds",
				Help:    "负载检查器建议的睡眠时间（秒）",
				Buckets: []float64{0, 0.1, 0.5, 1, 2, 5, 10, 30, 60},
			},
			[]string{"node_id", "checker_type"},
		),
	}
}

// StartRecordExecutionTime 开始记录一次调度循环的执行时间。
//
// 返回一个结束函数，调用方在循环结束时调用该函数：
//  1. 计算本次循环耗时并记录到 loopDuration 直方图
//  2. 返回耗时 Duration，供 loadchecker 使用
//
// 使用示例：
//
//	stop := metrics.StartRecordExecutionTime()
//	defer func() { duration := stop() }()
func (m *SchedulerMetrics) StartRecordExecutionTime() func() time.Duration {
	start := time.Now()
	return func() time.Duration {
		duration := time.Since(start)
		m.loopDuration.WithLabelValues(m.nodeID).Observe(duration.Seconds())
		return duration
	}
}

// RecordLoadCheckerSleep 记录负载检查器触发的退避时间。
//
// 参数：
//   - checkerType: 检查器类型标签（如 "cluster"、"database"）
//   - duration:    退避时间，仅 > 0 时记录（避免大量零值污染直方图）
func (m *SchedulerMetrics) RecordLoadCheckerSleep(checkerType string, duration time.Duration) {
	if duration > 0 {
		m.sleepDuration.WithLabelValues(m.nodeID, checkerType).Observe(duration.Seconds())
	}
}
