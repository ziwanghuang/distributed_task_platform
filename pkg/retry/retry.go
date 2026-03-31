// Package retry 提供重试策略的工厂函数和配置定义。
//
// 本包是调度平台重试机制的入口层，根据配置创建具体的重试策略实例。
// 具体策略实现位于 strategy 子包。
//
// 支持的重试策略：
//   - "fixed":       固定间隔重试（FixedIntervalRetryStrategy）
//   - "exponential": 指数退避重试（ExponentialBackoffRetryStrategy）
//
// 使用场景：
//   - 任务执行失败后的重试间隔计算
//   - RetryCompensator 根据配置创建策略后，调用 NextWithRetries 计算下次重试时间
package retry

import (
	"fmt"
	"time"

	"gitee.com/flycash/distributed_task_platform/pkg/retry/strategy"
)

// NewRetry 根据配置创建对应的重试策略实例。
//
// 参数 cfg.Type 决定使用哪种策略：
//   - "fixed":       使用 cfg.FixedInterval 配置创建固定间隔策略
//   - "exponential": 使用 cfg.ExponentialBackoff 配置创建指数退避策略
//
// 返回错误：配置类型未知时返回错误。
func NewRetry(cfg Config) (strategy.Strategy, error) {
	// 根据 config 中的字段来检测
	switch cfg.Type {
	case "fixed":
		return strategy.NewFixedIntervalRetryStrategy(cfg.FixedInterval.Interval, cfg.FixedInterval.MaxRetries), nil
	case "exponential":
		return strategy.NewExponentialBackoffRetryStrategy(cfg.ExponentialBackoff.InitialInterval, cfg.ExponentialBackoff.MaxInterval, cfg.ExponentialBackoff.MaxRetries), nil
	default:
		return nil, fmt.Errorf("未知重试类型: %s", cfg.Type)
	}
}

// Config 是重试策略的配置。
//
// 字段说明：
//   - Type:               策略类型，"fixed" 或 "exponential"
//   - FixedInterval:      固定间隔策略的配置（Type="fixed" 时使用）
//   - ExponentialBackoff: 指数退避策略的配置（Type="exponential" 时使用）
type Config struct {
	Type               string                    `json:"type"`
	FixedInterval      *FixedIntervalConfig      `json:"fixedInterval"`
	ExponentialBackoff *ExponentialBackoffConfig `json:"exponentialBackoff"`
}

// ExponentialBackoffConfig 指数退避重试策略的配置。
//
// 退避公式：interval = InitialInterval × 2^(retries-1)
// 当计算结果超过 MaxInterval 时，使用 MaxInterval 作为上限。
type ExponentialBackoffConfig struct {
	// InitialInterval 初始重试间隔
	InitialInterval time.Duration `json:"initialInterval"`
	// MaxInterval 最大重试间隔上限
	MaxInterval time.Duration `json:"maxInterval"`
	// MaxRetries 最大重试次数（0 或负数表示无限重试）
	MaxRetries int32 `json:"maxRetries"`
}

// FixedIntervalConfig 固定间隔重试策略的配置。
type FixedIntervalConfig struct {
	// MaxRetries 最大重试次数（0 或负数表示无限重试）
	MaxRetries int32 `json:"maxRetries"`
	// Interval 固定的重试间隔
	Interval time.Duration `json:"interval"`
}
