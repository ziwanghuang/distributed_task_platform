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
	logger   *elog.Component
	svc      task.ExecutionService
	consumer *mqx.Consumer
}

func NewBatchReportEventConsumer(name string, mq mq.MQ, topic string) *BatchReportEventConsumer {
	return &BatchReportEventConsumer{
		consumer: mqx.NewConsumer(name, mq, topic),
	}
}

func (c *BatchReportEventConsumer) Start(ctx context.Context) error {
	return c.consumer.Start(ctx, c.consumeExecutionReportEvent)
}

// consumeExecutionReportEvent 异步消费 ExecutionReport 事件
func (s *BatchReportEventConsumer) consumeExecutionReportEvent(ctx context.Context, message *mq.Message) error {
	report := &domain.Report{}
	err := json.Unmarshal(message.Value, report)
	if err != nil {
		s.logger.Error("反序列化MQ消息体失败",
			elog.String("step", "consumeExecutionReportEvent"),
			elog.String("MQ消息体", string(message.Value)),
			elog.FieldErr(err),
		)
		return err
	}

	if !report.ExecutionState.Status.IsValid() {
		err = errs.ErrInvalidTaskExecutionStatus
		s.logger.Error("执行记录状态非法",
			elog.String("step", "consumeExecutionReportEvent"),
			elog.String("MQ消息体", string(message.Value)),
			elog.FieldErr(err),
		)
		return err
	}

	err = s.svc.HandleReports(ctx, []*domain.Report{report})
	if err != nil {
		s.logger.Error("处理异步上报失败",
			elog.String("step", "consumeExecutionReportEvent"),
			elog.Any("report", report),
			elog.FieldErr(err))
		return err
	}
	return nil
}
