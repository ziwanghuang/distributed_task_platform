package loadchecker

import (
	"context"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

// DatabaseLoadChecker 基于数据库负载的检查器
type DatabaseLoadChecker struct {
	promClient      v1.API
	threshold       time.Duration // 数据库平均响应时间阈值
	timeWindow      time.Duration // 查询时间窗口
	backoffDuration time.Duration // 负载过高时的退避时间
}

// DatabaseLoadConfig 数据库负载检查器配置
type DatabaseLoadConfig struct {
	Threshold       time.Duration `yaml:"threshold"`       // 数据库响应时间阈值
	TimeWindow      time.Duration `yaml:"timeWindow"`      // 查询时间窗口
	BackoffDuration time.Duration `yaml:"backoffDuration"` // 负载过高时的退避时间
}

// NewDatabaseLoadChecker 创建数据库负载检查器
func NewDatabaseLoadChecker(promClient v1.API, config DatabaseLoadConfig) *DatabaseLoadChecker {
	return &DatabaseLoadChecker{
		promClient:      promClient,
		threshold:       config.Threshold,
		timeWindow:      config.TimeWindow,
		backoffDuration: config.BackoffDuration,
	}
}

// Check 检查数据库负载状态
func (c *DatabaseLoadChecker) Check(ctx context.Context) (sleepDuration time.Duration, shouldSchedule bool) {
	// 查询数据库平均响应时间
	avgTime, err := c.queryDBAvgTime(ctx)
	if err != nil {
		// 查询失败，假设可以继续调度（降级策略）
		return 0, true
	}

	// 检查是否超过阈值
	if avgTime > c.threshold {
		// 数据库负载过高，需要退避
		return c.backoffDuration, false
	}

	return 0, true
}

// queryDBAvgTime 查询数据库平均响应时间
func (c *DatabaseLoadChecker) queryDBAvgTime(ctx context.Context) (time.Duration, error) {
	// Prometheus查询语句：查询最近时间窗口内的数据库平均响应时间
	query := `avg(rate(gorm_query_duration_seconds_sum[` + c.timeWindow.String() + `]) / rate(gorm_query_duration_seconds_count[` + c.timeWindow.String() + `]))`

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
