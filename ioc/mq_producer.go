package ioc

import (
	"gitee.com/flycash/distributed_task_platform/internal/event"
	"github.com/ecodeclub/mq-api"
)

func InitCompleteProducer(q mq.MQ) event.CompleteProducer {
	producer, err := q.Producer("")
	if err != nil {
		panic(err)
	}
	return event.NewCompleteProducer(producer)
}
