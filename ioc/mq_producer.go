package ioc

import (
	"gitee.com/flycash/distributed_task_platform/internal/event"
	"github.com/ecodeclub/mq-api"
)

// InitCompleteProducer 初始化任务完成事件生产者。
// 当任务执行完成（成功或失败）时，通过该生产者发送 MQ 消息，
// 触发后续的状态更新、通知等异步处理。
// 使用空字符串作为 producer name，表示使用默认配置。
func InitCompleteProducer(q mq.MQ) event.CompleteProducer {
	producer, err := q.Producer("")
	if err != nil {
		panic(err)
	}
	return event.NewCompleteProducer(producer)
}
