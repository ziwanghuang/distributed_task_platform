package loadchecker

import (
	"context"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

// DatabaseLoadChecker 基于数据库响应时间的负载检查器。
//
// 核心思路：通过 Prometheus 查询 GORM 中间件上报的 gorm_query_duration_seconds 指标，
// 获取数据库的平均查询响应时间。如果响应时间超过阈值，说明 DB 负载过高，
// 此时应降低调度频率，减少对数据库的压力，避免雪崩效应。
//
// 与 ClusterLoadChecker 的区别：
//   - ClusterLoadChecker 关注计算资源（CPU/调度循环耗时）
//   - DatabaseLoadChecker 关注存储资源（数据库响应时间）
//   - 两者可以组合使用，任一检查器触发退避即降频
//
// 字段说明：
//   - promClient:      Prometheus API 客户端
//   - threshold:       数据库平均响应时间阈值，超过则触发退避
//   - timeWindow:      PromQL 查询的时间窗口
//   - backoffDuration: 触发退避时的固定退避时间（与 ClusterLoadChecker 的动态计算不同）
type DatabaseLoadChecker struct {
	promClient      v1.API
	threshold       time.Duration // 数据库平均响应时间阈值
	timeWindow      time.Duration // 查询时间窗口
	backoffDuration time.Duration // 负载过高时的退避时间
}

// DatabaseLoadConfig 数据库负载检查器的配置参数。
type DatabaseLoadConfig struct {
	Threshold       time.Duration `yaml:"threshold"`       // 数据库响应时间阈值，超过则触发退避
	TimeWindow      time.Duration `yaml:"timeWindow"`      // PromQL 查询时间窗口
	BackoffDuration time.Duration `yaml:"backoffDuration"` // 负载过高时的固定退避时间
}

// NewDatabaseLoadChecker 创建数据库负载检查器实例。
func NewDatabaseLoadChecker(promClient v1.API, config DatabaseLoadConfig) *DatabaseLoadChecker {
	return &DatabaseLoadChecker{
		promClient:      promClient,
		threshold:       config.Threshold,
		timeWindow:      config.TimeWindow,
		backoffDuration: config.BackoffDuration,
	}
}

// Check 检查数据库负载状态，决定是否允许继续调度。
//
// 判断逻辑简单直接：查询 DB 平均响应时间，超过阈值则返回固定退避时间。
// 降级策略：Prometheus 查询失败时，默认允许继续调度。
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

// queryDBAvgTime 通过 PromQL 查询数据库的平均响应时间。
//
// PromQL 语义：
//
//	avg(rate(gorm_query_duration_seconds_sum[window]) / rate(gorm_query_duration_seconds_count[window]))
//
// 即：先计算每个实例的平均单次查询耗时，再对所有实例取平均。
// gorm_query_duration_seconds 指标由 GORM 的 Prometheus 插件自动上报。
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
