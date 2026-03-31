// Package main 分布式任务调度平台 - 调度节点入口。
// 基于 ego 微服务框架启动，包含以下核心组件：
//   - gRPC 服务端：接收执行节点的状态上报（ReporterService）
//   - 调度器：核心调度循环（抢占任务→创建执行→分发到执行节点）
//   - Governor：健康检查 + pprof 性能诊断端口
//   - 后台长任务：4 个补偿器 + 2 个 MQ 消费者
package main

import (
	"context"

	"gitee.com/flycash/distributed_task_platform/cmd/scheduler/ioc"
	"github.com/gotomicro/ego"
	"github.com/gotomicro/ego/core/elog"
	"github.com/gotomicro/ego/server"
	"github.com/gotomicro/ego/server/egovernor"
)

func main() {
	// 创建 ego 应用实例，ego 负责配置加载、日志初始化、优雅停机等
	egoApp := ego.New()

	// 创建可取消的上下文，用于控制后台长任务的生命周期
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 通过 Wire 依赖注入初始化整个应用（DB→DAO→Repository→Service→Scheduler）
	app := ioc.InitSchedulerApp()

	// 启动所有后台长任务（补偿器 + MQ 消费者），每个任务在独立 goroutine 中运行
	app.StartTasks(ctx)

	// 启动 ego 服务，注册 3 个 Server 组件：
	// 1. Governor: 监控端口（默认 9003），提供 /debug/health、/debug/pprof 等端点
	// 2. GRPC: gRPC 服务端（默认 9002），处理执行节点的状态上报
	// 3. Scheduler: 调度器本身也是一个 ego Server，通过 Serve 方法启动调度循环
	if err := egoApp.Serve(
		egovernor.Load("server.governor").Build(),
		func() server.Server {
			return app.GRPC
		}(),
		func() server.Server {
			return app.Scheduler
		}(),
	).Cron().
		Run(); err != nil {
		elog.Panic("startup", elog.FieldErr(err))
	}
}
