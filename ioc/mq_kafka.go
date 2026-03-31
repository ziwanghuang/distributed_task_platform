package ioc

import (
	"context"
	"log"

	"github.com/ecodeclub/mq-api"
	"github.com/ecodeclub/mq-api/kafka"
	"github.com/gotomicro/ego/core/econf"
)

// initMQ 创建 Kafka MQ 连接并初始化所需的 Topic。
// 从 config.yaml 读取 Kafka 网络协议和地址列表，
// 连接成功后自动创建以下 Topic：
//   - executionReportEvent: 单条执行状态上报
//   - executionBatchReportEvent: 批量执行状态上报
func initMQ() (mq.MQ, error) {
	network := econf.GetString("mq.kafka.network")
	addresses := econf.GetStringSlice("mq.kafka.addr")
	log.Printf("initMQ: network = %#v, addr = %#v\n", network, addresses)
	queue, err := kafka.NewMQ(network, addresses)
	if err != nil {
		return nil, err
	}
	err = createTopic(queue, "executionReportEvent.topic", "executionReportEvent.partitions")
	if err != nil {
		return nil, err
	}
	err = createTopic(queue, "executionBatchReportEvent.topic", "executionBatchReportEvent.partitions")
	if err != nil {
		return nil, err
	}
	return queue, nil
}

// createTopic 从配置文件读取 Topic 名称和分区数，创建 Kafka Topic。
// topicKey 和 partitionsKey 是 config.yaml 中的配置键名。
func createTopic(queue mq.MQ, topicKey, partitionsKey string) error {
	topic := econf.GetString(topicKey)
	partitions := econf.GetInt(partitionsKey)
	log.Printf("initMQ: Topic = %#v, Partitions = %#v\n", topic, partitions)
	err := queue.CreateTopic(context.Background(), topic, partitions)
	if err != nil {
		return err
	}
	return nil
}
