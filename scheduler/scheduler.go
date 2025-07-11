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
	"gitee.com/flycash/distributed_task_platform/internal/service/runner"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"gitee.com/flycash/distributed_task_platform/pkg/acquirer"
	"gitee.com/flycash/distributed_task_platform/pkg/grpc"
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
	nodeID                 string                                              // 当前调度节点ID
	executionStateHandlers *syncx.Map[int64, runner.TaskExecutionStateHandler] // 正在执行任务的结果处理器
	runner                 runner.Runner                                       // Runner 分发器
	taskSvc                task.Service                                        // 任务服务
	acquirer               acquirer.TaskAcquirer                               // 任务抢占、续约、释放器
	consumers              map[string]*event.Consumer                          // 消费者
	grpcClients            *grpc.Clients[executorv1.ExecutorServiceClient]     // gRPC客户端池
	config                 Config                                              // 配置
	ctx                    context.Context
	cancel                 context.CancelFunc
	logger                 *elog.Component
}

// Config 调度器配置
type Config struct {
	BatchTimeout     time.Duration `yaml:"batchTimeout"`
	BatchSize        int           `yaml:"batchSize"`        // 批量获取任务数量
	PreemptedTimeout time.Duration `yaml:"preemptedTimeout"` // 表示处于 PREEMPTED 状态任务的超时时间（毫秒）
	ScheduleInterval time.Duration `yaml:"scheduleInterval"` // 调度间隔
	RenewInterval    time.Duration `yaml:"renewInterval"`    // 续约间隔
}

// NewScheduler 创建调度器实例
func NewScheduler(
	nodeID string,
	executionResultHandlers *syncx.Map[int64, runner.TaskExecutionStateHandler],
	runner runner.Runner,
	taskSvc task.Service,
	acquirer acquirer.TaskAcquirer,
	consumers map[string]*event.Consumer,
	grpcClients *grpc.Clients[executorv1.ExecutorServiceClient],
	config Config,
) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &Scheduler{
		nodeID:                 nodeID,
		executionStateHandlers: executionResultHandlers,
		runner:                 runner,
		taskSvc:                taskSvc,
		acquirer:               acquirer,
		consumers:              consumers,
		grpcClients:            grpcClients,
		config:                 config,
		ctx:                    ctx,
		cancel:                 cancel,
		logger:                 elog.DefaultLogger.With(elog.FieldComponentName("Scheduler")),
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

	// 启动续约循环
	go s.renewLoop()

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
	for {

		if s.ctx.Err() != nil {
			s.logger.Info("调度循环结束")
			return
		}

		s.logger.Info("开始一次调度")

		// 获取可调度的任务列表
		ctx, cancelFunc := context.WithTimeout(s.ctx, s.config.BatchTimeout)
		tasks, err := s.taskSvc.SchedulableTasks(ctx, s.config.PreemptedTimeout.Milliseconds(), s.config.BatchSize)
		cancelFunc()
		if err != nil {
			s.logger.Error("获取可调度任务失败", elog.FieldErr(err))
		}

		// 没有可以调度的任务就睡一会
		if len(tasks) == 0 {
			s.logger.Info("没有可调度的任务")
			time.Sleep(s.config.ScheduleInterval)
			continue
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
		s.logger.Info("本次调度信息",
			elog.Int("success", success),
			elog.Int("total", len(tasks)))
	}
}

// renewLoop 续约循环
func (s *Scheduler) renewLoop() {
	ticker := time.NewTicker(s.config.RenewInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			err := s.acquirer.Renew(s.ctx, s.nodeID)
			if err != nil {
				s.logger.Error("批量续约失败", elog.FieldErr(err))
			}
		}
	}
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
func (s *Scheduler) HandleReports(ctx context.Context, reports []*domain.Report) error {
	if len(reports) == 0 {
		return nil
	}
	s.logger.Debug("开始处理执行状态上报", elog.Int("count", len(reports)))

	var err error
	processedCount := 0
	skippedCount := 0

	for i := range reports {
		err1 := s.handleTaskExecutionState(ctx, reports[i].ExecutionState)
		if err1 != nil {
			skippedCount++
			s.logger.Error("处理执行节点上报的结果失败",
				elog.Any("result", reports[i].ExecutionState),
				elog.FieldErr(err1))
			// 包装错误，添加上报场景的特定信息
			err = multierr.Append(err,
				fmt.Errorf("处理执行节点上报的结果失败: taskID=%d, executionID=%d: %w",
					reports[i].ExecutionState.TaskID, reports[i].ExecutionState.ID, err1))
			continue
		}
		processedCount++
	}

	// 记录处理统计信息
	s.logger.Info("执行状态上报处理完成",
		elog.Int("total", len(reports)),
		elog.Int("processed", processedCount),
		elog.Int("skipped", skippedCount))
	return err
}

func (s *Scheduler) handleTaskExecutionState(ctx context.Context, state domain.ExecutionState) error {
	handler, exists := s.executionStateHandlers.Load(state.ID)
	if !exists || handler == nil {
		return errs.ErrExecutionStateHandlerNotFound
	}
	return handler(ctx, state)
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

// InterruptTaskExecution 中断任务执行
func (s *Scheduler) InterruptTaskExecution(ctx context.Context, execution domain.TaskExecution) error {
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
	if !resp.GetSuccess() {
		// 中断失败，忽略状态
		return errs.ErrInterruptTaskExecutionFailed
	}
	return s.handleTaskExecutionState(ctx, domain.ExecutionStateFromProto(resp.GetExecutionState()))
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
