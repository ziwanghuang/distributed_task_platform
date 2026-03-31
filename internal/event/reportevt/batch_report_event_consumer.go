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

// NewBatchReportEventConsumer 创建批量上报事件消费者
// 该消费者负责从 MQ 批量消费执行节点上报的 BatchReport 事件，
// 并委托给 ExecutionService 进行状态更新处理。
func NewBatchReportEventConsumer(name string, mq mq.MQ, topic string, svc task.ExecutionService) *BatchReportEventConsumer {
	return &BatchReportEventConsumer{
		consumer: mqx.NewConsumer(name, mq, topic),
		svc:      svc,
		logger:   elog.DefaultLogger.With(elog.FieldComponentName("reportevt.BatchReportEventConsumer")),
	}
}

// Start 启动消费者，开始监听 MQ topic 消费批量上报事件。
// 如果底层 MQ 消费者启动失败会 panic，因为这属于不可恢复的初始化错误。
func (c *BatchReportEventConsumer) Start(ctx context.Context) {
	if err := c.consumer.Start(ctx, c.consumeBatchExecutionReportEvent); err != nil {
		panic(err)
	}
}

// consumeBatchExecutionReportEvent 异步消费 ExecutionBatchReportEvent 事件。
// 处理流程：
//  1. 反序列化 MQ 消息体为 BatchReport 结构
//  2. 校验每条上报记录的执行状态是否合法（防止脏数据污染）
//  3. 委托给 ExecutionService.HandleReports 批量处理状态更新
//
// 返回 error 时，MQ 框架会根据配置进行重试或将消息投入死信队列。
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

	// 校验阶段：逐条检查上报记录的执行状态是否合法
	// 只要有一条非法就拒绝整个批次，保证数据完整性
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

	// 委托给执行服务批量处理，内部会逐条调用 UpdateState 进行状态机迁移
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
