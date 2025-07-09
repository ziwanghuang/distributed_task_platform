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
	resultChan chan<- *result
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
				elog.FieldErr(err1))
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
	acquiredTask, err := s.acquireTokenAndTask(s.ctx, task)
	if err != nil {
		s.logger.Error("任务抢占失败",
			elog.Int64("taskID", task.ID),
			elog.String("taskName", task.Name),
			elog.FieldErr(err))
		return false, err
	}

	// 抢占成功，立即创建TaskExecution记录
	execution, err := s.execSvc.Create(s.ctx, domain.TaskExecution{
		Task: *acquiredTask,
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
		s.releaseTokenAndTask(*acquiredTask)
		return false, err
	}

	// 抢占和创建都成功，启动异步任务管理
	go s.handleTaskExecution(execution, s.handleSchedulingTaskExecutionFunc)
	return true, nil
}

// acquireTokenAndTask 抢令牌 + 抢占任务
func (s *Scheduler) acquireTokenAndTask(ctx context.Context, task domain.Task) (*domain.Task, error) {
	// 抢令牌
	tokenCtx, tokenCancel := context.WithTimeout(s.ctx, s.config.TokenAcquireTimeout)
	err := s.tokens.Acquire(tokenCtx, 1)
	tokenCancel()
	if err != nil {
		return nil, fmt.Errorf("抢令牌失败: %w", err)
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

	// 抢占任务
	acquiredTask, err := s.taskAcquirer.Acquire(ctx, task.ID, s.nodeID)
	if err != nil {
		// 抢占任务失败，归还令牌
		s.tokens.Release(1)
		return nil, fmt.Errorf("任务抢占失败: %w", err)
	}

	// 抢占成功，，返回最新的 Task 对象指针
	return acquiredTask, nil
}

// releaseTokenAndTask 释放令牌和任务锁
func (s *Scheduler) releaseTokenAndTask(task domain.Task) {
	s.tokens.Release(1)
	if err := s.taskAcquirer.Release(s.ctx, task.ID, s.nodeID); err != nil {
		s.logger.Error("释放任务失败",
			elog.Int64("taskID", task.ID),
			elog.String("taskName", task.Name),
			elog.FieldErr(err))
	}
}

type HandleFunc func(ctx context.Context, result *result, execution *domain.TaskExecution) (stop bool)

func (s *Scheduler) handleTaskExecution(execution domain.TaskExecution, handleFunc HandleFunc) {
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
			execution: &execution,
			// 远程任务通过这个通道传递结果，主要路径有
			// 下方 selectedExecutor 执行
			// 调度节点的轮询协程定期主动轮询
			// 执行节点通过GPRC或kafka方式主动上报
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
			// 执行失败，执行状态未更新
			if res.err != nil {
				s.logger.Error("任务执行失败",
					elog.Int64("taskID", execution.Task.ID),
					elog.String("taskName", execution.Task.Name),
					elog.FieldErr(res.err))
				return
			}
			// 处理结果
			stop := handleFunc(s.ctx, res, &execution)
			if stop {
				return
			}
		case <-renewTicker.C:
			// 续约
			renewedTask, err := s.taskAcquirer.Renew(s.ctx, execution.Task.ID, s.nodeID)
			if err != nil {
				s.logger.Error("任务续约失败",
					elog.Int64("taskID", execution.Task.ID),
					elog.String("taskName", execution.Task.Name),
					elog.FieldErr(err))

				// 续约失败，意味着有人在调度了，啥也不干，只更新execution状态
				err = s.execSvc.UpdateStatusAndProgressAndEndTime(s.ctx, execution.ID, domain.TaskExecutionStatusFailedPreempted, execution.RunningProgress, time.Now().UnixMilli())
				if err != nil {
					s.logger.Error("更新续约状态失败",
						elog.Int64("taskID", execution.Task.ID),
						elog.Int64("executionID", execution.ID),
						elog.FieldErr(err))
				}
				return
			}
			// 更新execution中的task为最新版本
			execution.Task = *renewedTask
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
	switch {
	case execution.Status.IsPrepare() && res.state.Status.IsRunning():
		return s.setRunningState(ctx, res, execution)
	case execution.Status.IsRunning() && res.state.Status.IsRunning():
		return s.updateRunningProgress(ctx, res, execution)
	case (execution.Status.IsPrepare() || execution.Status.IsRunning()) && res.state.Status.IsTerminalStatus():
		return s.updateStatusAndProgressAndEndTime(ctx, res, execution)
	default:
		s.logger.Info("正常调度不支持的状态转换",
			elog.Int64("taskID", execution.Task.ID),
			elog.String("taskName", execution.Task.Name),
			elog.String("currentStatus", execution.Status.String()),
			elog.String("finalStatus", res.state.Status.String()))
		return true
	}
}

func (s *Scheduler) setRunningState(ctx context.Context, res *result, execution *domain.TaskExecution) (stop bool) {
	s.logger.Info("设置RUNNING状态",
		elog.Int64("executionID", res.state.ID),
		elog.String("currentStatus", execution.Status.String()),
		elog.String("targetStatus", res.state.Status.String()))
	// PREPARE → RUNNING
	// FAILED_RETRYABLE → RUNNING
	err := s.execSvc.SetRunningState(ctx, res.state.ID, res.state.RunningProgress)
	if err != nil {
		return false
	}
	// 更新内存中信息
	execution.Status = res.state.Status
	execution.RunningProgress = res.state.RunningProgress
	return false
}

func (s *Scheduler) updateRunningProgress(ctx context.Context, res *result, execution *domain.TaskExecution) (stop bool) {
	// RUNNING → RUNNING：更新进度
	err := s.execSvc.UpdateRunningProgress(ctx, res.state.ID, res.state.RunningProgress)
	if err != nil {
		return false
	}
	// 更新内存中信息
	execution.Status = res.state.Status
	execution.RunningProgress = res.state.RunningProgress
	return false
}

func (s *Scheduler) updateStatusAndProgressAndEndTime(ctx context.Context, res *result, execution *domain.TaskExecution) (stop bool) {
	err := s.execSvc.UpdateStatusAndProgressAndEndTime(ctx, res.state.ID, res.state.Status, res.state.RunningProgress, time.Now().UnixMilli())
	if err != nil {
		s.logger.Error("更新执行计划状态、进度和结束时间失败",
			elog.Int64("taskID", execution.Task.ID),
			elog.String("taskName", execution.Task.Name),
			elog.Any("state", res.state),
			elog.FieldErr(err))
		return true
	}
	s.logger.Info("任务执行完成",
		elog.Int64("taskID", execution.Task.ID),
		elog.String("taskName", execution.Task.Name),
		elog.String("status", res.state.Status.String()))

	s.updateTaskState(res, execution)
	return true
}

func (s *Scheduler) updateTaskState(res *result, execution *domain.TaskExecution) {
	// 更新调度参数
	if res.state.RequestReschedule {
		if updated, err1 := s.taskSvc.UpdateScheduleParams(s.ctx, execution.Task, res.state.RescheduleParams); err1 != nil {
			s.logger.Error("更新任务调度参数失败",
				elog.Int64("taskID", execution.Task.ID),
				elog.String("taskName", execution.Task.Name),
				elog.Any("state", res.state),
				elog.FieldErr(err1))
		} else {
			execution.Task = updated
		}
	}

	// 更新任务执行时间
	if _, err2 := s.taskSvc.UpdateNextTime(s.ctx, execution.Task); err2 != nil {
		s.logger.Error("更新任务下次执行时间失败",
			elog.Int64("taskID", execution.Task.ID),
			elog.String("taskName", execution.Task.Name),
			elog.FieldErr(err2))
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
func (s *Scheduler) RetryTaskExecution(execution domain.TaskExecution) error {
	// 是否有重试配置
	if execution.Task.RetryConfig == nil {
		execution.RetryCount++
		_ = s.execSvc.UpdateRetryResult(s.ctx, execution.ID, execution.RetryCount, execution.NextRetryTime, time.Now().UnixMilli(), domain.TaskExecutionStatusFailed)
		return fmt.Errorf("任务重试配置为空")
	}

	// 是否能够正常初始化
	retryStrategy, err := retry.NewRetry(execution.Task.RetryConfig.ToRetryComponentConfig())
	if err != nil {
		execution.RetryCount++
		_ = s.execSvc.UpdateRetryResult(s.ctx, execution.ID, execution.RetryCount, execution.NextRetryTime, time.Now().UnixMilli(), domain.TaskExecutionStatusFailed)
		return fmt.Errorf("创建重试策略失败: %w", err)
	}

	// 抢令牌和抢占任务
	acquiredTask, err := s.acquireTokenAndTask(s.ctx, execution.Task)
	if err != nil {
		s.logger.Error("重试：任务抢占失败",
			elog.Int64("taskID", execution.Task.ID),
			elog.String("taskName", execution.Task.Name),
			elog.FieldErr(err))
		return err
	}
	execution.Task = *acquiredTask

	// 开始重试调度
	go s.handleTaskExecution(execution, func(ctx context.Context, result *result, execution *domain.TaskExecution) (stop bool) {
		return s.handleRetryingTaskExecutionFunc(ctx, result, execution, retryStrategy)
	})

	return nil
}

func (s *Scheduler) handleRetryingTaskExecutionFunc(ctx context.Context, res *result, execution *domain.TaskExecution, retryStrategy strategy.Strategy) (stop bool) {
	switch {
	case execution.Status.IsFailedRetryable() && res.state.Status.IsRunning():
		return s.setRunningState(ctx, res, execution)
	case execution.Status.IsRunning() && res.state.Status.IsRunning():
		return s.updateRunningProgress(ctx, res, execution)
	case (execution.Status.IsFailedRetryable() || execution.Status.IsRunning()) && res.state.Status.IsTerminalStatus():
		return s.updateRetryResult(ctx, res, execution, retryStrategy)
	default:
		s.logger.Info("重试补偿不支持的状态转换",
			elog.Int64("taskID", execution.Task.ID),
			elog.String("taskName", execution.Task.Name),
			elog.String("currentStatus", execution.Status.String()),
			elog.String("finalStatus", res.state.Status.String()))
		return true
	}
}

func (s *Scheduler) updateRetryResult(ctx context.Context, res *result, execution *domain.TaskExecution, retryStrategy strategy.Strategy) (stop bool) {
	// FAILED_RETRYABLE 或者 RUNNING  → 终态：重试任务直接完成（成功或最终失败）
	s.logger.Info("重试任务完成",
		elog.Int64("executionID", res.state.ID),
		elog.String("currentStatus", execution.Status.String()),
		elog.String("finalStatus", res.state.Status.String()))

	// 设置结束时间
	execution.EndTime = time.Now().UnixMilli()
	// 增加重试计数
	execution.RetryCount++
	// 计算出下次重试时间
	duration, shouldRetry := retryStrategy.NextWithRetries(int32(execution.RetryCount))
	if shouldRetry {
		execution.NextRetryTime = time.Now().Add(duration).UnixMilli()
	}
	// 不管是否达到最大重试次数，都要更新状态（主要是重试次数），这样下次重试补偿任务会因其超过最大重试次数而不再重试
	err := s.execSvc.UpdateRetryResult(ctx, execution.ID, execution.RetryCount, execution.NextRetryTime, execution.EndTime, res.state.Status)
	if err != nil {
		s.logger.Error("更新执行计划重试结果失败",
			elog.Int64("taskID", execution.Task.ID),
			elog.String("taskName", execution.Task.Name),
			elog.Any("state", res.state),
			elog.FieldErr(err))
	}
	// 不管是否达到最大重试次数
	s.updateTaskState(res, execution)
	return true
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
		// 更新一下进度和状态
		switch {
		case state.Status.IsRunning():
			s.setRunningState(ctx, &result{state: state}, execution)
		case state.Status.IsTerminalStatus():
			// 可能出现覆盖的问题。
			s.updateStatusAndProgressAndEndTime(ctx, &result{state: state}, execution)
		}
		return errs.ErrInterruptTaskExecutionFailed
	}

	// 等效于执行节点主动请求重调度
	// 重调度的时候，执行节点已经自主停止了。
	// 中断成功时，执行节点也应该停止了。所以从调度节点的角度看，可以使用相同的逻辑处理，就是重调度
	state.RequestReschedule = true

	// 通知其退出调度循环，可重调度的任务都是远程的。本地的没有
	exeCtx, ok := s.remoteExecutions.Load(state.ID)
	if ok {
		// 仍有调度或重试协程，通知其退出。
		// TODO: InterruptResp.Success = true 时，应该是"终止态" 成功，失败，可重试失败。
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
	// 立即开始重调度
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
