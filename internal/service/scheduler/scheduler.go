package scheduler

import (
	"context"
	"fmt"
	"time"

	"gitee.com/flycash/distributed_task_platform/internal/service/acquirer"

	executorv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/executor/v1"
	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/errs"
	"gitee.com/flycash/distributed_task_platform/internal/service/picker"
	"gitee.com/flycash/distributed_task_platform/internal/service/runner"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"gitee.com/flycash/distributed_task_platform/pkg/grpc"
	"gitee.com/flycash/distributed_task_platform/pkg/grpc/balancer"
	"gitee.com/flycash/distributed_task_platform/pkg/loadchecker"
	"gitee.com/flycash/distributed_task_platform/pkg/prometheus"
	"github.com/gotomicro/ego/core/constant"
	"github.com/gotomicro/ego/core/elog"
	"github.com/gotomicro/ego/server"
)

var _ server.Server = &Scheduler{}

// Scheduler 分布式任务调度器
type Scheduler struct {
	nodeID             string                                            // 当前调度节点ID
	runner             runner.Runner                                     // Runner 分发器
	taskSvc            task.Service                                      // 任务服务
	execSvc            task.ExecutionService                             // 任务执行服务
	acquirer           acquirer.TaskAcquirer                             // 任务抢占、续约、释放器
	grpcClients        *grpc.ClientsV2[executorv1.ExecutorServiceClient] // gRPC客户端池
	config             Config                                            // 配置
	loadChecker        loadchecker.LoadChecker                           // 负载检查器
	metrics            *prometheus.SchedulerMetrics                      // 指标收集器
	executorNodePicker picker.ExecutorNodePicker                         // 智能节点选择器
	ctx                context.Context
	cancel             context.CancelFunc
	logger             *elog.Component
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
	runner runner.Runner,
	taskSvc task.Service,
	execSvc task.ExecutionService,
	acquirer acquirer.TaskAcquirer,
	grpcClients *grpc.ClientsV2[executorv1.ExecutorServiceClient],
	config Config,
	loadChecker loadchecker.LoadChecker,
	metrics *prometheus.SchedulerMetrics,
	executorNodePicker picker.ExecutorNodePicker,
) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &Scheduler{
		nodeID:             nodeID,
		runner:             runner,
		taskSvc:            taskSvc,
		execSvc:            execSvc,
		acquirer:           acquirer,
		grpcClients:        grpcClients,
		config:             config,
		loadChecker:        loadChecker,
		metrics:            metrics,
		executorNodePicker: executorNodePicker,
		ctx:                ctx,
		cancel:             cancel,
		logger:             elog.DefaultLogger.With(elog.FieldComponentName("Scheduler")),
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
	return nil
}

// scheduleLoop 主调度循环
func (s *Scheduler) scheduleLoop() {
	for {
		if s.ctx.Err() != nil {
			s.logger.Info("调度循环结束")
			return
		}

		stopRecordExecutionTimeFunc := s.metrics.StartRecordExecutionTime()
		s.logger.Info("开始一次调度")

		// 获取可调度的任务列表
		scheduleCtx, cancelFunc := context.WithTimeout(s.ctx, s.config.BatchTimeout)
		tasks, err := s.taskSvc.SchedulableTasks(scheduleCtx, s.config.PreemptedTimeout.Milliseconds(), s.config.BatchSize)
		cancelFunc()
		if err != nil {
			s.logger.Error("获取可调度任务失败", elog.FieldErr(err))
		}
		// 没有可以调度的任务就睡一会
		if len(tasks) == 0 {
			s.logger.Info("没有可调度的任务")
			// 结束计时
			stopRecordExecutionTimeFunc()
			// 睡眠一下
			time.Sleep(s.config.ScheduleInterval)
			continue
		}

		s.logger.Info("发现可调度任务", elog.Int("count", len(tasks)))
		// 开始调度
		successCount := 0
		for i := range tasks {
			err1 := s.runner.Run(s.newContext(tasks[i].ID), tasks[i])
			if err1 != nil {
				s.logger.Error("调度任务失败",
					elog.Int64("taskID", tasks[i].ID),
					elog.String("taskName", tasks[i].Name),
					elog.FieldErr(err1))
			} else {
				successCount++
			}
		}
		s.logger.Info("本次调度信息",
			elog.Int("success", successCount),
			elog.Int("total", len(tasks)))

		// 负载检查
		duration, ok := s.loadChecker.Check(loadchecker.WithExecutionTime(s.ctx, stopRecordExecutionTimeFunc()))
		if !ok {
			s.logger.Info("负载过高，降低调度频率", elog.Duration("sleep", duration))
			s.metrics.RecordLoadCheckerSleep("composite", duration)
			time.Sleep(duration)
			continue
		}
	}
}

func (s *Scheduler) newContext(taskID int64) context.Context {
	// 使用智能调度选择执行节点
	if nodeID, err := s.executorNodePicker.Pick(s.ctx); err == nil && nodeID != "" {
		s.logger.Info("智能调度选择节点成功",
			elog.String("selectedNodeID", nodeID),
			elog.Int64("taskID", taskID))
		return balancer.WithSpecificNodeID(s.ctx, nodeID)
	} else {
		s.logger.Error("智能调度选择节点失败，使用默认调度",
			elog.Int64("taskID", taskID),
			elog.FieldErr(err))
		// 如果智能调度失败，继续使用原始 ctx（相当于随机选择)
		return s.ctx
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
	return s.execSvc.UpdateState(ctx, domain.ExecutionStateFromProto(resp.GetExecutionState()))
}

// Stop 停止调度器
func (s *Scheduler) Stop() error {
	s.logger.Info("停止分布式任务调度器", elog.String("nodeID", s.nodeID))
	// 取消上下文
	s.cancel()
	return nil
}

func (s *Scheduler) GracefulStop(_ context.Context) error {
	s.logger.Info("停止分布式任务调度器", elog.String("nodeID", s.nodeID))
	s.cancel()
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
