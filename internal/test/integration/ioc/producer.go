package ioc

import (
	"gitee.com/flycash/distributed_task_platform/internal/event"
	"github.com/ecodeclub/mq-api"
)

func InitCompleteProducer(testmq mq.MQ)event.CompleteProducer{
	pro,err := testmq.Producer("complete_topic")
	if err != nil {
		panic(err)
	}
	return event.NewCompleteProducer(pro)
}
