package reportevt

import (
	"context"
	"encoding/json"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"github.com/ecodeclub/mq-api"
)

type ReportEventProducer interface {
	Produce(ctx context.Context, evt domain.Report) error
}

type reportEventProducer struct {
	producer mq.Producer
}

func NewReportEventProducer(producer mq.Producer) ReportEventProducer {
	return &reportEventProducer{
		producer: producer,
	}
}

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
