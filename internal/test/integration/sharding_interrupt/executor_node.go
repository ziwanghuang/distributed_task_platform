package integration

import (
	"context"
	"fmt"
	"net"
	"time"

	executorv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/executor/v1"
	"gitee.com/flycash/distributed_task_platform/internal/event/reportevt"
	"gitee.com/flycash/distributed_task_platform/pkg/grpc/registry"
	"github.com/ecodeclub/mq-api"
	"github.com/gotomicro/ego/core/elog"
	"google.golang.org/grpc"
)

const (
	defaultInitCapacity = 10
	defaultMaxCapacity  = 100
	defaultIncreaseStep = 10
	defaultGrowthRate   = 1.2
	startupDelay        = 100 * time.Millisecond
)

// ExecutorNode 执行节点，封装了MockExecutorService、gRPC服务器和Registry注册
type ExecutorNode struct {
	nodeID, testDataDir string
	port                int
	registry            registry.Registry
	mq                  mq.MQ

	// 内部组件
	mockService    *MockExecutorService
	reportProducer reportevt.ReportEventProducer
	grpcServer     *grpc.Server
	listener       net.Listener

	logger *elog.Component

	// 控制
	ctx    context.Context
	cancel context.CancelFunc
}

// NewExecutorNode 创建执行节点
func NewExecutorNode(nodeID, testDataDir string, port int, registry registry.Registry, mq mq.MQ) *ExecutorNode {
	ctx, cancel := context.WithCancel(context.Background())

	return &ExecutorNode{
		nodeID:      nodeID,
		port:        port,
		registry:    registry,
		testDataDir: testDataDir,
		mq:          mq,
		logger:      elog.DefaultLogger.With(elog.FieldComponentName("executor-node-" + nodeID)),
		ctx:         ctx,
		cancel:      cancel,
	}
}

// Start 启动执行节点
func (en *ExecutorNode) Start() error {
	en.logger.Info("启动执行节点",
		elog.String("nodeID", en.nodeID),
		elog.Int("port", en.port))

	// 1. 创建ReportEventProducer
	producer, err := en.mq.Producer("execution_report")
	if err != nil {
		return fmt.Errorf("创建生产者失败: %w", err)
	}
	en.reportProducer = reportevt.NewReportEventProducer(producer)

	// 2. 创建MockExecutorService
	en.mockService = NewMockExecutorService(en.nodeID, en.testDataDir, en.reportProducer)

	// 3. 创建gRPC服务器
	en.grpcServer = grpc.NewServer()
	executorv1.RegisterExecutorServiceServer(en.grpcServer, en.mockService)

	// 4. 创建监听器
	en.listener, err = net.Listen("tcp", fmt.Sprintf(":%d", en.port))
	if err != nil {
		return fmt.Errorf("监听端口失败: %w", err)
	}

	// 5. 注册到Registry
	serviceInstance := registry.ServiceInstance{
		Name:         "executor-service",
		Address:      fmt.Sprintf("localhost:%d", en.port),
		ID:           en.nodeID,
		InitCapacity: defaultInitCapacity,
		MaxCapacity:  defaultMaxCapacity,
		IncreaseStep: defaultIncreaseStep,
		GrowthRate:   defaultGrowthRate,
	}

	if err := en.registry.Register(en.ctx, serviceInstance); err != nil {
		en.listener.Close()
		return fmt.Errorf("注册服务失败: %w", err)
	}

	en.logger.Info("服务注册成功",
		elog.String("address", serviceInstance.Address),
		elog.String("nodeID", en.nodeID))

	// 6. 启动gRPC服务器（异步）
	go func() {
		en.logger.Info("gRPC服务器开始监听", elog.Int("port", en.port))
		if err := en.grpcServer.Serve(en.listener); err != nil {
			en.logger.Error("gRPC服务器运行错误", elog.FieldErr(err))
		}
	}()

	// 7. 等待服务器启动
	time.Sleep(startupDelay)

	en.logger.Info("执行节点启动完成", elog.String("nodeID", en.nodeID))
	return nil
}

// Stop 停止执行节点
func (en *ExecutorNode) Stop() error {
	en.logger.Info("停止执行节点", elog.String("nodeID", en.nodeID))

	// 1. 取消上下文
	en.cancel()

	// 2. 从Registry注销
	serviceInstance := registry.ServiceInstance{
		Name:    "executor-service",
		Address: fmt.Sprintf("localhost:%d", en.port),
		ID:      en.nodeID,
	}

	if err := en.registry.UnRegister(context.Background(), serviceInstance); err != nil {
		en.logger.Error("注销服务失败", elog.FieldErr(err))
	} else {
		en.logger.Info("服务注销成功", elog.String("nodeID", en.nodeID))
	}

	// 3. 停止gRPC服务器
	if en.grpcServer != nil {
		en.grpcServer.GracefulStop()
		en.logger.Info("gRPC服务器已停止", elog.String("nodeID", en.nodeID))
	}

	// 4. 关闭监听器
	if en.listener != nil {
		en.listener.Close()
	}

	en.logger.Info("执行节点已停止", elog.String("nodeID", en.nodeID))
	return nil
}

// GetAddress 获取节点地址
func (en *ExecutorNode) GetAddress() string {
	return fmt.Sprintf("localhost:%d", en.port)
}

// GetNodeID 获取节点ID
func (en *ExecutorNode) GetNodeID() string {
	return en.nodeID
}

// IsRunning 检查节点是否在运行
func (en *ExecutorNode) IsRunning() bool {
	return en.grpcServer != nil && en.listener != nil
}
