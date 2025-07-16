package prometheus

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// SchedulerMetrics 调度器指标收集器
type SchedulerMetrics struct {
	nodeID        string
	loopDuration  *prometheus.HistogramVec
	sleepDuration *prometheus.HistogramVec
}

// NewSchedulerMetrics 创建调度器指标收集器
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

// StartRecordExecutionTime 记录调度循环开始，返回结束记录函数
func (m *SchedulerMetrics) StartRecordExecutionTime() func() time.Duration {
	start := time.Now()
	return func() time.Duration {
		duration := time.Since(start)
		m.loopDuration.WithLabelValues(m.nodeID).Observe(duration.Seconds())
		return duration
	}
}

// RecordLoadCheckerSleep 记录负载检查器睡眠时间
func (m *SchedulerMetrics) RecordLoadCheckerSleep(checkerType string, duration time.Duration) {
	if duration > 0 {
		m.sleepDuration.WithLabelValues(m.nodeID, checkerType).Observe(duration.Seconds())
	}
}
