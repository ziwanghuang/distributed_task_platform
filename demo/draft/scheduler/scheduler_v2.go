package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ============================================================================
// 基于纵向划分的调度器设计
// ============================================================================

// JobScheduler 调度器主控制器
type JobScheduler struct {
	nodeID string

	// 依赖服务
	jobRepo       JobRepository
	executionRepo JobExecutionRepository

	// 控制
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// 运行时状态
	runningExecutors sync.Map // map[int64]*JobExecutor，正在运行的Job执行器

	// 配置
	scanInterval time.Duration // 扫描间隔
	batchSize    int           // 批量处理大小
}

func NewJobScheduler(nodeID string) *JobScheduler {
	ctx, cancel := context.WithCancel(context.Background())

	return &JobScheduler{
		nodeID:       nodeID,
		ctx:          ctx,
		cancel:       cancel,
		scanInterval: 10 * time.Second,
		batchSize:    10,
	}
}

// SetRepositories 设置依赖的数据访问层
func (s *JobScheduler) SetRepositories(jobRepo JobRepository, executionRepo JobExecutionRepository) {
	s.jobRepo = jobRepo
	s.executionRepo = executionRepo
}

// Start 启动调度器
func (s *JobScheduler) Start() error {
	fmt.Printf("🚀 启动调度器 (NodeID: %s)\n", s.nodeID)

	// 启动主调度循环
	s.wg.Add(1)
	go s.mainScheduleLoop(&s.wg)

	fmt.Println("✅ 调度器已启动")
	return nil
}

// Stop 停止调度器
func (s *JobScheduler) Stop() error {
	fmt.Println("🛑 开始停止调度器...")

	// 1. 取消主循环
	s.cancel()

	// 2. 停止所有正在运行的JobExecutor
	s.runningExecutors.Range(func(key, value interface{}) bool {
		jobID := key.(int64)
		executor := value.(*JobExecutor)

		fmt.Printf("停止JobExecutor (JobID: %d)\n", jobID)
		if err := executor.Stop(); err != nil {
			fmt.Printf("停止JobExecutor失败: %v\n", err)
		}
		return true
	})

	// 3. 等待所有协程结束
	s.wg.Wait()

	fmt.Println("✅ 调度器已停止")
	return nil
}

// ============================================================================
// 主调度循环：发现任务 -> 创建JobExecutor -> 异步执行
// ============================================================================

func (s *JobScheduler) mainScheduleLoop(wg *sync.WaitGroup) {
	defer wg.Done()

	ticker := time.NewTicker(s.scanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			fmt.Println("主调度循环收到停止信号")
			return

		case <-ticker.C:
			s.scheduleJobs()
		}
	}
}

func (s *JobScheduler) scheduleJobs() {
	fmt.Println("=== 开始调度周期 ===")

	// 1. 获取可调度的Jobs
	jobs, err := s.getSchedulableJobs()
	if err != nil {
		fmt.Printf("获取可调度任务失败: %v\n", err)
		return
	}

	fmt.Printf("发现 %d 个可调度任务\n", len(jobs))

	// 2. 为每个Job创建JobExecutor并异步执行
	for _, job := range jobs {
		// 检查是否已经在执行中
		if _, exists := s.runningExecutors.Load(job.ID); exists {
			fmt.Printf("Job %d 已在执行中，跳过\n", job.ID)
			continue
		}

		// 创建JobExecutor
		executor := NewJobExecutor(job, s.nodeID)
		executor.jobRepo = s.jobRepo
		executor.executionRepo = s.executionRepo

		// 加入运行列表
		s.runningExecutors.Store(job.ID, executor)

		// 异步执行Job的完整生命周期
		go s.executeJobAsync(job.ID, executor)
	}

	// 3. 清理已完成的JobExecutor
	s.cleanupCompletedExecutors()
}

// ============================================================================
// 异步执行单个Job的完整生命周期
// ============================================================================

func (s *JobScheduler) executeJobAsync(jobID int64, executor *JobExecutor) {
	fmt.Printf("🎯 开始执行Job %d\n", jobID)

	// 确保从运行列表中移除
	defer func() {
		s.runningExecutors.Delete(jobID)
		fmt.Printf("✅ Job %d 执行完成，已从运行列表移除\n", jobID)
	}()

	// 执行Job的完整生命周期
	err := executor.Execute()
	if err != nil {
		fmt.Printf("❌ Job %d 执行失败: %v\n", jobID, err)
	} else {
		fmt.Printf("✅ Job %d 执行成功\n", jobID)
	}
}

// getSchedulableJobs 获取可调度的Jobs
func (s *JobScheduler) getSchedulableJobs() ([]*Job, error) {
	// 查询条件：
	// 1. status = 'ACTIVE'
	// 2. next_time <= NOW()
	// 3. 按优先级排序，限制数量

	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()

	return s.jobRepo.GetSchedulableJobs(ctx, s.batchSize)
}

// cleanupCompletedExecutors 清理已完成的JobExecutor
func (s *JobScheduler) cleanupCompletedExecutors() {
	// 这个方法在当前设计中可能不需要，因为executeJobAsync会自动清理
	// 但可以用于处理异常情况，比如超时的executor
}

// ============================================================================
// 监控和统计接口
// ============================================================================

// GetRunningJobsCount 获取正在运行的Job数量
func (s *JobScheduler) GetRunningJobsCount() int {
	count := 0
	s.runningExecutors.Range(func(key, value interface{}) bool {
		count++
		return true
	})
	return count
}

// GetRunningJobIDs 获取正在运行的Job ID列表
func (s *JobScheduler) GetRunningJobIDs() []int64 {
	var jobIDs []int64
	s.runningExecutors.Range(func(key, value interface{}) bool {
		jobID := key.(int64)
		jobIDs = append(jobIDs, jobID)
		return true
	})
	return jobIDs
}

// GetJobExecutorStatus 获取特定Job的执行状态
func (s *JobScheduler) GetJobExecutorStatus(jobID int64) (*JobExecutionStatus, error) {
	if executorInterface, exists := s.runningExecutors.Load(jobID); exists {
		executor := executorInterface.(*JobExecutor)
		if executor.currentExecution != nil {
			return &executor.currentExecution.Status, nil
		}
	}
	return nil, fmt.Errorf("Job %d 不在运行中", jobID)
}
