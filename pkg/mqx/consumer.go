// Package mqx 提供消息队列消费者的通用封装。
//
// 本包对 ecodeclub/mq-api 的消费者接口进行了封装，提供：
//   - 统一的消费者生命周期管理（Start/Stop）
//   - 内置日志记录（消费成功/失败均有日志）
//   - 双 context 退出机制（内部 context + 外部传入 context）
//
// 在调度平台中的使用场景：
//   - 消费 report_event topic：异步接收 executor 上报的任务执行状态
//   - 消费 complete_event topic：接收任务完成事件（如需要）
//
// 消费模式：单条消费（每次从 channel 读取一条消息并处理）。
package mqx

import (
	"context"

	"github.com/ecodeclub/mq-api"
	"github.com/gotomicro/ego/core/elog"
)

type (
	// ConsumeFunc 是消息处理函数类型。
	// 接收 context 和单条消息，返回处理错误。
	ConsumeFunc func(ctx context.Context, message *mq.Message) error

	// Consumer 是 MQ 消费者封装。
	//
	// 核心字段：
	//   - name:   消费者名称，同时作为 MQ 的 consumer group 标识
	//   - mq:     MQ 实例（底层可以是 Kafka、内存 MQ 等）
	//   - topic:  订阅的 topic 名称
	//   - ctx:    内部 context，调用 Stop() 时取消
	//   - cancel: 内部 context 的取消函数
	//   - logger: 日志组件，自动携带消费者名称标签
	Consumer struct {
		name string

		mq     mq.MQ
		topic  string
		ctx    context.Context
		cancel context.CancelFunc

		logger *elog.Component
	}
)

// NewConsumer 创建一个新的 MQ 消费者。
//
// 参数：
//   - name:  消费者名称，作为 consumer group 标识和日志标签
//   - mq:    MQ 实例
//   - topic: 要订阅的 topic
func NewConsumer(name string, mq mq.MQ, topic string) *Consumer {
	ctx, cancelFunc := context.WithCancel(context.Background())
	return &Consumer{
		name:   name,
		mq:     mq,
		topic:  topic,
		ctx:    ctx,
		cancel: cancelFunc,
		logger: elog.DefaultLogger.With(elog.FieldComponent(name)),
	}
}

// Start 启动消费者，开始从指定 topic 消费消息。
//
// 工作流程：
//  1. 创建 MQ 消费者实例（指定 topic 和 consumer group）
//  2. 获取消费 channel
//  3. 启动后台 goroutine 进行持续消费
//
// 返回错误仅在创建消费者或获取 channel 失败时发生。
func (c *Consumer) Start(ctx context.Context, consumeFunc ConsumeFunc) error {
	consumer, err := c.mq.Consumer(c.topic, c.name)
	if err != nil {
		c.logger.Error("获取MQ消费者失败",
			elog.FieldErr(err),
		)
		return err
	}
	ch, err := consumer.ConsumeChan(ctx)
	if err != nil {
		c.logger.Error("获取MQ消费者Chan失败",
			elog.FieldErr(err),
		)
		return err
	}
	go c.consume(ctx, ch, consumeFunc)
	return nil
}

// consume 是消费循环的核心方法，在独立 goroutine 中运行。
//
// 退出条件（三选一）：
//   - 内部 ctx 被取消（Stop() 被调用）
//   - 外部 ctx 被取消（上层服务关闭）
//   - mqChan 被关闭（MQ 连接断开）
//
// 每条消息的处理结果都会记录日志（成功/失败），便于问题排查。
func (c *Consumer) consume(ctx context.Context, mqChan <-chan *mq.Message, consumeFunc func(ctx context.Context, message *mq.Message) error) {
	c.logger.Info("消费者已启动")
	for {
		select {
		case <-c.ctx.Done():
			c.logger.Info("内部上下文取消，结束消费循环")
			return
		case <-ctx.Done():
			c.logger.Info("参数上下文取消，结束消费循环")
			return
		case message, ok := <-mqChan:
			if !ok {
				return
			}
			err := consumeFunc(ctx, message)
			if err != nil {
				c.logger.Error("消费消息失败",
					elog.String("消息体", string(message.Value)),
					elog.FieldErr(err))
			}
			c.logger.Info("消费消息成功",
				elog.String("消息体", string(message.Value)),
			)
		}
	}
}

// Name 返回消费者名称。
func (c *Consumer) Name() string {
	return c.name
}

// Stop 停止消费者，取消内部 context 以退出消费循环。
func (c *Consumer) Stop() error {
	c.cancel()
	return nil
}
