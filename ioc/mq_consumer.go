package ioc

import (
	"fmt"
	"gitee.com/flycash/distributed_task_platform/pkg/mqx"

	"github.com/ecodeclub/mq-api"
	"github.com/gotomicro/ego/core/econf"
)

func InitConsumers(q mq.MQ, nodeID string) map[string]*mqx.Consumer {
	return map[string]*mqx.Consumer{
		"executionReportEvent":      initPushMessageConsumer(q, nodeID),
		"executionBatchReportEvent": initScaleUpConsumer(q, nodeID),
	}
}

func initPushMessageConsumer(q mq.MQ, nodeID string) *mqx.Consumer {
	topic := econf.GetString("executionReportEvent.topic")
	return mqx.NewConsumer(name("executionReportEvent", nodeID), q, topic)
}

func initScaleUpConsumer(q mq.MQ, nodeID string) *mqx.Consumer {
	topic := econf.GetString("executionBatchReportEvent.topic")
	return mqx.NewConsumer(name("executionBatchReportEvent", nodeID), q, topic)
}

func name(eventName, nodeID string) string {
	return fmt.Sprintf("%s-%s", eventName, nodeID)
}
