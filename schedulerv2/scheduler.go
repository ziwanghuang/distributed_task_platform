//nolint:contextcheck // 忽略
package schedulerv2

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	executorv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/executor/v1"
	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/errs"
	"gitee.com/flycash/distributed_task_platform/internal/event"
	"gitee.com/flycash/distributed_task_platform/internal/service/runner"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"gitee.com/flycash/distributed_task_platform/pkg/grpc"
	"gitee.com/flycash/distributed_task_platform/scheduler"
	"github.com/ecodeclub/ekit/syncx"
	"github.com/ecodeclub/mq-api"
	"github.com/gotomicro/ego/core/constant"
	"github.com/gotomicro/ego/core/elog"
	"github.com/gotomicro/ego/server"
	"go.uber.org/multierr"
)

var _ server.Server = &Scheduler{}

// Scheduler 分布式任务调度器
type Scheduler struct {
	nodeID           string                                          // 当前调度节点ID
	remoteExecutions *syncx.Map[int64, *runner.TaskExecutionContext] // 正在执行的【远程】TaskExecution 及其上下文
	runner           runner.Runner                                   // Runner 分发器
	taskSvc          task.Service                                    // 任务服务
	consumers        map[string]*event.Consumer                      // 消费者
	grpcClients      *grpc.Clients[executorv1.ExecutorServiceClient] // gRPC客户端池
	config           scheduler.Config                                // 配置
	ctx              context.Context
	cancel           context.CancelFunc
	logger           *elog.Component
}

// NewSchedulerV2 创建调度器实例
func NewSchedulerV2(
	nodeID string,
	remoteExecutions *syncx.Map[int64, *runner.TaskExecutionContext],
	runner runner.Runner,
	taskSvc task.Service,
	consumers map[string]*event.Consumer,
	grpcClients *grpc.Clients[executorv1.ExecutorServiceClient],
	config scheduler.Config,
) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &Scheduler{
		nodeID:           nodeID,
		remoteExecutions: remoteExecutions,
		runner:           runner,
		taskSvc:          taskSvc,
		consumers:        consumers,
		grpcClients:      grpcClients,
		config:           config,
		ctx:              ctx,
		cancel:           cancel,
		logger:           elog.DefaultLogger.With(elog.FieldComponentName("Scheduler")),
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
		err1 := s.runner.Run(s.ctx, tasks[i])
		if err1 != nil {
			s.logger.Error("调度任务失败",
				elog.Int64("taskID", tasks[i].ID),
				elog.String("taskName", tasks[i].Name),
				elog.FieldErr(err1))
		} else {
			success++
		}
	}
	s.logger.Info("调度信息",
		elog.Int("success", success),
		elog.Int("total", len(tasks)))
	return nil
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
			s.remoteExecutions.Range(func(_ int64, value *runner.TaskExecutionContext) bool {
				exec := value.Execution
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
				sendErr := s.trySendResult(context.Background(), value.ResultChan, &runner.Result{State: state}, s.config.ReportTimeout)
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
func (s *Scheduler) trySendResult(ctx context.Context, resultChan chan<- *runner.Result, result *runner.Result, timeout time.Duration) error {
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
		if execCtx.Execution.Task.ID != report.ExecutionState.TaskID {
			skippedCount++
			s.logger.Warn("执行记录TaskID不匹配，跳过上报",
				elog.Int64("expectedTaskID", execCtx.Execution.Task.ID),
				elog.Int64("reportTaskID", report.ExecutionState.TaskID),
				elog.Int64("executionID", report.ExecutionState.ID))
			continue
		}

		// 推送上报结果
		sendErr := s.trySendResult(context.Background(), execCtx.ResultChan, &runner.Result{State: report.ExecutionState}, s.config.ReportTimeout)
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

// RetryTaskExecution 重试任务执行
func (s *Scheduler) RetryTaskExecution(execution domain.TaskExecution) error {
	return s.runner.Retry(s.ctx, execution)
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
		// 中断失败，忽略状态
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
		sendErr := s.trySendResult(ctx, exeCtx.ResultChan, &runner.Result{State: state}, s.config.ReportTimeout)
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
	err = s.runner.Run(ctx, tk)
	if err != nil {
		s.logger.Error("重调度任务失败",
			elog.Int64("taskID", tk.ID),
			elog.String("taskName", tk.Name),
			elog.FieldErr(err))
		return fmt.Errorf("%w: %w", errs.ErrRescheduleTaskExecutionFailed, err)
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
