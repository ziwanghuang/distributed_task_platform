package main

import (
	"context"
	"fmt"
	"log"
	"time"

	executorv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/executor/v1"
	"gitee.com/flycash/distributed_task_platform/demo/draft/scheduler"
	"google.golang.org/grpc"
)

// 分布式调度器：本机抢占，异地执行
// 调度中心负责抢占任务，远程执行节点负责实际执行
type DistributedScheduler struct {
	nodeID          string
	taskRepo        scheduler.TaskRepository
	executionRepo   scheduler.ExecutionRepository
	executorClients map[string]executorv1.ExecutorServiceClient // gRPC客户端池
	loadBalancer    scheduler.LoadBalancer
}

func NewDistributedScheduler(nodeID string) *DistributedScheduler {
	return &DistributedScheduler{
		nodeID:          nodeID,
		executorClients: make(map[string]executorv1.ExecutorServiceClient),
	}
}

// 注册执行节点的gRPC客户端
func (ds *DistributedScheduler) RegisterExecutorNode(nodeID, address string) error {
	conn, err := grpc.Dial(address, grpc.WithInsecure())
	if err != nil {
		return fmt.Errorf("连接执行节点失败 %s: %w", address, err)
	}

	client := executorv1.NewExecutorServiceClient(conn)
	ds.executorClients[nodeID] = client

	log.Printf("注册执行节点: %s -> %s", nodeID, address)
	return nil
}

func (ds *DistributedScheduler) Run(ctx context.Context) error {
	log.Printf("分布式调度器启动，节点ID: %s", ds.nodeID)

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("调度器收到停止信号")
			return ctx.Err()
		case <-ticker.C:
			ds.scheduleLoop(ctx)
		}
	}
}

func (ds *DistributedScheduler) scheduleLoop(ctx context.Context) {
	log.Println("=== 开始分布式调度循环 ===")

	// 1. 调度中心抢占任务（本机抢占）
	tasks, err := ds.getSchedulableTasks(ctx)
	if err != nil {
		log.Printf("获取可调度任务失败: %v", err)
		return
	}

	log.Printf("发现 %d 个可调度任务", len(tasks))

	// 2. 对每个任务进行抢占和远程执行
	for _, task := range tasks {
		if err := ds.preemptAndScheduleTask(ctx, task); err != nil {
			log.Printf("处理任务 %s 失败: %v", task.Name, err)
		}
	}
}

// 抢占任务（在调度中心执行）
func (ds *DistributedScheduler) preemptTask(ctx context.Context, task *scheduler.Task) error {
	log.Printf("调度中心抢占任务: %s (ID: %d, Version: %d)", task.Name, task.ID, task.Version)

	// 抢占操作：CAS更新task表
	// UPDATE task SET status = 'PREEMPTED', schedule_node = 'scheduler-001', version = version + 1
	// WHERE id = ? AND version = ? AND status = 'ACTIVE'
	err := ds.taskRepo.PreemptTask(ctx, task.ID, ds.nodeID, task.Version)
	if err != nil {
		log.Printf("抢占任务失败: %v", err)
		return err
	}

	log.Printf("✅ 调度中心成功抢占任务: %s", task.Name)
	return nil
}

// 调度任务到远程执行节点（异地执行）
func (ds *DistributedScheduler) scheduleTaskToRemoteNode(ctx context.Context, task *scheduler.Task) error {
	log.Printf("开始调度任务到远程节点: %s", task.Name)

	// 1. 创建execution记录
	execution := &scheduler.Execution{
		TaskID:     task.ID,
		TaskName:   task.Name,
		Status:     scheduler.ExecutionStatusPrepare,
		Params:     task.Params,
		CreateTime: time.Now(),
		UpdateTime: time.Now(),
	}

	err := ds.executionRepo.Create(ctx, execution)
	if err != nil {
		return fmt.Errorf("创建执行记录失败: %w", err)
	}

	log.Printf("创建执行记录，ID: %d", execution.ID)

	// 2. 选择执行节点（负载均衡）
	executorNode, err := ds.loadBalancer.SelectNode(ctx, task)
	if err != nil {
		return fmt.Errorf("选择执行节点失败: %w", err)
	}

	log.Printf("选择执行节点: %s", executorNode)

	// 3. 更新执行记录的执行节点
	execution.ExecutorNode = executorNode
	err = ds.executionRepo.UpdateStatus(ctx, execution.ID, scheduler.ExecutionStatusRunning)
	if err != nil {
		return fmt.Errorf("更新执行状态失败: %w", err)
	}

	// 4. 获取执行节点的gRPC客户端
	executorClient, exists := ds.executorClients[executorNode]
	if !exists {
		return fmt.Errorf("执行节点 %s 的gRPC客户端不存在", executorNode)
	}

	// 5. 调用远程执行节点执行任务（这是关键的gRPC调用）
	log.Printf("调用远程执行节点 %s 执行任务...", executorNode)

	executeReq := &executorv1.ExecuteRequest{
		Eid:      execution.ID,
		TaskName: task.Name,
		Params:   convertParams(task.Params),
	}

	executeResp, err := executorClient.Execute(ctx, executeReq)
	if err != nil {
		// gRPC调用失败
		ds.executionRepo.UpdateStatus(ctx, execution.ID, scheduler.ExecutionStatusFailed)
		return fmt.Errorf("远程执行任务失败: %w", err)
	}

	// 6. 处理执行结果
	executionState := executeResp.ExecutionState
	finalStatus := convertProtoStatus(executionState.Status)

	err = ds.executionRepo.UpdateStatus(ctx, execution.ID, finalStatus)
	if err != nil {
		return fmt.Errorf("更新执行结果失败: %w", err)
	}

	// 7. 如果执行成功，释放任务锁
	if finalStatus == scheduler.ExecutionStatusSuccess {
		nextTime := ds.calculateNextTime(task.CronExpr)
		err = ds.taskRepo.ReleaseTask(ctx, task.ID, nextTime, task.Version+1)
		if err != nil {
			log.Printf("释放任务锁失败: %v", err)
		} else {
			log.Printf("✅ 任务在远程节点 %s 执行成功，下次执行时间: %v", executorNode, nextTime)
		}
	}

	return nil
}

func (ds *DistributedScheduler) preemptAndScheduleTask(ctx context.Context, task *scheduler.Task) error {
	// 1. 调度中心抢占任务
	if err := ds.preemptTask(ctx, task); err != nil {
		return err
	}

	// 2. 调度任务到远程执行节点
	if err := ds.scheduleTaskToRemoteNode(ctx, task); err != nil {
		return err
	}

	return nil
}

func (ds *DistributedScheduler) getSchedulableTasks(ctx context.Context) ([]*scheduler.Task, error) {
	// 实现与本地调度器相同的逻辑
	normalTasks, err := ds.taskRepo.GetSchedulableTasks(ctx, 10)
	if err != nil {
		return nil, err
	}

	timeoutTasks, err := ds.taskRepo.GetTimeoutPreemptedTasks(ctx, 10)
	if err != nil {
		return nil, err
	}

	return append(normalTasks, timeoutTasks...), nil
}

func (ds *DistributedScheduler) calculateNextTime(cronExpr string) time.Time {
	// 实现cron表达式解析
	return time.Now().Add(30 * time.Minute)
}

// 辅助函数：转换参数格式
func convertParams(params map[string]string) map[string]string {
	if params == nil {
		return make(map[string]string)
	}
	return params
}

// 辅助函数：转换proto状态到内部状态
func convertProtoStatus(status int32) scheduler.ExecutionStatus {
	switch status {
	case 0: // PREPARE
		return scheduler.ExecutionStatusPrepare
	case 1: // RUNNING
		return scheduler.ExecutionStatusRunning
	case 2: // RETRYABLE_FAILED
		return scheduler.ExecutionStatusRetryableFailed
	case 3: // FAILED
		return scheduler.ExecutionStatusFailed
	case 4: // SUCCESS
		return scheduler.ExecutionStatusSuccess
	default:
		return scheduler.ExecutionStatusFailed
	}
}

func main() {
	ctx := context.Background()

	// 创建分布式调度器
	distributedScheduler := NewDistributedScheduler("distributed-scheduler-001")

	// 注册执行节点
	distributedScheduler.RegisterExecutorNode("executor-001", "executor-node-1:8080")
	distributedScheduler.RegisterExecutorNode("executor-002", "executor-node-2:8080")
	distributedScheduler.RegisterExecutorNode("executor-003", "executor-node-3:8080")

	log.Println("启动分布式调度器...")
	if err := distributedScheduler.Run(ctx); err != nil {
		log.Fatalf("分布式调度器运行失败: %v", err)
	}
}
