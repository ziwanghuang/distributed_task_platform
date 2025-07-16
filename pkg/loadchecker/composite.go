package loadchecker

import (
	"context"
	"time"
)

// CompositeStrategy 组合策略
type CompositeStrategy int

const (
	// StrategyAND 所有检查器都通过才可调度
	StrategyAND CompositeStrategy = iota
	// StrategyOR 任一检查器通过就可调度
	StrategyOR
)

// CompositeChecker 组合检查器
type CompositeChecker struct {
	checkers []LoadChecker
	strategy CompositeStrategy
}

// NewCompositeChecker 创建组合检查器
func NewCompositeChecker(strategy CompositeStrategy, checkers ...LoadChecker) *CompositeChecker {
	return &CompositeChecker{
		checkers: checkers,
		strategy: strategy,
	}
}

// Check 执行组合检查
func (c *CompositeChecker) Check(ctx context.Context) (sleepDuration time.Duration, shouldSchedule bool) {
	switch c.strategy {
	case StrategyAND:
		return c.checkAND(ctx)
	case StrategyOR:
		return c.checkOR(ctx)
	default:
		return c.checkAND(ctx)
	}
}

// checkAND 所有检查器都必须通过
func (c *CompositeChecker) checkAND(ctx context.Context) (sleepDuration time.Duration, shouldSchedule bool) {
	var maxDuration time.Duration

	for _, checker := range c.checkers {
		duration, ok := checker.Check(ctx)
		if !ok {
			// 有任一检查器不通过，返回最大睡眠时间
			if duration > maxDuration {
				maxDuration = duration
			}
		}
	}

	// 如果maxDuration > 0，说明有检查器不通过
	return maxDuration, maxDuration == 0
}

// checkOR 任一检查器通过即可
func (c *CompositeChecker) checkOR(ctx context.Context) (sleepDuration time.Duration, shouldSchedule bool) {
	minDuration := time.Hour // 设置一个很大的初始值
	allFailed := true

	for _, checker := range c.checkers {
		duration, ok := checker.Check(ctx)
		if ok {
			// 有检查器通过，可以继续调度
			return 0, true
		} else {
			allFailed = false
			// 记录最小睡眠时间
			if duration < minDuration {
				minDuration = duration
			}
		}
	}

	// 所有检查器都不通过
	if allFailed && minDuration == time.Hour {
		minDuration = time.Minute // 默认睡眠时间
	}

	return minDuration, false
}
