package ioc

import (
	"fmt"

	"gitee.com/flycash/distributed_task_platform/internal/event"
	"github.com/ecodeclub/mq-api"
	"github.com/gotomicro/ego/core/econf"
)

func InitConsumers(q mq.MQ, nodeID string) map[string]*event.Consumer {
	return map[string]*event.Consumer{
		"executionReportEvent":      initPushMessageConsumer(q, nodeID),
		"executionBatchReportEvent": initScaleUpConsumer(q, nodeID),
	}
}

func initPushMessageConsumer(q mq.MQ, nodeID string) *event.Consumer {
	topic := econf.GetString("executionReportEvent.topic")
	partitions := econf.GetInt("executionReportEvent.partitions")
	return event.NewConsumer(name("executionReportEvent", nodeID), q, topic, partitions)
}

func initScaleUpConsumer(q mq.MQ, nodeID string) *event.Consumer {
	topic := econf.GetString("executionBatchReportEvent.topic")
	partitions := econf.GetInt("executionBatchReportEvent.partitions")
	return event.NewConsumer(name("executionBatchReportEvent", nodeID), q, topic, partitions)
}

func name(eventName, nodeID string) string {
	return fmt.Sprintf("%s-%s", eventName, nodeID)
}
