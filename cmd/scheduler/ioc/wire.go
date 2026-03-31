//go:build wireinject

// Package ioc 使用 Google Wire 实现编译期依赖注入。
// 本文件定义了调度器应用的所有 Provider Set 和注入入口函数。
// Wire 会根据这些定义自动生成 wire_gen.go，完成依赖图的装配。
//
// 依赖注入的层次结构：
//
//	基础设施层（DB/MQ/etcd/Redis/Prometheus）
//	    ↓
//	数据访问层（DAO → Repository）
//	    ↓
//	服务层（Service → Runner → Scheduler）
//	    ↓
//	应用层（SchedulerApp = gRPC + Scheduler + Tasks）
package ioc

import (
	grpcapi "gitee.com/flycash/distributed_task_platform/internal/grpc"
	"gitee.com/flycash/distributed_task_platform/internal/repository"
	"gitee.com/flycash/distributed_task_platform/internal/repository/dao"
	tasksvc "gitee.com/flycash/distributed_task_platform/internal/service/task"
	"gitee.com/flycash/distributed_task_platform/ioc"
	"github.com/google/wire"
)

var (
	// BaseSet 基础设施 Provider 集合。
	// 包含所有中间件的初始化：MySQL、分布式锁、etcd、Kafka MQ、
	// Runner（执行链路）、Invoker（调用分发）、注册中心、Prometheus 客户端。
	BaseSet = wire.NewSet(
		ioc.InitDB,
		ioc.InitDistributedLock,
		ioc.InitEtcdClient,
		ioc.InitMQ,
		ioc.InitRunner,
		ioc.InitInvoker,
		ioc.InitRegistry,
		ioc.InitPrometheusClient,
	)

	// taskSet 任务管理 Provider 集合。
	// 按 DAO → Repository → Service 的分层结构组织。
	taskSet = wire.NewSet(
		dao.NewGORMTaskDAO,
		repository.NewTaskRepository,
		tasksvc.NewService,
	)

	// taskExecutionSet 任务执行管理 Provider 集合。
	// 负责执行记录的持久化和状态管理。
	taskExecutionSet = wire.NewSet(
		dao.NewGORMTaskExecutionDAO,
		repository.NewTaskExecutionRepository,
		tasksvc.NewExecutionService,
	)

	// planSet DAG 工作流 Provider 集合。
	// PlanService 负责 DSL 解析、DAG 图构建和执行依赖管理。
	planSet = wire.NewSet(
		tasksvc.NewPlanService,
	)

	// schedulerSet 调度器核心 Provider 集合。
	// 包含节点 ID 生成、负载检查、调度器实例、任务获取器、
	// 执行节点 gRPC 客户端和智能节点选择器。
	schedulerSet = wire.NewSet(
		ioc.InitNodeID,
		ioc.InitClusterLoadChecker,
		ioc.InitScheduler,
		ioc.InitMySQLTaskAcquirer,
		ioc.InitExecutorServiceGRPCClients,
		ioc.InitExecutorNodePicker,
	)

	// compensatorSet 补偿器 Provider 集合。
	// 4 个补偿器分别处理：重试、重调度、分片汇总、超时中断。
	compensatorSet = wire.NewSet(
		ioc.InitRetryCompensator,
		ioc.InitRescheduleCompensator,
		ioc.InitShardingCompensator,
		ioc.InitInterruptCompensator,
	)

	// producerSet MQ 生产者 Provider 集合。
	producerSet = wire.NewSet(
		ioc.InitCompleteProducer,
	)

	// consumerSet MQ 消费者 Provider 集合。
	// 单条上报和批量上报两个消费者。
	consumerSet = wire.NewSet(
		ioc.InitExecutionReportEventConsumer,
		ioc.InitExecutionBatchReportEventConsumer,
	)
)

// InitSchedulerApp 是 Wire 的注入入口函数。
// Wire 根据此函数签名和 wire.Build 中的 Provider Set 自动推导依赖图，
// 生成 wire_gen.go 中的实际装配代码。
// 返回完整装配好的 SchedulerApp（含 gRPC 服务端、调度器、所有后台任务）。
func InitSchedulerApp() *ioc.SchedulerApp {
	wire.Build(
		// 基础设施
		BaseSet,

		taskSet,
		taskExecutionSet,
		planSet,
		schedulerSet,
		compensatorSet,
		consumerSet,
		producerSet,

		// GRPC服务器
		grpcapi.NewReporterServer,
		ioc.InitSchedulerNodeGRPCServer,
		ioc.InitTasks,
		wire.Struct(new(ioc.SchedulerApp), "*"),
	)

	return new(ioc.SchedulerApp)
}
