package ioc

import (
	"context"
	"fmt"
	"gitee.com/flycash/distributed_task_platform/internal/event"
	"gitee.com/flycash/distributed_task_platform/internal/event/complete"
	"gitee.com/flycash/distributed_task_platform/internal/service/runner"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"github.com/ecodeclub/mq-api"
)

func InitConsumers(q mq.MQ, nodeID string) map[string]*event.Consumer {
	return map[string]*event.Consumer{
		"executionReportEvent":      initPushMessageConsumer(q, nodeID),
		"executionBatchReportEvent": initScaleUpConsumer(q, nodeID),
	}
}

func InitCompleteConsumer(q mq.MQ,
	planRunner *runner.PlanRunner,
	taskSvc task.Service,
	execSvc task.ExecutionService,
	nodeID string,
) *CompleteConsumer{
	topic := "complete_topic"
	partition := 1
	con := event.NewConsumer(name(topic, nodeID), q, topic, partition)
	comConsumer := complete.NewConsumer(planRunner, execSvc, taskSvc)
	return &CompleteConsumer{
		com: con,
		Consumer: comConsumer,
	}
}

type CompleteConsumer struct {
	*complete.Consumer
	com *event.Consumer
}

func (c *CompleteConsumer) Start(ctx context.Context, com event.Consumer)  {
	err := c.com.Start(context.Background(), c.Consume)
	if err != nil {
		panic(err)
	}
}
func initPushMessageConsumer(q mq.MQ, nodeID string) *event.Consumer {
	topic := "execution_report"
	partitions := 1
	return event.NewConsumer(name("executionReportEvent", nodeID), q, topic, partitions)
}

func initScaleUpConsumer(q mq.MQ, nodeID string) *event.Consumer {
	topic := "execution_batch_report"
	partitions := 1
	return event.NewConsumer(name("executionBatchReportEvent", nodeID), q, topic, partitions)
}

func name(eventName, nodeID string) string {
	return fmt.Sprintf("%s-%s", eventName, nodeID)
}
