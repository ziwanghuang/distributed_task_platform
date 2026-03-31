// Package strategy 定义重试策略接口和具体策略实现。
//
// 本包提供两种重试策略：
//   - FixedIntervalRetryStrategy:        固定间隔重试，每次重试等待相同时间
//   - ExponentialBackoffRetryStrategy:   指数退避重试，每次重试等待时间指数增长
//
// Strategy 接口设计为支持两种使用模式：
//   - 有状态模式：调用 Next()，内部自动维护重试计数器（适合单次任务重试）
//   - 无状态模式：调用 NextWithRetries(retries)，外部传入当前重试次数（适合补偿器场景，从 DB 读取重试次数）
package strategy

import (
	"time"
)

// Strategy 是重试策略的统一接口。
//
// 方法说明：
//   - NextWithRetries: 根据外部传入的重试次数计算下一次重试间隔。
//     返回 (间隔, true) 表示应该继续重试，返回 (0, false) 表示已达最大重试次数。
//   - Next: 内部自增重试计数器后计算间隔（有状态，不建议并发使用）
//   - Report: 上报错误信息（当前实现为空操作，预留给未来的自适应重试策略）
type Strategy interface {
	// NextWithRetries 根据当前重试次数计算下一次重试间隔，如果不需要继续重试，那么第二参数返回 false
	NextWithRetries(retries int32) (time.Duration, bool)
	// Next 返回下一次重试的间隔，如果不需要继续重试，那么第二参数返回 false
	Next() (time.Duration, bool)
	Report(err error) Strategy
}
