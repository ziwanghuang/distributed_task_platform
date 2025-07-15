package reportevt

import (
	"context"
	"encoding/json"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/errs"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"gitee.com/flycash/distributed_task_platform/pkg/mqx"
	"github.com/ecodeclub/mq-api"
	"github.com/gotomicro/ego/core/elog"
)

type BatchReportEventConsumer struct {
	svc      task.ExecutionService
	consumer *mqx.Consumer
	logger   *elog.Component
}

func NewBatchReportEventConsumer(name string, mq mq.MQ, topic string) *BatchReportEventConsumer {
	return &BatchReportEventConsumer{
		consumer: mqx.NewConsumer(name, mq, topic),
	}
}

func (c *BatchReportEventConsumer) Start(ctx context.Context) {
	if err := c.consumer.Start(ctx, c.consumeBatchExecutionReportEvent); err != nil {
		panic(err)
	}
}

// consumeBatchExecutionReportEvent 异步消费 ExecutionBatchReportEvent 事件
func (c *BatchReportEventConsumer) consumeBatchExecutionReportEvent(ctx context.Context, message *mq.Message) error {
	batchReport := &domain.BatchReport{}
	err := json.Unmarshal(message.Value, batchReport)
	if err != nil {
		c.logger.Error("反序列化MQ消息体失败",
			elog.String("step", "consumeBatchExecutionReportEvent"),
			elog.String("MQ消息体", string(message.Value)),
			elog.FieldErr(err),
		)
		return err
	}

	for i := range batchReport.Reports {
		if !batchReport.Reports[i].ExecutionState.Status.IsValid() {
			err = errs.ErrInvalidTaskExecutionStatus
			c.logger.Error("执行记录状态非法",
				elog.String("step", "consumeBatchExecutionReportEvent"),
				elog.String("MQ消息体", string(message.Value)),
				elog.FieldErr(err),
			)
			return err
		}
	}

	err = c.svc.HandleReports(ctx, batchReport.Reports)
	if err != nil {
		c.logger.Error("处理异步上报失败",
			elog.String("step", "consumeBatchExecutionReportEvent"),
			elog.Any("batchReport", batchReport),
			elog.FieldErr(err))
		return err
	}
	return nil
}
