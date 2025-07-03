//nolint:contextcheck // 忽略
package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	executorv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/executor/v1"
	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/errs"
	"gitee.com/flycash/distributed_task_platform/internal/event"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"gitee.com/flycash/distributed_task_platform/pkg/grpc"
	"gitee.com/flycash/distributed_task_platform/pkg/retry"
	"gitee.com/flycash/distributed_task_platform/pkg/retry/strategy"
	"gitee.com/flycash/distributed_task_platform/scheduler/executor"
	"github.com/ecodeclub/ekit/syncx"
	"github.com/ecodeclub/mq-api"
	"github.com/gotomicro/ego/core/constant"
	"github.com/gotomicro/ego/core/elog"
	"github.com/gotomicro/ego/server"
	"go.uber.org/multierr"
	"golang.org/x/sync/semaphore"
)

var _ server.Server = &Scheduler{}

type result struct {
	state domain.ExecutionState
	err   error
}

// executionContext 执行上下文，包含TaskExecution和相关通道
type executionContext struct {
	execution  *domain.TaskExecution
	resultChan chan *result
}

// Scheduler 分布式任务调度器
type Scheduler struct {
	nodeID           string                              // 当前调度节点ID
	tokens           *semaphore.Weighted                 // 令牌
	remoteExecutions syncx.Map[int64, *executionContext] // 正在执行的TaskExecution及其上下文
	taskSvc          task.Service                        // 任务服务
	execSvc          task.ExecutionService               // 任务执行服务
	taskAcquirer     TaskAcquirer                        // 任务抢占器
	executors        map[string]executor.Executor
	consumers        map[string]*event.Consumer                      // 消费者
	grpcClients      *grpc.Clients[executorv1.ExecutorServiceClient] // gRPC客户端池
	config           Config                                          // 配置
	ctx              context.Context
	cancel           context.CancelFunc
	logger           *elog.Component
}

// Config 调度器配置
type Config struct {
	BatchTimeout        time.Duration `yaml:"batchTimeout"`
	BatchSize           int           `yaml:"batchSize"`           // 批量获取任务数量
	PreemptedTimeout    time.Duration `yaml:"preemptedTimeout"`    // 表示处于 PREEMPTED 状态任务的超时时间（毫秒）
	ScheduleInterval    time.Duration `yaml:"scheduleInterval"`    // 调度间隔
	RenewInterval       time.Duration `yaml:"renewInterval"`       // 续约间隔
	MaxConcurrentTasks  int64         `yaml:"maxConcurrentTasks"`  // 最大并发任务数
	TokenAcquireTimeout time.Duration `yaml:"tokenAcquireTimeout"` // 抢令牌超时时间
	PollInterval        time.Duration `yaml:"pollInterval"`        // 轮询间隔
	PollTimeout         time.Duration `yaml:"pollTimeout"`         // 轮询超时
	ReportTimeout       time.Duration `yaml:"reportTimeout"`
}

// NewScheduler 创建调度器实例
func NewScheduler(
	nodeID string,
	taskSvc task.Service,
	execSvc task.ExecutionService,
	acquirer TaskAcquirer,
	executors map[string]executor.Executor,
	consumers map[string]*event.Consumer,
	grpcClients *grpc.Clients[executorv1.ExecutorServiceClient],
	config Config,
) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &Scheduler{
		nodeID:       nodeID,
		taskSvc:      taskSvc,
		execSvc:      execSvc,
		taskAcquirer: acquirer,
		executors:    executors,
		consumers:    consumers,
		tokens:       semaphore.NewWeighted(config.MaxConcurrentTasks),
		grpcClients:  grpcClients,
		config:       config,
		ctx:          ctx,
		cancel:       cancel,
		logger:       elog.DefaultLogger.With(elog.FieldComponentName("Scheduler")),
	}
}

func (s *Scheduler) NodeID() string {
	return s.nodeID
}

func (s *Scheduler) Name() string {
	return fmt.Sprintf("Scheduler-%s", s.nodeID)
}

func (s *Scheduler) PackageName() string {
	return "scheduler.Scheduler"
}

func (s *Scheduler) Init() error {
	return nil
}

// Start 启动调度器
func (s *Scheduler) Start() error {
	s.logger.Info("启动分布式任务调度器", elog.String("nodeID", s.nodeID))

	// 启动调度循环
	go s.scheduleLoop()

	// 启动轮询循环
	go s.pollingLoop()

	// 初始化推送消息消费者
	var err error
	for key := range s.consumers {
		switch key {
		case "executionReportEvent":
			err = s.consumers[key].Start(s.ctx, s.consumeExecutionReportEvent)
			if err != nil {
				return err
			}
		case "executionBatchReportEvent":
			err = s.consumers[key].Start(s.ctx, s.consumeExecutionBatchReportEvent)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// scheduleLoop 主调度循环
func (s *Scheduler) scheduleLoop() {
	s.logger.Info("启动调度循环中.....")
	defer func() {
		s.logger.Info("退出调度循环中.....")
	}()
	ticker := time.NewTicker(s.config.ScheduleInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.logger.Info("开始一次调度")
			if err := s.schedule(); err != nil {
				s.logger.Error("本次调度失败", elog.FieldErr(err))
			}
		case <-s.ctx.Done():
			s.logger.Info("调度循环结束")
			return
		}
	}
}

// schedule 单次调度逻辑
func (s *Scheduler) schedule() error {
	// 获取可调度的任务列表
	ctx, cancelFunc := context.WithTimeout(s.ctx, s.config.BatchTimeout)
	tasks, err := s.taskSvc.SchedulableTasks(ctx, s.config.PreemptedTimeout.Milliseconds(), s.config.BatchSize)
	cancelFunc()
	if err != nil {
		return fmt.Errorf("获取可调度任务失败: %w", err)
	}
	if len(tasks) == 0 {
		s.logger.Info("没有可调度的任务")
		return nil
	}

	s.logger.Info("发现可调度任务", elog.Int("count", len(tasks)))
	// 开始调度
	success := 0
	for i := range tasks {
		ok, err1 := s.scheduleTask(tasks[i])
		if err1 != nil {
			s.logger.Error("调度任务失败",
				elog.Int64("taskID", tasks[i].ID),
				elog.String("taskName", tasks[i].Name),
				elog.FieldErr(err))
		}
		if ok {
			success++
		}
	}
	s.logger.Info("调度信息",
		elog.Int("success", success),
		elog.Int("total", len(tasks)))
	return nil
}

func (s *Scheduler) scheduleTask(task domain.Task) (bool, error) {
	// 抢令牌和抢占任务
	success, err := s.acquireTokenAndTask(s.ctx, task)
	if err != nil {
		s.logger.Error("任务抢占失败",
			elog.Int64("taskID", task.ID),
			elog.String("taskName", task.Name),
			elog.FieldErr(err))
		return false, err
	}
	if !success {
		return false, nil
	}

	// 抢占成功，立即创建TaskExecution记录
	task.Version++
	execution, err := s.execSvc.Create(s.ctx, domain.TaskExecution{
		Task: task,
		// 可以认为开始执行了，防止执行节点直接返回"终态"状态Failed，Success等
		StartTime: time.Now().UnixMilli(),
		Status:    domain.TaskExecutionStatusPrepare,
	})
	if err != nil {
		s.logger.Error("创建任务执行记录失败",
			elog.Int64("taskID", task.ID),
			elog.String("taskName", task.Name),
			elog.FieldErr(err))
		// 创建失败，释放令牌和锁
		s.releaseTokenAndTask(task)
		return false, err
	}

	// 抢占和创建都成功，启动异步任务管理
	go s.taskExecutionHandleLoop(execution, s.handleSchedulingTaskExecutionFunc)
	return true, nil
}

// acquireTokenAndTask 抢令牌 + 抢占任务
func (s *Scheduler) acquireTokenAndTask(ctx context.Context, task domain.Task) (bool, error) {
	// 抢令牌
	tokenCtx, tokenCancel := context.WithTimeout(s.ctx, s.config.TokenAcquireTimeout)
	err := s.tokens.Acquire(tokenCtx, 1)
	tokenCancel()
	if err != nil {
		return false, fmt.Errorf("抢令牌失败: %w", err)
	}

	// 令牌抢占成功，添加panic保护
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("任务抢占过程中发生panic",
				elog.Int64("taskID", task.ID),
				elog.String("taskName", task.Name),
				elog.Any("panic", r))
			// 令牌已获取，需要释放
			s.tokens.Release(1)
			// 重新抛出panic
			panic(r)
		}
	}()

	// 填充调度节点ID
	task.ScheduleNodeID = s.nodeID
	if err = s.taskAcquirer.Acquire(ctx, task); err != nil {
		// 抢占任务失败，归还令牌
		s.tokens.Release(1)
		return false, fmt.Errorf("任务抢占失败: %w", err)
	}

	// 抢占成功
	return true, nil
}

// releaseTokenAndTask 释放令牌和任务锁
func (s *Scheduler) releaseTokenAndTask(task domain.Task) {
	s.tokens.Release(1)
	if err := s.taskAcquirer.Release(s.ctx, task); err != nil {
		s.logger.Error("释放任务失败",
			elog.Int64("taskID", task.ID),
			elog.String("taskName", task.Name),
			elog.FieldErr(err))
	}
}

type HandleFunc func(ctx context.Context, result *result, execution *domain.TaskExecution) (stop bool)

func (s *Scheduler) taskExecutionHandleLoop(execution domain.TaskExecution, handleFunc HandleFunc) {
	s.logger.Info("开始处理任务",
		elog.Int64("taskID", execution.Task.ID),
		elog.String("taskName", execution.Task.Name))

	// 续约定时器
	renewTicker := time.NewTicker(s.config.RenewInterval)
	defer renewTicker.Stop()

	// 确保最后释放任务和清理缓存
	defer func() {
		s.releaseTokenAndTask(execution.Task)
	}()

	// 选中执行器
	selectedExecutor, ok := s.executors[execution.Task.ExecutionMethod.String()]
	if !ok {
		err := fmt.Errorf("%w: %s", errs.ErrInvalidTaskExecutionMethod, execution.Task.ExecutionMethod)
		s.logger.Error("未找到任务执行器",
			elog.Int64("taskID", execution.Task.ID),
			elog.String("taskName", execution.Task.Name),
			elog.String("executionMethod", execution.Task.ExecutionMethod.String()),
			elog.FieldErr(err))
		return
	}

	const resultChanBufferSize = 10
	resultChan := make(chan *result, resultChanBufferSize)

	// 远程任务需要轮询
	if execution.Task.ExecutionMethod.IsRemote() {
		s.remoteExecutions.Store(execution.ID, &executionContext{
			execution:  &execution,
			resultChan: resultChan,
		})
		defer s.remoteExecutions.Delete(execution.ID)
	}

	// 启动执行任务
	go func() {
		state, err := selectedExecutor.Run(s.ctx, execution)
		resultChan <- &result{state: state, err: err}
	}()

	for {
		select {
		case res := <-resultChan:
			// 处理结果
			stop := handleFunc(s.ctx, res, &execution)
			if stop {
				return
			}
		case <-renewTicker.C:
			// 续约
			if err := s.taskAcquirer.Renew(s.ctx, execution.Task); err != nil {
				s.logger.Error("任务续约失败",
					elog.Int64("taskID", execution.Task.ID),
					elog.String("taskName", execution.Task.Name),
					elog.FieldErr(err))

				// 续约失败，直接更新execution状态
				if updateErr := s.execSvc.UpdateStatusAndEndTime(s.ctx, execution.ID, domain.TaskExecutionStatusFailedPreempted, time.Now().UnixMilli()); updateErr != nil {
					s.logger.Error("更新续约失败状态失败",
						elog.Int64("taskID", execution.Task.ID),
						elog.Int64("executionID", execution.ID),
						elog.FieldErr(updateErr))
				}
				return
			}
			s.logger.Debug("任务续约成功",
				elog.Int64("taskID", execution.Task.ID),
				elog.String("taskName", execution.Task.Name))

		case <-s.ctx.Done():
			// 调度器停止
			return
		}
	}
}

func (s *Scheduler) handleSchedulingTaskExecutionFunc(ctx context.Context, res *result, execution *domain.TaskExecution) (stop bool) {
	// 执行失败，执行状态未更新，这个由重试补偿任务来处理
	if res.err != nil {
		s.logger.Error("任务执行失败",
			elog.Int64("taskID", execution.Task.ID),
			elog.String("taskName", execution.Task.Name),
			elog.FieldErr(res.err))
		return true
	}

	// 执行成功，更新状态 - 使用正常任务的状态转换
	err := s.updateSchedulingExecutionState(ctx, execution.Status, res.state)
	if err != nil {
		s.logger.Error("更新执行状态失败",
			elog.Int64("taskID", execution.Task.ID),
			elog.String("taskName", execution.Task.Name),
			elog.Any("state", res.state),
			elog.FieldErr(err))
		return false // 继续等待重试
	}
	// 更新内存中信息
	execution.Status = res.state.Status
	execution.RunningProgress = res.state.RunningProgress

	// 继续等待轮询协程或者上报流程处理状态迁移
	if execution.Status.IsRunning() {
		return false
	}

	s.logger.Info("任务执行完成",
		elog.Int64("taskID", execution.Task.ID),
		elog.String("taskName", execution.Task.Name),
		elog.String("status", res.state.Status.String()))

	// 更新下次执行时间，允许其再次被调度
	if updateErr := s.taskSvc.UpdateNextTime(s.ctx, execution.Task); updateErr != nil {
		s.logger.Error("更新下次执行时间失败",
			elog.Int64("taskID", execution.Task.ID),
			elog.String("taskName", execution.Task.Name),
			elog.FieldErr(updateErr))
	}
	return true
}

// updateSchedulingExecutionState 更新执行状态，统一处理所有状态转换和更新
func (s *Scheduler) updateSchedulingExecutionState(ctx context.Context, status domain.TaskExecutionStatus, targetState domain.ExecutionState) error {
	switch {
	// PREPARE 状态的转换
	case status.IsPrepare() && targetState.Status.IsRunning():
		return s.execSvc.SetRunningState(ctx, targetState.ID, targetState.RunningProgress)
	case status.IsPrepare() && targetState.Status.IsTerminalStatus():
		return s.execSvc.UpdateStatusAndEndTime(ctx, targetState.ID, targetState.Status, time.Now().UnixMilli())
	// RUNNING 状态的转换
	case status.IsRunning() && targetState.Status.IsRunning():
		return s.execSvc.UpdateProgress(ctx, targetState.ID, targetState.RunningProgress)
	case status.IsRunning() && targetState.Status.IsTerminalStatus():
		return s.execSvc.UpdateStatusAndEndTime(ctx, targetState.ID, targetState.Status, time.Now().UnixMilli())
	default:
		return fmt.Errorf("不支持的状态转换：%s → %s", status.String(), targetState.Status.String())
	}
}

// pollingLoop 轮询循环，每隔一段时间轮询所有 remoteExecutions
func (s *Scheduler) pollingLoop() {
	s.logger.Info("启动轮询循环中.....")
	defer func() {
		s.logger.Info("退出轮询循环中.....")
	}()
	ticker := time.NewTicker(s.config.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			// 遍历所有需要轮询的executions
			s.logger.Info("开始新一轮执行状态轮询.....")
			s.remoteExecutions.Range(func(_ int64, value *executionContext) bool {
				exec := value.execution
				ctx, cancel := context.WithTimeout(context.Background(), s.config.PollTimeout)
				state, err := s.pollExecutionState(ctx, exec)
				cancel()
				if err != nil {
					s.logger.Warn("主动轮询任务执行状态失败",
						elog.Int64("taskID", exec.Task.ID),
						elog.Int64("executionID", exec.ID),
						elog.String("status", exec.Status.String()),
						elog.FieldErr(err))
					return true
				}
				s.logger.Debug("主动轮询任务执行状态成功",
					elog.Int64("taskID", state.TaskID),
					elog.Int64("executionID", state.ID),
					elog.String("status", state.Status.String()),
				)
				// 推送轮询结果
				sendErr := s.trySendResult(context.Background(), value.resultChan, &result{state: state}, s.config.ReportTimeout)
				if sendErr != nil {
					s.logger.Warn("推送轮询结果失败",
						elog.Int64("taskID", state.TaskID),
						elog.Int64("executionID", state.ID),
						elog.String("timeout", s.config.ReportTimeout.String()),
						elog.FieldErr(sendErr))
				}

				return true
			})
		case <-s.ctx.Done():
			return
		}
	}
}

// trySendResult 尝试发送结果到resultChan，支持超时和context取消
func (s *Scheduler) trySendResult(ctx context.Context, resultChan chan<- *result, result *result, timeout time.Duration) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	select {
	case <-s.ctx.Done():
		// 调度器被停止
		return s.ctx.Err()
	case <-timeoutCtx.Done():
		// 超时或外部context被取消
		return timeoutCtx.Err()
	case resultChan <- result:
		// 成功发送
		return nil
	}
}

// pollExecutionState 主动轮询执行节点状态
func (s *Scheduler) pollExecutionState(ctx context.Context, exec *domain.TaskExecution) (domain.ExecutionState, error) {
	if exec.Task.GrpcConfig != nil {
		return s.pollGRPCExecutionState(ctx, exec)
	} else if exec.Task.HTTPConfig != nil {
		return s.pollHTTPExecutionState(ctx, exec)
	}
	return domain.ExecutionState{}, fmt.Errorf("未找到有效的通信配置，无法轮询执行状态")
}

// pollGRPCExecutionState 通过gRPC轮询状态
func (s *Scheduler) pollGRPCExecutionState(ctx context.Context, exec *domain.TaskExecution) (domain.ExecutionState, error) {
	client := s.grpcClients.Get(exec.Task.GrpcConfig.ServiceName)
	resp, err := client.Query(ctx, &executorv1.QueryRequest{
		Eid: exec.ID,
	})
	if err != nil {
		return domain.ExecutionState{}, err
	}
	return domain.ExecutionStateFromProto(resp.GetExecutionState()), nil
}

// pollHTTPExecutionState 通过HTTP轮询状态
func (s *Scheduler) pollHTTPExecutionState(_ context.Context, exec *domain.TaskExecution) (domain.ExecutionState, error) {
	// TODO: 实现HTTP轮询状态查询，当前为占位实现
	s.logger.Warn("HTTP状态轮询尚未实现，暂时标记为已完成",
		elog.Int64("taskId", exec.Task.ID),
		elog.Int64("executionId", exec.ID))
	return domain.ExecutionState{}, nil
}

// consumeExecutionReportEvent 异步消费 ExecutionReport 事件
func (s *Scheduler) consumeExecutionReportEvent(ctx context.Context, message *mq.Message) error {
	report := &domain.Report{}
	err := json.Unmarshal(message.Value, report)
	if err != nil {
		s.logger.Error("反序列化MQ消息体失败",
			elog.String("step", "consumeExecutionReportEvent"),
			elog.String("MQ消息体", string(message.Value)),
			elog.FieldErr(err),
		)
		return err
	}

	if !report.ExecutionState.Status.IsValid() {
		err = errs.ErrInvalidTaskExecutionStatus
		s.logger.Error("执行记录状态非法",
			elog.String("step", "consumeExecutionReportEvent"),
			elog.String("MQ消息体", string(message.Value)),
			elog.FieldErr(err),
		)
		return err
	}

	err = s.HandleReports(ctx, []*domain.Report{report})
	if err != nil {
		s.logger.Error("处理异步上报失败",
			elog.String("step", "consumeExecutionReportEvent"),
			elog.Any("report", report),
			elog.FieldErr(err))
		return err
	}
	return nil
}

// HandleReports 批量处理执行节点上报的执行状态
func (s *Scheduler) HandleReports(_ context.Context, reports []*domain.Report) error {
	if len(reports) == 0 {
		return nil
	}
	s.logger.Debug("开始处理执行状态上报", elog.Int("count", len(reports)))

	var err error
	processedCount := 0
	skippedCount := 0

	for i := range reports {
		report := reports[i]
		// 通过execution ID查找正在执行的TaskExecution
		execCtx, exists := s.remoteExecutions.Load(report.ExecutionState.ID)
		if !exists {
			skippedCount++
			s.logger.Debug("执行记录不存在，跳过上报",
				elog.Int64("taskID", report.ExecutionState.TaskID),
				elog.Int64("executionID", report.ExecutionState.ID))
			continue
		}
		if execCtx == nil {
			skippedCount++
			s.logger.Warn("执行记录为空，跳过上报",
				elog.Int64("taskID", report.ExecutionState.TaskID),
				elog.Int64("executionID", report.ExecutionState.ID))
			continue
		}
		// 验证TaskID是否匹配
		if execCtx.execution.Task.ID != report.ExecutionState.TaskID {
			skippedCount++
			s.logger.Warn("执行记录TaskID不匹配，跳过上报",
				elog.Int64("expectedTaskID", execCtx.execution.Task.ID),
				elog.Int64("reportTaskID", report.ExecutionState.TaskID),
				elog.Int64("executionID", report.ExecutionState.ID))
			continue
		}

		// 推送上报结果
		sendErr := s.trySendResult(context.Background(), execCtx.resultChan, &result{state: report.ExecutionState}, s.config.ReportTimeout)
		if sendErr == nil {
			processedCount++
		} else {
			// 包装错误，添加上报场景的特定信息
			wrappedErr := fmt.Errorf("处理上报超时: taskID=%d, executionID=%d: %w",
				report.ExecutionState.TaskID, report.ExecutionState.ID, sendErr)
			err = multierr.Append(err, wrappedErr)
		}
	}
	// 记录处理统计信息
	s.logger.Info("执行状态上报处理完成",
		elog.Int("total", len(reports)),
		elog.Int("processed", processedCount),
		elog.Int("skipped", skippedCount))
	return err
}

// consumeExecutionBatchReportEvent 异步消费 ExecutionBatchReport 事件
func (s *Scheduler) consumeExecutionBatchReportEvent(ctx context.Context, message *mq.Message) error {
	batchReport := &domain.BatchReport{}
	err := json.Unmarshal(message.Value, &batchReport)
	if err != nil {
		s.logger.Error("反序列化MQ消息体失败",
			elog.String("step", "consumeExecutionBatchReportEvent"),
			elog.String("MQ消息体", string(message.Value)),
			elog.FieldErr(err),
		)
		return err
	}

	for i := range batchReport.Reports {
		if !batchReport.Reports[i].ExecutionState.Status.IsValid() {
			err = errs.ErrInvalidTaskExecutionStatus
			s.logger.Error("执行记录状态非法",
				elog.String("step", "consumeExecutionBatchReportEvent"),
				elog.String("MQ消息体", string(message.Value)),
				elog.FieldErr(err),
			)
			return err
		}
	}

	err = s.HandleReports(ctx, batchReport.Reports)
	if err != nil {
		s.logger.Error("处理异步批量上报失败",
			elog.String("step", "consumeExecutionBatchReportEvent"),
			elog.Any("reports", batchReport),
			elog.FieldErr(err))
		return err
	}
	return nil
}

// RetryTaskExecution 重试任务执行 - 原始版本
func (s *Scheduler) RetryTaskExecution(execution domain.TaskExecution) (bool, error) {
	// 抢令牌和抢占任务
	success, err := s.acquireTokenAndTask(s.ctx, execution.Task)
	if err != nil {
		s.logger.Error("重试：任务抢占失败",
			elog.Int64("taskID", execution.Task.ID),
			elog.String("taskName", execution.Task.Name),
			elog.FieldErr(err))
		return false, err
	}
	if !success {
		return false, nil
	}
	// 抢占成功，启动异步任务管理
	execution.Task.Version++
	// 是否有重试配置
	if execution.Task.RetryConfig == nil {
		execution.RetryCount++
		_ = s.execSvc.UpdateRetryResult(s.ctx, execution.ID, execution.RetryCount, execution.NextRetryTime, time.Now().UnixMilli(), domain.TaskExecutionStatusFailed)
		s.releaseTokenAndTask(execution.Task)
		return false, fmt.Errorf("任务重试配置为空")
	}
	// 是否能够正常初始化
	retryStrategy, err := retry.NewRetry(execution.Task.RetryConfig.ToRetryComponentConfig())
	if err != nil {
		execution.RetryCount++
		_ = s.execSvc.UpdateRetryResult(s.ctx, execution.ID, execution.RetryCount, execution.NextRetryTime, time.Now().UnixMilli(), domain.TaskExecutionStatusFailed)
		s.releaseTokenAndTask(execution.Task)
		return false, fmt.Errorf("创建重试策略失败: %w", err)
	}

	// 重试情况
	go s.taskExecutionHandleLoop(execution, func(ctx context.Context, result *result, execution *domain.TaskExecution) (stop bool) {
		return s.handleRetryingTaskExecutionFunc(ctx, result, execution, retryStrategy)
	})

	return true, nil
}

func (s *Scheduler) handleRetryingTaskExecutionFunc(ctx context.Context, res *result, execution *domain.TaskExecution, retryStrategy strategy.Strategy) (stop bool) {
	// 执行失败
	if res.err != nil {
		s.logger.Error("重试任务执行失败",
			elog.Int64("taskID", execution.Task.ID),
			elog.String("taskName", execution.Task.Name),
			elog.FieldErr(res.err))
		return true
	}

	execution.RetryCount++

	duration, shouldRetry := retryStrategy.NextWithRetries(int32(execution.RetryCount))
	if !shouldRetry {
		// 到达最大重试次数，直接结束
		execution.EndTime = time.Now().UnixMilli()
		_ = s.execSvc.UpdateRetryResult(ctx, execution.ID, execution.RetryCount, execution.NextRetryTime, execution.EndTime, domain.TaskExecutionStatusFailed)
		return true
	}

	// 计算出下次重试时间
	execution.NextRetryTime = time.Now().Add(duration).UnixMilli()
	execution.EndTime = time.Now().UnixMilli()

	// 执行成功，更新状态 - 使用重试任务的状态转换
	err := s.updateRetryingExecutionState(s.ctx, execution, res.state)
	if err != nil {
		s.logger.Error("更新重试执行状态失败",
			elog.Int64("taskID", execution.Task.ID),
			elog.String("taskName", execution.Task.Name),
			elog.Any("state", res.state),
			elog.FieldErr(err))
		return false // 继续等待重试
	}

	// 更新内存中信息
	execution.Status = res.state.Status
	execution.RunningProgress = res.state.RunningProgress

	// 重试任务执行完成
	if execution.Status.IsSuccess() || execution.Status.IsFailed() {
		s.logger.Info("重试任务执行完成",
			elog.Int64("taskID", execution.Task.ID),
			elog.String("taskName", execution.Task.Name),
			elog.String("status", res.state.Status.String()))

		// 重试完成，更新任务的下次执行时间
		if updateErr := s.taskSvc.UpdateNextTime(s.ctx, execution.Task); updateErr != nil {
			s.logger.Error("更新下次执行时间失败",
				elog.Int64("taskID", execution.Task.ID),
				elog.String("taskName", execution.Task.Name),
				elog.FieldErr(updateErr))
		}
		return true
	}
	return false
}

func (s *Scheduler) updateRetryingExecutionState(ctx context.Context, execution *domain.TaskExecution, targetState domain.ExecutionState) error {
	// 重试任务的状态转换逻辑
	switch {
	// FAILED_RETRYABLE → RUNNING：重试任务开始执行
	case execution.Status.IsFailedRetryable() && targetState.Status.IsRunning():
		s.logger.Info("重试任务开始执行",
			elog.Int64("executionID", targetState.ID),
			elog.String("currentStatus", execution.Status.String()),
			elog.String("targetStatus", targetState.Status.String()))
		return s.execSvc.SetRunningState(ctx, targetState.ID, targetState.RunningProgress)

	// FAILED_RETRYABLE → 终态：重试任务直接完成（成功或最终失败）
	case execution.Status.IsFailedRetryable() && targetState.Status.IsTerminalStatus():
		s.logger.Info("重试任务直接完成",
			elog.Int64("executionID", targetState.ID),
			elog.String("currentStatus", execution.Status.String()),
			elog.String("finalStatus", targetState.Status.String()))
		if targetState.Status.IsSuccess() || targetState.Status.IsFailed() {
			// 不需要下次重试时间
			execution.Status = targetState.Status
			execution.NextRetryTime = 0
			execution.EndTime = time.Now().UnixMilli()
		}
		return s.execSvc.UpdateRetryResult(ctx, execution.ID, execution.RetryCount, execution.NextRetryTime, execution.EndTime, execution.Status)

	// RUNNING → RUNNING：更新进度
	case execution.Status.IsRunning() && targetState.Status.IsRunning():
		return s.execSvc.UpdateProgress(ctx, targetState.ID, targetState.RunningProgress)

	// RUNNING → 终态：执行完成
	case execution.Status.IsRunning() && targetState.Status.IsTerminalStatus():
		s.logger.Info("重试任务执行完成",
			elog.Int64("executionID", targetState.ID),
			elog.String("currentStatus", execution.Status.String()),
			elog.String("finalStatus", targetState.Status.String()))
		if targetState.Status.IsSuccess() || targetState.Status.IsFailed() {
			// 不需要下次重试时间
			execution.Status = targetState.Status
			execution.NextRetryTime = 0
			execution.EndTime = time.Now().UnixMilli()
		}
		return s.execSvc.UpdateRetryResult(ctx, execution.ID, execution.RetryCount, execution.NextRetryTime, execution.EndTime, execution.Status)
	default:
		return fmt.Errorf("重试任务不支持的状态转换：%s → %s", execution.Status.String(), targetState.Status.String())
	}
}

func (s *Scheduler) InterruptTaskExecution(ctx context.Context, execution *domain.TaskExecution) error {
	if execution.Task.GrpcConfig == nil {
		return fmt.Errorf("未找到GPRC配置，无法执行中断任务")
	}

	client := s.grpcClients.Get(execution.Task.GrpcConfig.ServiceName)
	resp, err := client.Interrupt(ctx, &executorv1.InterruptRequest{
		Eid: execution.ID,
	})
	if err != nil {
		return fmt.Errorf("发送中断请求失败：%w", err)
	}

	state := domain.ExecutionStateFromProto(resp.GetExecutionState())
	if !resp.GetSuccess() {
		// 中断失败
		return errs.ErrInterruptTaskExecutionFailed
	}
	// 等效于执行节点主动请求中断，因为此时执行节点已经停止了。
	state.RequestReschedule = true
	// 通知其退出调度循环
	exeCtx, ok := s.remoteExecutions.Load(state.ID)
	if ok {
		// 仍有调度或重试协程，通知其退出。
		// 这里有一个假设 —— InterruptResp.Success = true 时，应该是"终止态" 成功，失败，可重试失败，最好给一个 INTERRUPT 状态。
		sendErr := s.trySendResult(ctx, exeCtx.resultChan, &result{state: state}, s.config.ReportTimeout)
		if sendErr != nil {
			s.logger.Warn("推送中断结果失败",
				elog.Int64("taskID", state.TaskID),
				elog.Int64("executionID", state.ID),
				elog.String("timeout", s.config.ReportTimeout.String()),
				elog.FieldErr(sendErr))
			// 不返回错误，开始重调度
		}
	}
	// 通知调度或者重试协程停止（释放Task），立即重调度
	return s.RescheduleNow(ctx, state)
}

func (s *Scheduler) RescheduleNow(ctx context.Context, state domain.ExecutionState) error {
	tk, err := s.taskSvc.GetByID(ctx, state.TaskID)
	if err != nil {
		s.logger.Error("查找Task失败", elog.FieldErr(err))
		return err
	}
	// 更新调度参数
	err = s.taskSvc.UpdateScheduleParams(ctx, tk.ID, tk.Version, state.RescheduleParams)
	if err != nil {
		s.logger.Error("更新调度信息失败", elog.FieldErr(err))
		return err
	}
	// 立即开始重调度
	tk.Version++
	success, err := s.scheduleTask(tk)
	if err != nil {
		s.logger.Error("重调度任务失败",
			elog.Int64("taskID", tk.ID),
			elog.String("taskName", tk.Name),
			elog.FieldErr(err))
		return err
	}
	if !success {
		s.logger.Warn("重调度任务失败",
			elog.Int64("taskID", tk.ID),
			elog.String("taskName", tk.Name))
		return errs.ErrRescheduleTaskExecutionFailed
	}
	return nil
}

// Stop 停止调度器
func (s *Scheduler) Stop() error {
	s.logger.Info("停止分布式任务调度器", elog.String("nodeID", s.nodeID))
	// 取消上下文
	s.cancel()
	return nil
}

func (s *Scheduler) GracefulStop(ctx context.Context) error {
	s.logger.Info("停止分布式任务调度器", elog.String("nodeID", s.nodeID))

	// 关闭消费者
	for key := range s.consumers {
		err := s.consumers[key].Stop()
		if err != nil {
			s.logger.Error("关闭消费者失败",
				elog.String("name", s.consumers[key].Name()),
				elog.FieldErr(err))
		} else {
			s.logger.Info("关闭消费者成功",
				elog.String("name", s.consumers[key].Name()),
			)
		}
	}

	s.cancel()
	<-ctx.Done()
	return nil
}

func (s *Scheduler) Info() *server.ServiceInfo {
	info := server.ApplyOptions(
		server.WithName(s.Name()),
		server.WithKind(constant.ServiceProvider),
	)
	info.Healthy = s.ctx.Err() == nil
	return &info
}
