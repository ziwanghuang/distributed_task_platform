package main

import (
	"fmt"
	"gitee.com/flycash/distributed_task_platform/example/ioc"
)

func main() {
	etcdClient := ioc.InitEtcdClient()
	reg := ioc.InitRegistry(etcdClient)
	// 启动十个节点
	ch := make(chan int)
	for i := 0; i < 10; i++ {
		port := 8890 + i
		node := fmt.Sprintf("%s:%d", serverName, port)
		executorNode := NewExecutorNode(node, port, reg)
		err := executorNode.Start()
		if err != nil {
			panic(err)
		}
	}
	// 阻塞住，不让程序退出
	<-ch
}
