package loadchecker

import (
	"context"
	"time"

	"golang.org/x/time/rate"
)

// LimiterChecker 基于令牌桶的限流检查器
type LimiterChecker struct {
	limiter      *rate.Limiter
	waitDuration time.Duration // 当限流时的等待时间
}

// LimiterConfig 限流检查器配置
type LimiterConfig struct {
	RateLimit    float64       `yaml:"rateLimit"`    // 每秒允许的请求数
	BurstSize    int           `yaml:"burstSize"`    // 突发请求容量
	WaitDuration time.Duration `yaml:"waitDuration"` // 限流时的等待时间
}

// NewLimiterChecker 创建限流检查器
func NewLimiterChecker(config LimiterConfig) *LimiterChecker {
	return &LimiterChecker{
		limiter:      rate.NewLimiter(rate.Limit(config.RateLimit), config.BurstSize),
		waitDuration: config.WaitDuration,
	}
}

// Check 检查是否可以继续调度
func (c *LimiterChecker) Check(_ context.Context) (sleepDuration time.Duration, shouldSchedule bool) {
	// Reserve 方法会返回一个预定信息，它不会阻塞
	reservation := c.limiter.Reserve()

	// 如果为 true，表示我们无需等待，可以直接获取令牌
	if reservation.OK() {
		return 0, true
	}

	// 如果 OK() 为 false，说明令牌桶已空。
	// reservation.Delay() 会返回为了获取下一个令牌需要等待的确切时间。
	// 这样可以实现更精确的退避。
	sleepDuration = reservation.Delay()

	// 取消预留，因为我们不会立即使用这个令牌
	reservation.Cancel()

	// 也可以在这里加一个最小等待时间的保护，防止 Delay 返回一个非常小的值
	if c.waitDuration > 0 && sleepDuration < c.waitDuration {
		sleepDuration = c.waitDuration
	}

	return sleepDuration, false
}
