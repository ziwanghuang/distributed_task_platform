// Package reportevt 实现了基于消息队列（MQ）的执行状态上报事件的生产和消费。
//
// 除了执行节点通过 gRPC 直接上报状态外，系统还支持通过 Kafka MQ 异步上报。
// 异步链路适用于：
//   - 执行节点与调度平台不在同一网络环境（需要跨网络传输）
//   - 高吞吐场景下的状态上报削峰
//   - 需要消息持久化保证的场景
//
// 组件：
//   - ReportEventConsumer: 消费 MQ 中的上报事件，委托给 ExecutionService 处理
//   - ReportEventProducer: 将上报事件序列化后发送到 MQ
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

// ReportEventConsumer 是 MQ 上报事件的消费者。
// 从 Kafka topic 中拉取执行状态上报消息，反序列化后委托给 ExecutionService.HandleReports 处理。
// 与 gRPC 上报链路共享相同的业务处理逻辑。
type ReportEventConsumer struct {
	execSvc  task.ExecutionService // 执行记录服务，处理上报的状态更新
	consumer *mqx.Consumer        // MQ 消费者封装，提供 Start/消费回调机制
	logger   *elog.Component      // 日志组件
}

// NewReportEventConsumer 创建 MQ 上报事件消费者。
// 参数说明：
//   - name: 消费者名称，用于日志标识和 MQ consumer group
//   - mq: MQ 实例（Kafka 实现）
//   - topic: 要消费的 topic 名称
//   - execSvc: 执行记录服务
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
