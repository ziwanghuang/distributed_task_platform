package strategy

import (
	"sync/atomic"
	"time"
)

// 编译时断言：确保 FixedIntervalRetryStrategy 实现了 Strategy 接口
var _ Strategy = (*FixedIntervalRetryStrategy)(nil)

// FixedIntervalRetryStrategy 固定间隔重试策略。
//
// 每次重试都等待相同的 interval 时间，直到达到 maxRetries 上限。
// 适用于对重试时间不敏感的场景（如定时任务重调度）。
//
// 与指数退避的对比：
//   - 固定间隔：简单可预测，适合已知恢复时间的场景
//   - 指数退避：逐步拉长间隔，适合不确定恢复时间的场景（避免频繁重试加重负载）
//
// 字段说明：
//   - maxRetries: 最大重试次数，0 或负数表示无限重试
//   - interval:   固定的重试间隔时间
//   - retries:    当前重试次数（仅 Next() 方法使用，原子操作）
type FixedIntervalRetryStrategy struct {
	maxRetries int32         // 最大重试次数，如果是 0 或负数，表示无限重试
	interval   time.Duration // 重试间隔时间
	retries    int32         // 当前重试次数
}

// NewFixedIntervalRetryStrategy 创建固定间隔重试策略实例。
func NewFixedIntervalRetryStrategy(interval time.Duration, maxRetries int32) *FixedIntervalRetryStrategy {
	return &FixedIntervalRetryStrategy{
		maxRetries: maxRetries,
		interval:   interval,
	}
}

// NextWithRetries 根据外部传入的重试次数判断是否继续重试（无状态模式）。
// 返回固定的 interval 间隔，直到 retries > maxRetries 时返回 (0, false)。
func (s *FixedIntervalRetryStrategy) NextWithRetries(retries int32) (time.Duration, bool) {
	return s.nextWithRetries(retries)
}

// nextWithRetries 核心判断逻辑。
// maxRetries <= 0 表示无限重试，否则 retries <= maxRetries 时继续重试。
func (s *FixedIntervalRetryStrategy) nextWithRetries(retries int32) (time.Duration, bool) {
	if s.maxRetries <= 0 || retries <= s.maxRetries {
		return s.interval, true
	}
	return 0, false
}

// Next 内部自增重试计数器后判断是否继续重试（有状态模式）。
func (s *FixedIntervalRetryStrategy) Next() (time.Duration, bool) {
	retries := atomic.AddInt32(&s.retries, 1)
	return s.nextWithRetries(retries)
}

// Report 上报错误信息（当前为空操作）。
func (s *FixedIntervalRetryStrategy) Report(_ error) Strategy {
	return s
}
