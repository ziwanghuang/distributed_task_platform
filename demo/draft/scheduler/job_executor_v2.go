package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ============================================================================
// 纵向划分设计：JobExecutor 管理单个Job的完整生命周期
// ============================================================================

// JobExecutor 单个Job的完整生命周期管理器
// 职责：抢占 -> 续约 -> 执行 -> 监控 -> 收集结果 -> 释放
type JobExecutor struct {
	// 基础信息
	job    *Job
	nodeID string

	// 依赖服务
	jobRepo       JobRepository
	executionRepo JobExecutionRepository
	grpcExecutor  *GRPCExecutor
	httpExecutor  *HTTPExecutor

	// 生命周期控制
	ctx              context.Context
	cancel           context.CancelFunc
	wg               sync.WaitGroup
	currentExecution *JobExecution

	// 配置
	renewalInterval time.Duration // 续约间隔
	monitorInterval time.Duration // 监控间隔

	// 状态
	isPreempted bool      // 是否已抢占
	startTime   time.Time // 开始时间
}

func NewJobExecutor(job *Job, nodeID string) *JobExecutor {
	ctx, cancel := context.WithCancel(context.Background())

	return &JobExecutor{
		job:             job,
		nodeID:          nodeID,
		ctx:             ctx,
		cancel:          cancel,
		renewalInterval: 5 * time.Minute,  // 5分钟续约一次
		monitorInterval: 30 * time.Second, // 30秒监控一次
		isPreempted:     false,
	}
}

// ============================================================================
// 主执行流程：完整的Job生命周期
// ============================================================================

// Execute 执行Job的完整生命周期
func (je *JobExecutor) Execute() error {
	je.startTime = time.Now()

	// 阶段1: 抢占Job
	if err := je.preemptJob(); err != nil {
		return fmt.Errorf("抢占Job失败: %w", err)
	}

	// 确保最后释放锁
	defer je.releaseJob()

	// 阶段2: 启动续约协程
	je.wg.Add(1)
	go je.renewalLoop(&je.wg)

	// 阶段3: 创建并执行JobExecution
	if err := je.executeJobExecution(); err != nil {
		return fmt.Errorf("执行JobExecution失败: %w", err)
	}

	return nil
}

// Stop 停止JobExecutor
func (je *JobExecutor) Stop() error {
	je.cancel()
	je.wg.Wait()
	return nil
}

// ============================================================================
// 阶段1: 抢占Job (ACTIVE -> PREEMPTED)
// ============================================================================

func (je *JobExecutor) preemptJob() error {
	err := je.jobRepo.PreemptJob(je.ctx, je.job.ID, je.nodeID, je.job.Version)
	if err != nil {
		return err
	}

	je.isPreempted = true
	je.job.Status = JobStatusPreempted
	je.job.ScheduleNode = je.nodeID
	je.job.Version++

	return nil
}

// ============================================================================
// 阶段2: 续约管理（防止超时被重新调度）
// ============================================================================

func (je *JobExecutor) renewalLoop(wg *sync.WaitGroup) {
	defer wg.Done()

	ticker := time.NewTicker(je.renewalInterval)
	defer ticker.Stop()

	for {
		select {
		case <-je.ctx.Done():
			return
		case <-ticker.C:
			if err := je.renewJob(); err != nil {
				// TODO: 记录日志，续约失败可能导致任务被其他节点重新调度
				fmt.Printf("续约失败: %v\n", err)
			}
		}
	}
}

func (je *JobExecutor) renewJob() error {
	if !je.isPreempted {
		return nil // 未抢占状态不需要续约
	}

	return je.jobRepo.RenewJob(je.ctx, je.job.ID, je.nodeID)
}

// ============================================================================
// 阶段3: 执行JobExecution的完整生命周期
// ============================================================================

func (je *JobExecutor) executeJobExecution() error {
	// 3.1 创建JobExecution记录
	execution, err := je.createJobExecution()
	if err != nil {
		return err
	}
	je.currentExecution = execution

	// 3.2 启动进度监控协程
	je.wg.Add(1)
	go je.monitorLoop(&je.wg)

	// 3.3 调用执行器
	if err := je.callExecutor(); err != nil {
		return err
	}

	// 3.4 等待执行完成
	return je.waitForCompletion()
}

// createJobExecution 创建JobExecution记录
func (je *JobExecutor) createJobExecution() (*JobExecution, error) {
	execution := &JobExecution{
		JobID:        je.job.ID,
		JobName:      je.job.Name,
		Status:       JobExecutionStatusPrepare,
		StartTime:    &je.startTime,
		Progress:     0,
		ExecutorNode: je.nodeID,
		Params:       je.job.Params,
		TraceID:      generateTraceID(), // TODO: 实现
		CreateTime:   time.Now(),
		UpdateTime:   time.Now(),
	}

	err := je.executionRepo.Create(je.ctx, execution)
	if err != nil {
		return nil, err
	}

	return execution, nil
}

// ============================================================================
// 阶段4: 进度监控（轮询执行状态）
// ============================================================================

func (je *JobExecutor) monitorLoop(wg *sync.WaitGroup) {
	defer wg.Done()

	ticker := time.NewTicker(je.monitorInterval)
	defer ticker.Stop()

	for {
		select {
		case <-je.ctx.Done():
			return
		case <-ticker.C:
			if err := je.monitorProgress(); err != nil {
				fmt.Printf("监控进度失败: %v\n", err)
			}

			// 检查是否已完成
			if je.currentExecution != nil && je.isExecutionTerminal(je.currentExecution.Status) {
				return
			}
		}
	}
}

func (je *JobExecutor) monitorProgress() error {
	if je.currentExecution == nil {
		return nil
	}

	// 根据Job配置选择查询方式
	var status JobExecutionStatus
	var progress int32
	var err error

	if je.job.GRPCConfig != nil {
		status, progress, err = je.grpcExecutor.Query(je.job, je.currentExecution)
	} else if je.job.HTTPConfig != nil {
		status, progress, err = je.httpExecutor.Query(je.job, je.currentExecution)
	} else {
		return fmt.Errorf("未配置执行器")
	}

	if err != nil {
		return err
	}

	// 更新状态和进度
	if status != je.currentExecution.Status {
		je.executionRepo.UpdateStatus(je.ctx, je.currentExecution.ID, status)
		je.currentExecution.Status = status
	}

	if progress != je.currentExecution.Progress {
		je.executionRepo.UpdateProgress(je.ctx, je.currentExecution.ID, progress)
		je.currentExecution.Progress = progress
	}

	return nil
}

// ============================================================================
// 阶段5: 调用执行器
// ============================================================================

func (je *JobExecutor) callExecutor() error {
	if je.currentExecution == nil {
		return fmt.Errorf("execution不存在")
	}

	// 更新状态为RUNNING
	err := je.executionRepo.UpdateStatus(je.ctx, je.currentExecution.ID, JobExecutionStatusRunning)
	if err != nil {
		return err
	}
	je.currentExecution.Status = JobExecutionStatusRunning

	// 异步调用执行器
	go func() {
		defer func() {
			if r := recover(); r != nil {
				// 处理panic
				je.handleExecutionPanic(r)
			}
		}()

		var err error
		if je.job.GRPCConfig != nil {
			err = je.grpcExecutor.Execute(je.job, je.currentExecution)
		} else if je.job.HTTPConfig != nil {
			err = je.httpExecutor.Execute(je.job, je.currentExecution)
		} else {
			err = fmt.Errorf("未配置执行器")
		}

		if err != nil {
			je.handleExecutionError(err)
		}
	}()

	return nil
}

// ============================================================================
// 阶段6: 等待完成和结果处理
// ============================================================================

func (je *JobExecutor) waitForCompletion() error {
	// 设置超时
	timeout := time.Duration(je.job.Timeout) * time.Second
	ctx, cancel := context.WithTimeout(je.ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// 超时处理
			return je.handleTimeout()

		case <-ticker.C:
			if je.currentExecution != nil && je.isExecutionTerminal(je.currentExecution.Status) {
				// 执行完成
				return je.handleCompletion()
			}
		}
	}
}

func (je *JobExecutor) handleCompletion() error {
	if je.currentExecution == nil {
		return nil
	}

	// 更新结束时间
	now := time.Now()
	je.currentExecution.EndTime = &now
	je.currentExecution.Duration = now.Sub(je.startTime).Milliseconds()

	// TODO: 根据执行结果决定是否需要重试
	// TODO: 记录执行日志
	// TODO: 发送通知

	return nil
}

func (je *JobExecutor) handleTimeout() error {
	if je.currentExecution == nil {
		return nil
	}

	// 尝试取消执行
	if je.job.GRPCConfig != nil {
		je.grpcExecutor.Cancel(je.job, je.currentExecution)
	} else if je.job.HTTPConfig != nil {
		je.httpExecutor.Cancel(je.job, je.currentExecution)
	}

	// 更新状态为超时
	je.executionRepo.UpdateStatus(je.ctx, je.currentExecution.ID, JobExecutionStatusTimeout)
	je.currentExecution.Status = JobExecutionStatusTimeout

	return fmt.Errorf("执行超时")
}

func (je *JobExecutor) handleExecutionError(err error) {
	if je.currentExecution == nil {
		return
	}

	je.executionRepo.UpdateStatus(je.ctx, je.currentExecution.ID, JobExecutionStatusFailed)
	je.currentExecution.Status = JobExecutionStatusFailed
	je.currentExecution.ErrorMessage = err.Error()
}

func (je *JobExecutor) handleExecutionPanic(r interface{}) {
	if je.currentExecution == nil {
		return
	}

	je.executionRepo.UpdateStatus(je.ctx, je.currentExecution.ID, JobExecutionStatusFailed)
	je.currentExecution.Status = JobExecutionStatusFailed
	je.currentExecution.ErrorMessage = fmt.Sprintf("执行器panic: %v", r)
}

// ============================================================================
// 阶段7: 释放Job锁 (PREEMPTED -> ACTIVE)
// ============================================================================

func (je *JobExecutor) releaseJob() error {
	if !je.isPreempted {
		return nil
	}

	// 计算下次执行时间
	nextTime := je.calculateNextTime()

	// 释放锁
	err := je.jobRepo.ReleaseJob(je.ctx, je.job.ID, nextTime, je.nodeID)
	if err != nil {
		return err
	}

	je.isPreempted = false
	je.job.Status = JobStatusActive
	je.job.ScheduleNode = ""
	je.job.NextTime = nextTime

	return nil
}

func (je *JobExecutor) calculateNextTime() time.Time {
	// TODO: 根据cron表达式计算下次执行时间
	return time.Now().Add(24 * time.Hour) // 简单示例：明天同一时间
}

// ============================================================================
// 辅助方法
// ============================================================================

func (je *JobExecutor) isExecutionTerminal(status JobExecutionStatus) bool {
	switch status {
	case JobExecutionStatusSuccess,
		JobExecutionStatusFailed,
		JobExecutionStatusRetryableFailed,
		JobExecutionStatusCanceled,
		JobExecutionStatusTimeout:
		return true
	default:
		return false
	}
}

func generateTraceID() string {
	// TODO: 实现TraceID生成
	return fmt.Sprintf("trace-%d", time.Now().UnixNano())
}

// ============================================================================
// 扩展执行器接口（扩展已有的GRPCExecutor和HTTPExecutor）
// ============================================================================

func (ge *GRPCExecutor) Query(job *Job, execution *JobExecution) (JobExecutionStatus, int32, error) {
	// TODO: gRPC查询
	return JobExecutionStatusRunning, 50, nil
}

func (ge *GRPCExecutor) Cancel(job *Job, execution *JobExecution) error {
	// TODO: gRPC取消
	return nil
}

func (he *HTTPExecutor) Query(job *Job, execution *JobExecution) (JobExecutionStatus, int32, error) {
	// TODO: HTTP查询
	return JobExecutionStatusRunning, 50, nil
}

func (he *HTTPExecutor) Cancel(job *Job, execution *JobExecution) error {
	// TODO: HTTP取消
	return nil
}
