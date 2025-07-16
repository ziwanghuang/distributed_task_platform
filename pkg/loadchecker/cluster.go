package loadchecker

import (
	"context"
	"fmt"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

// ClusterLoadChecker 集群负载检查器
type ClusterLoadChecker struct {
	nodeID             string
	promClient         v1.API
	thresholdRatio     float64       // 性能阈值比例，1.2表示超过平均值20%
	timeWindow         time.Duration // 查询时间窗口
	slowdownMultiplier float64       // 降频乘数
	minBackoffDuration time.Duration // 最小退避时间
}

// ClusterLoadConfig 集群负载检查器配置
type ClusterLoadConfig struct {
	ThresholdRatio     float64       `yaml:"thresholdRatio"`     // 1.2表示超过平均值20%
	TimeWindow         time.Duration `yaml:"timeWindow"`         // 查询时间窗口
	SlowdownMultiplier float64       `yaml:"slowdownMultiplier"` // 降频乘数，2.0表示睡眠时间加倍
	MinBackoffDuration time.Duration `yaml:"minBackoffDuration"` // 最小退避时间
}

// NewClusterLoadChecker 创建集群负载检查器
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

// Check 检查集群负载状态
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

// queryClusterAverageTime 查询集群平均执行时间
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
