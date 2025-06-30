package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"gitee.com/flycash/distributed_task_platform/demo/draft/scheduler"
)

func main() {
	// ============================================================================
	// 纵向划分调度器示例
	// ============================================================================

	fmt.Println("🚀 启动基于纵向划分的任务调度器...")

	// 1. 创建JobScheduler
	nodeID := "local-node-001"
	jobScheduler := scheduler.NewJobScheduler(nodeID)

	// 2. 初始化依赖（实际项目中会注入真实的Repository实现）
	// jobRepo := repository.NewJobRepository(db)
	// executionRepo := repository.NewJobExecutionRepository(db)
	// jobScheduler.SetRepositories(jobRepo, executionRepo)

	// 3. 启动调度器
	if err := jobScheduler.Start(); err != nil {
		log.Fatalf("启动调度器失败: %v", err)
	}

	// 4. 打印架构说明
	printArchitectureInfo()

	// 5. 等待中断信号
	waitForShutdown(jobScheduler)
}

func printArchitectureInfo() {
	fmt.Println()
	fmt.Println("📋 === 纵向划分架构说明 ===")
	fmt.Println("🔹 JobScheduler: 主调度器，负责发现任务并创建JobExecutor")
	fmt.Println("🔹 JobExecutor: 单个Job的完整生命周期管理器")
	fmt.Println("   ├── 抢占Job (ACTIVE -> PREEMPTED)")
	fmt.Println("   ├── 启动续约协程 (防止超时)")
	fmt.Println("   ├── 创建JobExecution记录")
	fmt.Println("   ├── 调用执行器 (gRPC/HTTP)")
	fmt.Println("   ├── 启动监控协程 (轮询状态)")
	fmt.Println("   ├── 等待执行完成")
	fmt.Println("   └── 释放Job锁 (PREEMPTED -> ACTIVE)")
	fmt.Println()
	fmt.Println("✨ 优势：高内聚、低耦合、易于理解和维护")
	fmt.Println("🔄 开始轮询可调度任务...")
	fmt.Println()
}

func waitForShutdown(jobScheduler *scheduler.JobScheduler) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 启动状态监控协程
	go func() {
		for {
			select {
			case <-sigChan:
				return
			default:
				fmt.Printf("📊 当前运行中的Job数量: %d, Job IDs: %v\n",
					jobScheduler.GetRunningJobsCount(),
					jobScheduler.GetRunningJobIDs())
				// 等待一段时间再次检查
				select {
				case <-sigChan:
					return
				default:
					// 这里可以添加适当的延时，比如time.Sleep(30 * time.Second)
				}
			}
		}
	}()

	sig := <-sigChan
	fmt.Printf("\n收到信号 %v，开始优雅停止...\n", sig)

	// 优雅停止调度器
	if err := jobScheduler.Stop(); err != nil {
		log.Printf("停止调度器时出错: %v", err)
	}

	fmt.Println("✅ 调度器已停止")
}
