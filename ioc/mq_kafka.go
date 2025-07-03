package ioc

import (
	"context"
	"log"

	"github.com/ecodeclub/mq-api"
	"github.com/ecodeclub/mq-api/kafka"
	"github.com/gotomicro/ego/core/econf"
)

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
