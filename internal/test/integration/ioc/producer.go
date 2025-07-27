package ioc

import (
	"gitee.com/flycash/distributed_task_platform/internal/event"
	"gitee.com/flycash/distributed_task_platform/internal/event/reportevt"
	"github.com/ecodeclub/mq-api"
)

func InitCompleteProducer(testmq mq.MQ) event.CompleteProducer {
	pro, err := testmq.Producer("complete_topic")
	if err != nil {
		panic(err)
	}
	return event.NewCompleteProducer(pro)
}

func InitReportEventProducer(testmq mq.MQ) reportevt.ReportEventProducer {
	pro, err := testmq.Producer("execution_report")
	if err != nil {
		panic(err)
	}
	return reportevt.NewReportEventProducer(pro)
}
