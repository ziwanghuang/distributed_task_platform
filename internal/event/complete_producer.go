package event

import (
	"context"
	"encoding/json"

	"github.com/ecodeclub/mq-api"
)

// CompleteProducer 任务完成事件生产者接口。
// 当任务执行到达终止状态（Success/Failed/Cancelled）时，
// 通过该接口向 MQ 发送完成事件，驱动下游消费者处理后续逻辑。
type CompleteProducer interface {
	Produce(ctx context.Context, evt Event) error
}

// completeProducer CompleteProducer 的 MQ 实现。
// 将 Event 序列化为 JSON 后发送到 Kafka topic。
type completeProducer struct {
	producer mq.Producer // 底层 MQ 生产者
}

// NewCompleteProducer 创建完成事件生产者
func NewCompleteProducer(producer mq.Producer) CompleteProducer {
	return &completeProducer{
		producer: producer,
	}
}

// Produce 发送任务完成事件到 MQ。
// 序列化 Event 为 JSON，发送到预配置的 Kafka topic。
// 下游消费者（complete.Consumer）会根据事件类型路由处理。
func (c *completeProducer) Produce(ctx context.Context, evt Event) error {
	val, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	_, err = c.producer.Produce(ctx, &mq.Message{
		Value: val,
	})
	return err
}
