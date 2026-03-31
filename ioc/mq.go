package ioc

import (
	"sync"
	"time"

	"github.com/ecodeclub/ekit/retry"
	"github.com/ecodeclub/mq-api"
)

// 包级变量：MQ 实例和初始化保护锁。
// 使用 sync.Once 确保 MQ 连接只初始化一次（Wire 可能多次调用 InitMQ）。
var (
	q          mq.MQ
	mqInitOnce sync.Once
)

// InitMQ 初始化消息队列连接（Kafka）。
// 使用 sync.Once + 指数退避重试（初始 1s，最大 10s，最多 10 次），
// 确保在 Kafka 容器启动较慢时也能成功连接。
// 底层调用 initMQ() 完成实际的 Kafka 连接和 Topic 创建。
func InitMQ() mq.MQ {
	mqInitOnce.Do(func() {
		const maxInterval = 10 * time.Second
		const maxRetries = 10
		strategy, err := retry.NewExponentialBackoffRetryStrategy(time.Second, maxInterval, maxRetries)
		if err != nil {
			panic(err)
		}
		for {
			q, err = initMQ()
			if err == nil {
				break
			}
			next, ok := strategy.Next()
			if !ok {
				panic("InitMQ 重试失败......")
			}
			time.Sleep(next)
		}
	})
	return q
}
