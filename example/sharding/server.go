package main

import (
	"context"
	"fmt"
	reporterv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/reporter/v1"
	"github.com/gotomicro/ego/client/egrpc"
	"time"

	executorv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/executor/v1"
	exampleGrpc "gitee.com/flycash/distributed_task_platform/example/grpc"
	"gitee.com/flycash/distributed_task_platform/pkg/grpc/registry"
	"github.com/gotomicro/ego/core/elog"
)

const (
	defaultInitCapacity = 10
	defaultMaxCapacity  = 100
	defaultIncreaseStep = 10
	defaultGrowthRate   = 1.2
	startupDelay        = 100 * time.Millisecond
	serverName          = "shardingJob"
)

// ExecutorNode 执行节点，封装了MockExecutorService、gRPC服务器和Registry注册
type ExecutorNode struct {
	nodeID   string
	port     int
	registry registry.Registry
	// 内部组件
	executor   *exampleGrpc.Executor
	grpcServer *exampleGrpc.Server

	logger *elog.Component
	// 控制
	ctx    context.Context
	cancel context.CancelFunc
}

// NewExecutorNode 创建执行节点
func NewExecutorNode(nodeID string, port int, registry registry.Registry) *ExecutorNode {
	ctx, cancel := context.WithCancel(context.Background())
	return &ExecutorNode{
		nodeID:   nodeID,
		port:     port,
		registry: registry,
		logger:   elog.DefaultLogger.With(elog.FieldComponentName("executor-node-" + nodeID)),
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Start 启动执行节点
func (en *ExecutorNode) Start() error {
	en.logger.Info("启动执行节点",
		elog.String("nodeID", en.nodeID),
		elog.Int("port", en.port))
	conn := egrpc.Load("").Build(egrpc.WithAddr("127.0.0.1:9002"))
	client := reporterv1.NewReporterServiceClient(conn)
	// 初始化 执行器 每次1000个
	executor := exampleGrpc.NewExecutor(client, 1000)
	// 初始化 grpc服务器
	en.grpcServer = exampleGrpc.NewServer(serverName, fmt.Sprintf("127.0.0.1:%d", en.port), en.registry)
	// 注册
	executorv1.RegisterExecutorServiceServer(en.grpcServer, executor)
	//  启动gRPC服务器（异步）
	go func() {
		en.logger.Info("gRPC服务器开始监听", elog.Int("port", en.port))
		if err := en.grpcServer.Run(); err != nil {
			en.logger.Error("gRPC服务器运行错误", elog.FieldErr(err))
		}
	}()
	time.Sleep(startupDelay)
	en.logger.Info("执行节点启动完成", elog.String("nodeID", en.nodeID))
	return nil
}
