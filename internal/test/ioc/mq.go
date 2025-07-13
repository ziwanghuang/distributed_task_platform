package ioc

import (
	"context"
	"sync"
	"time"

	"github.com/ecodeclub/ekit/retry"
	"github.com/ecodeclub/mq-api"
	"github.com/ecodeclub/mq-api/memory"
)

var (
	q          mq.MQ
	mqInitOnce sync.Once
)

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

func initMQ() (mq.MQ, error) {
	type Topic struct {
		Name       string `yaml:"name"`
		Partitions int    `yaml:"partitions"`
	}
	topics := []Topic{
		{
			Name:       "test_topic",
			Partitions: 1,
		},
		{
			Name:       "complete_topic",
			Partitions: 1,
		},
	}
	// 替换用内存实现，方便测试
	qq := memory.NewMQ()
	for _, t := range topics {
		err := qq.CreateTopic(context.Background(), t.Name, t.Partitions)
		if err != nil {
			return nil, err
		}
	}
	return qq, nil
}
