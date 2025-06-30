package scheduler

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ============================================================================
// 事件循环架构设计
// ============================================================================

// SchedulerEventLoop 主调度事件循环
type SchedulerEventLoop struct {
	nodeID          string
	jobRepo         JobRepository
	executionRepo   JobExecutionRepository
	taskExecutor    *TaskExecutor
	renewalManager  *RenewalManager
	progressMonitor *ProgressMonitor
	resultCollector *ResultCollector

	// 控制
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// 配置
	scanInterval time.Duration // 扫描间隔
	batchSize    int           // 批量处理大小
}

func NewSchedulerEventLoop(nodeID string) *SchedulerEventLoop {
	ctx, cancel := context.WithCancel(context.Background())

	loop := &SchedulerEventLoop{
		nodeID:       nodeID,
		ctx:          ctx,
		cancel:       cancel,
		scanInterval: 10 * time.Second,
		batchSize:    10,
	}

	// 初始化组件
	loop.taskExecutor = NewTaskExecutor(loop)
	loop.renewalManager = NewRenewalManager(loop)
	loop.progressMonitor = NewProgressMonitor(loop)
	loop.resultCollector = NewResultCollector(loop)

	return loop
}

// Start 启动事件循环
func (sel *SchedulerEventLoop) Start() error {
	// TODO: 启动主事件循环

	// 启动续约管理器
	sel.wg.Add(1)
	go sel.renewalManager.Start(sel.ctx, &sel.wg)

	// 启动进度监控器
	sel.wg.Add(1)
	go sel.progressMonitor.Start(sel.ctx, &sel.wg)

	// 启动结果收集器
	sel.wg.Add(1)
	go sel.resultCollector.Start(sel.ctx, &sel.wg)

	// 启动主调度循环
	sel.wg.Add(1)
	go sel.mainLoop(&sel.wg)

	return nil
}

// Stop 停止事件循环
func (sel *SchedulerEventLoop) Stop() error {
	// TODO: 优雅停止
	sel.cancel()
	sel.wg.Wait()
	return nil
}

// mainLoop 主事件循环
func (sel *SchedulerEventLoop) mainLoop(wg *sync.WaitGroup) {
	defer wg.Done()

	ticker := time.NewTicker(sel.scanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-sel.ctx.Done():
			return
		case <-ticker.C:
			sel.scanAndProcessJobs()
		}
	}
}

// scanAndProcessJobs 扫描并处理任务
func (sel *SchedulerEventLoop) scanAndProcessJobs() {
	// TODO: 1. 获取可调度的任务
	jobs := sel.getSchedulableJobs()

	// TODO: 2. 对每个任务尝试抢占和处理
	for _, job := range jobs {
		// 为每个任务启动独立协程处理
		go sel.processJobAsync(job)
	}
}

// processJobAsync 在 event_loop_core.go 中实现

// getSchedulableJobs 获取可调度的任务
func (sel *SchedulerEventLoop) getSchedulableJobs() []*Job {
	// TODO: 查询可调度任务
	// 1. 正常到期的任务
	// 2. 超时需要补偿的任务
	return nil
}

// ============================================================================
// TaskExecutor 任务执行协调器
// ============================================================================

type TaskExecutor struct {
	loop         *SchedulerEventLoop
	grpcExecutor *GRPCExecutor
	httpExecutor *HTTPExecutor
}

func NewTaskExecutor(loop *SchedulerEventLoop) *TaskExecutor {
	return &TaskExecutor{
		loop:         loop,
		grpcExecutor: &GRPCExecutor{},
		httpExecutor: &HTTPExecutor{},
	}
}

// PreemptJob 抢占任务
func (te *TaskExecutor) PreemptJob(job *Job) error {
	// TODO: 乐观锁抢占
	// UPDATE job SET status='PREEMPTED', schedule_node=?, version=version+1
	// WHERE id=? AND version=? AND status='ACTIVE'
	return nil
}

// CreateExecution 创建执行记录
func (te *TaskExecutor) CreateExecution(job *Job) (*JobExecution, error) {
	// TODO: 创建JobExecution记录，状态为PREPARE
	return nil, nil
}

// ExecuteJob 执行任务
func (te *TaskExecutor) ExecuteJob(job *Job, execution *JobExecution) error {
	// TODO: 根据job配置选择执行方式
	if job.GRPCConfig != nil {
		return te.grpcExecutor.Execute(job, execution)
	}

	if job.HTTPConfig != nil {
		return te.httpExecutor.Execute(job, execution)
	}

	return ErrNoExecutorConfigured
}

// ReleaseJob 释放任务锁
func (te *TaskExecutor) ReleaseJob(job *Job) error {
	// TODO: 计算下次执行时间，释放锁
	// nextTime := cronParser.Next(job.CronExpr)
	// UPDATE job SET status='ACTIVE', schedule_node=NULL, next_time=?
	// WHERE id=? AND schedule_node=?
	return nil
}

// ============================================================================
// RenewalManager 续约管理器
// ============================================================================

type RenewalManager struct {
	loop            *SchedulerEventLoop
	preemptedJobs   sync.Map // 正在处理的任务
	renewalInterval time.Duration
}

func NewRenewalManager(loop *SchedulerEventLoop) *RenewalManager {
	return &RenewalManager{
		loop:            loop,
		renewalInterval: 5 * time.Minute, // 每5分钟续约一次
	}
}

// Start 启动续约管理器
func (rm *RenewalManager) Start(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	ticker := time.NewTicker(rm.renewalInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rm.renewAllJobs()
		}
	}
}

// AddJob 添加需要续约的任务
func (rm *RenewalManager) AddJob(jobID int64) {
	// TODO: 添加到续约列表
	rm.preemptedJobs.Store(jobID, time.Now())
}

// RemoveJob 移除不需要续约的任务
func (rm *RenewalManager) RemoveJob(jobID int64) {
	// TODO: 从续约列表移除
	rm.preemptedJobs.Delete(jobID)
}

// renewAllJobs 续约所有任务
func (rm *RenewalManager) renewAllJobs() {
	// TODO: 遍历所有需要续约的任务，更新utime
	rm.preemptedJobs.Range(func(key, value interface{}) bool {
		jobID := key.(int64)
		rm.renewSingleJob(jobID)
		return true
	})
}

// renewSingleJob 续约单个任务
func (rm *RenewalManager) renewSingleJob(jobID int64) error {
	// TODO: UPDATE job SET utime=NOW() WHERE id=? AND schedule_node=?
	return nil
}

// ============================================================================
// ProgressMonitor 进度监控器
// ============================================================================

type ProgressMonitor struct {
	loop              *SchedulerEventLoop
	runningExecutions sync.Map // 正在监控的执行
	pollInterval      time.Duration
}

func NewProgressMonitor(loop *SchedulerEventLoop) *ProgressMonitor {
	return &ProgressMonitor{
		loop:         loop,
		pollInterval: 30 * time.Second, // 每30秒轮询一次
	}
}

// Start 启动进度监控器
func (pm *ProgressMonitor) Start(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	ticker := time.NewTicker(pm.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pm.pollAllExecutions()
		}
	}
}

// AddExecution 添加需要监控的执行
func (pm *ProgressMonitor) AddExecution(execution *JobExecution) {
	// TODO: 添加到监控列表
	pm.runningExecutions.Store(execution.ID, execution)
}

// RemoveExecution 移除不需要监控的执行
func (pm *ProgressMonitor) RemoveExecution(executionID int64) {
	// TODO: 从监控列表移除
	pm.runningExecutions.Delete(executionID)
}

// pollAllExecutions 轮询所有执行状态
func (pm *ProgressMonitor) pollAllExecutions() {
	// TODO: 遍历所有正在执行的任务，查询状态
	pm.runningExecutions.Range(func(key, value interface{}) bool {
		execution := value.(*JobExecution)
		pm.pollSingleExecution(execution)
		return true
	})
}

// pollSingleExecution 轮询单个执行状态
func (pm *ProgressMonitor) pollSingleExecution(execution *JobExecution) {
	// TODO: 调用ExecutorService.Query查询状态
	// 或者等待进度上报消息
}

// ============================================================================
// ResultCollector 结果收集器
// ============================================================================

type ResultCollector struct {
	loop           *SchedulerEventLoop
	completedQueue chan *JobExecution // 完成的执行队列
}

func NewResultCollector(loop *SchedulerEventLoop) *ResultCollector {
	return &ResultCollector{
		loop:           loop,
		completedQueue: make(chan *JobExecution, 100),
	}
}

// Start 启动结果收集器
func (rc *ResultCollector) Start(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case execution := <-rc.completedQueue:
			rc.handleCompletedExecution(execution)
		}
	}
}

// NotifyCompleted 通知执行完成
func (rc *ResultCollector) NotifyCompleted(execution *JobExecution) {
	// TODO: 非阻塞发送到完成队列
	select {
	case rc.completedQueue <- execution:
	default:
		// 队列满了，记录日志或丢弃
	}
}

// handleCompletedExecution 处理完成的执行
func (rc *ResultCollector) handleCompletedExecution(execution *JobExecution) {
	// TODO: 1. 更新execution状态
	// TODO: 2. 从监控列表移除
	// TODO: 3. 从续约列表移除
	// TODO: 4. 释放job锁
}

// ============================================================================
// 执行器实现
// ============================================================================

type GRPCExecutor struct{}

func (ge *GRPCExecutor) Execute(job *Job, execution *JobExecution) error {
	// TODO: 调用gRPC ExecutorService.Run
	return nil
}

type HTTPExecutor struct{}

func (he *HTTPExecutor) Execute(job *Job, execution *JobExecution) error {
	// TODO: 调用HTTP端点
	return nil
}

// ============================================================================
// 错误定义
// ============================================================================

var ErrNoExecutorConfigured = errors.New("no executor configured")

// ============================================================================
// 接口定义（引用已有的）
// ============================================================================

type JobRepository interface {
	GetSchedulableJobs(ctx context.Context, limit int) ([]*Job, error)
	PreemptJob(ctx context.Context, jobID int64, scheduleNode string, version int64) error
	RenewJob(ctx context.Context, jobID int64, scheduleNode string) error
	ReleaseJob(ctx context.Context, jobID int64, nextTime time.Time, scheduleNode string) error
}

type JobExecutionRepository interface {
	Create(ctx context.Context, execution *JobExecution) error
	UpdateStatus(ctx context.Context, executionID int64, status JobExecutionStatus) error
	UpdateProgress(ctx context.Context, executionID int64, progress int32) error
}
