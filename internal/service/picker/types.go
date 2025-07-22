// Package picker 提供了基于不同策略选择执行节点的功能。
package picker

import (
	"context"
	"time"
)

// ExecutorNodePicker 是执行节点选择器的通用接口。
// 任何实现该接口的类型都可以根据特定逻辑选择一个最优的执行节点。
type ExecutorNodePicker interface {
	// Name 返回选择器的可读名称，主要用于日志和监控。
	Name() string
	// Pick 根据内部策略选择一个最优的执行节点。
	// 如果没有可用的节点或发生错误，将返回错误。
	Pick(ctx context.Context) (nodeID string, err error)
}

// Config 定义了智能调度所需的全部配置。
type Config struct {
	Strategy       string        `yaml:"strategy"`       // 调度策略，决定使用哪种算法，如：cpu_priority, memory_priority
	JobName        string        `yaml:"jobName"`        // Prometheus中执行节点关联的job名称，用于过滤和关联指标
	TopNCandidates int           `yaml:"topNCandidates"` // 从资源最优的前N个候选节点中随机选择一个，以实现负载均衡
	TimeWindow     time.Duration `yaml:"timeWindow"`     // 查询指标时使用的时间窗口，例如 '1m'，用于平滑瞬时抖动
	QueryTimeout   time.Duration `yaml:"queryTimeout"`   // 执行Prometheus查询的超时时间
}

// 调度策略常量定义
const (
	StrategyCPUPriority    = "cpu_priority"
	StrategyMemoryPriority = "memory_priority"
)

// 指标名称常量定义
const (
	MetricCPUIdlePercent       = "executor_cpu_idle_percent"
	MetricMemoryAvailableBytes = "executor_memory_available_bytes"
)

// 默认配置值
const (
	DefaultTopNCandidates = 3
	DefaultTimeWindow     = 1 * time.Minute // 使用1分钟作为默认窗口，更好地平滑数据
	DefaultQueryTimeout   = 5 * time.Second
	DefaultJobName        = "executors"
)

// validateAndSetDefaults 验证并填充配置的默认值。
// 这是一个内部函数，确保所有从外部传入的配置都是有效的。
func (c *Config) validateAndSetDefaults() {
	if c.TopNCandidates <= 0 {
		c.TopNCandidates = DefaultTopNCandidates
	}
	if c.TimeWindow <= 0 {
		c.TimeWindow = DefaultTimeWindow
	}
	if c.QueryTimeout <= 0 {
		c.QueryTimeout = DefaultQueryTimeout
	}
	if c.JobName == "" {
		c.JobName = DefaultJobName
	}
}
