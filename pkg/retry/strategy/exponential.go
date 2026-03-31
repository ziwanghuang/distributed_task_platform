package strategy

import (
	"math"
	"sync/atomic"
	"time"
)

// 编译时断言：确保 ExponentialBackoffRetryStrategy 实现了 Strategy 接口
var _ Strategy = (*ExponentialBackoffRetryStrategy)(nil)

// ExponentialBackoffRetryStrategy 指数退避重试策略。
//
// 退避公式：interval = initialInterval × 2^(retries-1)
//
// 例如 initialInterval=1s, maxInterval=30s, maxRetries=5：
//   - 第1次重试: 1s
//   - 第2次重试: 2s
//   - 第3次重试: 4s
//   - 第4次重试: 8s
//   - 第5次重试: 16s
//   - 超过5次: 不再重试
//
// 当计算结果超过 maxInterval 或发生溢出时，使用 maxInterval 作为上限，
// 并通过 maxIntervalReached 标记避免后续重复计算。
//
// 字段说明：
//   - initialInterval:    初始重试间隔
//   - maxInterval:        最大重试间隔上限
//   - maxRetries:         最大重试次数（0 或负数表示无限重试）
//   - retries:            当前重试次数（仅 Next() 方法使用，原子操作）
//   - maxIntervalReached: 是否已达到最大间隔（优化标记，避免重复计算 Pow）
type ExponentialBackoffRetryStrategy struct {
	// 初始重试间隔
	initialInterval time.Duration
	// 最大重试间隔
	maxInterval time.Duration
	// 最大重试次数
	maxRetries int32
	// 当前重试次数
	retries int32
	// 是否已经达到最大重试间隔
	maxIntervalReached atomic.Value
}

// NewExponentialBackoffRetryStrategy 创建指数退避重试策略实例。
func NewExponentialBackoffRetryStrategy(initialInterval, maxInterval time.Duration, maxRetries int32) *ExponentialBackoffRetryStrategy {
	return &ExponentialBackoffRetryStrategy{
		initialInterval: initialInterval,
		maxInterval:     maxInterval,
		maxRetries:      maxRetries,
	}
}

// Report 上报错误信息（当前为空操作，预留给未来的自适应重试策略）。
func (s *ExponentialBackoffRetryStrategy) Report(_ error) Strategy {
	return s
}

// NextWithRetries 根据外部传入的重试次数计算下一次重试间隔（无状态模式）。
// 补偿器从 DB 读取任务的 retryCount 后调用此方法。
func (s *ExponentialBackoffRetryStrategy) NextWithRetries(retries int32) (time.Duration, bool) {
	return s.nextWithRetries(retries)
}

// nextWithRetries 是核心计算逻辑。
//
// 判断流程：
//  1. maxRetries <= 0 表示无限重试，直接计算间隔
//  2. retries <= maxRetries 表示未达上限，计算间隔
//  3. 否则返回 (0, false) 表示不再重试
//
// 间隔计算：interval = initialInterval × 2^(retries-1)
// 如果结果溢出（<= 0）或超过 maxInterval，使用 maxInterval 并标记。
func (s *ExponentialBackoffRetryStrategy) nextWithRetries(retries int32) (time.Duration, bool) {
	if s.maxRetries <= 0 || retries <= s.maxRetries {
		if reached, ok := s.maxIntervalReached.Load().(bool); ok && reached {
			return s.maxInterval, true
		}
		const two = 2
		interval := s.initialInterval * time.Duration(math.Pow(two, float64(retries-1)))
		// 溢出或当前重试间隔大于最大重试间隔
		if interval <= 0 || interval > s.maxInterval {
			s.maxIntervalReached.Store(true)
			return s.maxInterval, true
		}
		return interval, true
	}
	return 0, false
}

// Next 内部自增重试计数器后计算间隔（有状态模式）。
// 使用原子操作保证并发安全。
func (s *ExponentialBackoffRetryStrategy) Next() (time.Duration, bool) {
	retries := atomic.AddInt32(&s.retries, 1)
	return s.nextWithRetries(retries)
}
