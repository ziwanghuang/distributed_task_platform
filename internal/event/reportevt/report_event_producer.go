package reportevt

import (
	"context"
	"encoding/json"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"github.com/ecodeclub/mq-api"
)

// ReportEventProducer 定义了上报事件生产者的接口。
// 调用方通过 Produce 方法将执行状态上报事件发送到 MQ，
// 供 ReportEventConsumer 异步消费处理。
type ReportEventProducer interface {
	// Produce 将执行状态上报事件序列化为 JSON 并发送到 Kafka topic。
	// 参数 evt 包含完整的执行状态信息（执行 ID、状态、进度等）。
	Produce(ctx context.Context, evt domain.Report) error
}

// reportEventProducer 是 ReportEventProducer 接口的 Kafka 实现。
// 内部持有 mq.Producer，负责将 domain.Report 序列化为 JSON 后发送到指定 topic。
type reportEventProducer struct {
	producer mq.Producer // MQ 生产者（Kafka 实现）
}

// NewReportEventProducer 创建上报事件生产者实例。
// 参数 producer 为 MQ 生产者，通常由 ioc 层初始化并绑定到指定的 Kafka topic。
func NewReportEventProducer(producer mq.Producer) ReportEventProducer {
	return &reportEventProducer{
		producer: producer,
	}
}

// Produce 将执行状态上报事件序列化并发送到 Kafka。
// 序列化格式为 JSON，消息体对应 domain.Report 的 JSON 表示。
func (c *reportEventProducer) Produce(ctx context.Context, evt domain.Report) error {
	val, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	_, err = c.producer.Produce(ctx, &mq.Message{
		Value: val,
	})
	return err
}
