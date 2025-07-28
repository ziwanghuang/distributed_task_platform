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

type ReportEventConsumer struct {
	execSvc  task.ExecutionService
	consumer *mqx.Consumer
	logger   *elog.Component
}

func NewReportEventConsumer(name string, mq mq.MQ, topic string, execSvc task.ExecutionService) *ReportEventConsumer {
	return &ReportEventConsumer{
		consumer: mqx.NewConsumer(name, mq, topic),
		execSvc:  execSvc,
		logger:   elog.DefaultLogger.With(elog.FieldComponentName("reportevt.ReportEventConsumer")),
	}
}

func (c *ReportEventConsumer) Start(ctx context.Context) {
	if err := c.consumer.Start(ctx, c.consumeExecutionReportEvent); err != nil {
		panic(err)
	}
}

// consumeExecutionReportEvent 异步消费 ExecutionReportEvent 事件
func (c *ReportEventConsumer) consumeExecutionReportEvent(ctx context.Context, message *mq.Message) error {
	report := &domain.Report{}
	err := json.Unmarshal(message.Value, report)
	if err != nil {
		c.logger.Error("反序列化MQ消息体失败",
			elog.String("step", "consumeExecutionReportEvent"),
			elog.String("MQ消息体", string(message.Value)),
			elog.FieldErr(err),
		)
		return err
	}

	if !report.ExecutionState.Status.IsValid() {
		err = errs.ErrInvalidTaskExecutionStatus
		c.logger.Error("执行记录状态非法",
			elog.String("step", "consumeExecutionReportEvent"),
			elog.String("MQ消息体", string(message.Value)),
			elog.FieldErr(err),
		)
		return err
	}

	err = c.execSvc.HandleReports(ctx, []*domain.Report{report})
	if err != nil {
		c.logger.Error("处理异步上报失败",
			elog.String("step", "consumeExecutionReportEvent"),
			elog.Any("report", report),
			elog.FieldErr(err))
		return err
	}
	return nil
}
