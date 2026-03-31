package loadchecker

import (
	"context"
	"time"

	"golang.org/x/time/rate"
)

// LimiterChecker 基于令牌桶算法的限流检查器。
// 用于控制调度器的调度频率，防止在高负载场景下过度调度。
//
// 工作原理：
//   - 内部维护一个 rate.Limiter 令牌桶，以 RateLimit（每秒令牌数）的速率补充令牌
//   - 令牌桶容量为 BurstSize，允许短时间内的突发调度
//   - 当令牌耗尽时，返回 shouldSchedule=false 和建议的等待时间
type LimiterChecker struct {
	limiter      *rate.Limiter
	waitDuration time.Duration // 当限流时的最小等待时间（兜底值）
}

// LimiterConfig 限流检查器配置
type LimiterConfig struct {
	RateLimit    float64       `yaml:"rateLimit"`    // 每秒允许的请求数（令牌补充速率）
	BurstSize    int           `yaml:"burstSize"`    // 突发请求容量（令牌桶最大容量）
	WaitDuration time.Duration `yaml:"waitDuration"` // 限流时的最小等待时间
}

// NewLimiterChecker 创建限流检查器
func NewLimiterChecker(config LimiterConfig) *LimiterChecker {
	return &LimiterChecker{
		limiter:      rate.NewLimiter(rate.Limit(config.RateLimit), config.BurstSize),
		waitDuration: config.WaitDuration,
	}
}

// Check 检查当前是否允许继续调度。
// 返回值：
//   - sleepDuration: 如果不允许调度，建议的等待时间
//   - shouldSchedule: true 表示令牌桶中有可用令牌，允许继续调度
//
// 实现说明：
//
//	使用 Allow() 而非 Reserve()，因为 Reserve().OK() 在 rate != Inf 时永远返回 true，
//	只是告诉你"预定成功了，但可能需要等待"。而 Allow() 是真正的"当前是否有令牌"检查。
func (c *LimiterChecker) Check(_ context.Context) (sleepDuration time.Duration, shouldSchedule bool) {
	// Allow() 等价于 AllowN(time.Now(), 1)，
	// 只有在令牌桶中确实有可用令牌时才返回 true 并消耗一个令牌
	if c.limiter.Allow() {
		return 0, true
	}

	// 令牌桶已空，需要等待。
	// 使用 Reserve 获取精确的等待时间（下一个令牌到达的时间）
	reservation := c.limiter.Reserve()
	sleepDuration = reservation.Delay()
	// 取消预留，因为我们不会在此刻等待消费这个令牌，
	// 而是由调用方决定如何处理等待（通常是 sleep 后重新进入调度循环）
	reservation.Cancel()

	// 兜底保护：确保等待时间不会过短，防止忙等
	if c.waitDuration > 0 && sleepDuration < c.waitDuration {
		sleepDuration = c.waitDuration
	}

	return sleepDuration, false
}
