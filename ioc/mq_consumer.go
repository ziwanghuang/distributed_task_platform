package ioc

import (
	"fmt"

	"gitee.com/flycash/distributed_task_platform/internal/event/reportevt"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"

	"github.com/ecodeclub/mq-api"
	"github.com/gotomicro/ego/core/econf"
)

// InitExecutionReportEventConsumer 创建单条上报事件消费者。
// 消费 Kafka 中的单条 Report 消息，处理执行节点上报的执行状态。
func InitExecutionReportEventConsumer(q mq.MQ, nodeID string, execSvc task.ExecutionService) *reportevt.ReportEventConsumer {
	topic := econf.GetString("executionReportEvent.topic")
	return reportevt.NewReportEventConsumer(name("executionReportEvent", nodeID), q, topic, execSvc)
}

// InitExecutionBatchReportEventConsumer 创建批量上报事件消费者。
// 消费 Kafka 中的 BatchReport 消息，批量处理执行节点上报的执行状态。
func InitExecutionBatchReportEventConsumer(q mq.MQ, nodeID string, execSvc task.ExecutionService) *reportevt.BatchReportEventConsumer {
	topic := econf.GetString("executionBatchReportEvent.topic")
	return reportevt.NewBatchReportEventConsumer(name("executionBatchReportEvent", nodeID), q, topic, execSvc)
}

func name(eventName, nodeID string) string {
	return fmt.Sprintf("%s-%s", eventName, nodeID)
}
