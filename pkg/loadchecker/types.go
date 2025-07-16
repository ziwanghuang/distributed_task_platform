package loadchecker

import (
	"context"
	"time"
)

// LoadChecker 负载检查接口
type LoadChecker interface {
	// Check 检查当前负载状态
	// 参数：ctx 包含执行时间等上下文信息
	// 返回：sleepDuration 睡眠时间，shouldSchedule 是否可以继续调度; shouldSchedule = false时 duration 才有意义
	Check(ctx context.Context) (sleepDuration time.Duration, shouldSchedule bool)
}

// contextKey 是用于在 context 中传递负载检查信息的 key 类型
type contextKey string

// ExecutionTimeContextKey 是在 context 中存储执行时间的 key
const ExecutionTimeContextKey contextKey = "execution_time"

// WithExecutionTime 在 context 中添加执行时间
func WithExecutionTime(ctx context.Context, duration time.Duration) context.Context {
	return context.WithValue(ctx, ExecutionTimeContextKey, duration)
}

// GetExecutionTime 从 context 中获取执行时间
func GetExecutionTime(ctx context.Context) time.Duration {
	if v := ctx.Value(ExecutionTimeContextKey); v != nil {
		if d, ok := v.(time.Duration); ok {
			return d
		}
	}
	return 0
}
