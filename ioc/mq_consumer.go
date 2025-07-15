package ioc

import (
	"fmt"

	"gitee.com/flycash/distributed_task_platform/internal/event/reportevt"

	"github.com/ecodeclub/mq-api"
	"github.com/gotomicro/ego/core/econf"
)

func InitExecutionReportEventConsumer(q mq.MQ, nodeID string) *reportevt.ReportEventConsumer {
	topic := econf.GetString("executionReportEvent.topic")
	return reportevt.NewReportEventConsumer(name("executionReportEvent", nodeID), q, topic)
}

func InitExecutionBatchReportEventConsumer(q mq.MQ, nodeID string) *reportevt.BatchReportEventConsumer {
	topic := econf.GetString("executionBatchReportEvent.topic")
	return reportevt.NewBatchReportEventConsumer(name("executionBatchReportEvent", nodeID), q, topic)
}

func name(eventName, nodeID string) string {
	return fmt.Sprintf("%s-%s", eventName, nodeID)
}
