package ioc

import (
	"context"
	"fmt"

	"gitee.com/flycash/distributed_task_platform/internal/event/complete"
	"gitee.com/flycash/distributed_task_platform/internal/service/acquirer"
	"gitee.com/flycash/distributed_task_platform/internal/service/runner"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"gitee.com/flycash/distributed_task_platform/pkg/mqx"
	"github.com/ecodeclub/mq-api"
)

func InitCompleteConsumer(q mq.MQ,
	planRunner *runner.PlanRunner,
	taskSvc task.Service,
	execSvc task.ExecutionService,
	acquire acquirer.TaskAcquirer,
	nodeID string,
) *CompleteConsumer {
	topic := "complete_topic"
	con := mqx.NewConsumer(name(topic, nodeID), q, topic)
	comConsumer := complete.NewConsumer(planRunner, execSvc, taskSvc, acquire)
	return &CompleteConsumer{
		com:      con,
		Consumer: comConsumer,
	}
}

type CompleteConsumer struct {
	*complete.Consumer
	com *mqx.Consumer
}

func (c *CompleteConsumer) Start() {
	err := c.com.Start(context.Background(), c.Consume)
	if err != nil {
		panic(err)
	}
}

func InitConsumers(q mq.MQ, nodeID string) map[string]*mqx.Consumer {
	return map[string]*mqx.Consumer{
		"executionReportEvent":      initPushMessageConsumer(q, nodeID),
		"executionBatchReportEvent": initScaleUpConsumer(q, nodeID),
	}
}

func initPushMessageConsumer(q mq.MQ, nodeID string) *mqx.Consumer {
	topic := "execution_report"
	return mqx.NewConsumer(name("executionReportEvent", nodeID), q, topic)
}

func initScaleUpConsumer(q mq.MQ, nodeID string) *mqx.Consumer {
	topic := "execution_batch_report"
	return mqx.NewConsumer(name("executionBatchReportEvent", nodeID), q, topic)
}

func name(eventName, nodeID string) string {
	return fmt.Sprintf("%s-%s", eventName, nodeID)
}
