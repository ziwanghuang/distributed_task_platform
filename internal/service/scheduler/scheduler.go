package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"gitee.com/flycash/distributed_task_platform/internal/service/acquirer"

	executorv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/executor/v1"
	"gitee.com/flycash/distributed_task_platform/internal/domain"
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

// Scheduler 分布式任务调度器（V1 版本，单机全量调度）。
// 核心职责：
//  1. 定期从数据库拉取可调度任务（状态 = WAITING 且到达执行时间）
//  2. 通过 Runner 执行任务（抢占 → 创建执行记录 → 远程调用）
//  3. 通过 LoadChecker 动态调整调度频率，防止过载
//  4. 维护任务续约循环，防止长时间执行的任务被误判为超时
//
// 与 SchedulerV2 的区别：V1 是单机全量扫描，V2 支持分库分片调度。
type Scheduler struct {
	nodeID             string                                            // 当前调度节点ID，全局唯一标识
	runner             runner.Runner                                     // Runner 分发器，按任务类型路由到不同的执行器
	taskSvc            task.Service                                      // 任务服务，提供可调度任务查询
	execSvc            task.ExecutionService                             // 任务执行服务，管理执行记录
	acquirer           acquirer.TaskAcquirer                             // 任务抢占、续约、释放器
	grpcClients        *grpc.ClientsV2[executorv1.ExecutorServiceClient] // gRPC客户端池，连接执行节点
	config             Config                                            // 调度器配置（批次大小、超时、间隔等）
	loadChecker        loadchecker.LoadChecker                           // 负载检查器，动态调整调度节奏
	metrics            *prometheus.SchedulerMetrics                      // Prometheus 指标收集器
	executorNodePicker picker.ExecutorNodePicker                         // 智能节点选择器，基于 Prometheus 指标选择最优执行节点
	ctx                context.Context
	cancel             context.CancelFunc
	wg                 sync.WaitGroup // 追踪 scheduleLoop 和 renewLoop goroutine 的生命周期
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
		logger:             elog.DefaultLogger.With(elog.FieldComponentName("SchedulerV2")),
	}
}

func (s *Scheduler) NodeID() string {
	return s.nodeID
}

func (s *Scheduler) Name() string {
	return fmt.Sprintf("SchedulerV2-%s", s.nodeID)
}

func (s *Scheduler) PackageName() string {
	return "scheduler.SchedulerV2"
}

func (s *Scheduler) Init() error {
	return nil
}

// Start 启动调度器，开启两个核心后台循环：
//   - scheduleLoop: 主调度循环，定期拉取并执行任务
//   - renewLoop: 续约循环，定期为已抢占的任务续约防止超时
//
// 两个循环都受 ctx 控制，通过 WaitGroup 追踪生命周期。
func (s *Scheduler) Start() error {
	s.logger.Info("启动分布式任务调度器", elog.String("nodeID", s.nodeID))

	// 启动调度循环
	s.wg.Add(2)
	go func() {
		defer s.wg.Done()
		s.scheduleLoop()
	}()

	// 启动续约循环
	go func() {
		defer s.wg.Done()
		s.renewLoop()
	}()
	return nil
}

// scheduleLoop 主调度循环，是调度器的核心逻辑。
// 每轮循环的流程：
//  1. 检查 ctx 是否已取消（优雅停机信号）
//  2. 开始计时（用于 Prometheus 指标）
//  3. 带超时地从数据库批量拉取可调度任务
//  4. 如果无任务，sleep 后进入下一轮
//  5. 逐个任务调用 Runner.Run 执行（内部会抢占+异步调用执行节点）
//  6. 执行完毕后通过 LoadChecker 检查系统负载，必要时降频
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
			err1 := s.runner.Run(s.newContext(tasks[i]), tasks[i])
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

// newContext 为任务创建携带路由信息的 context。
// 通过 ExecutorNodePicker（基于 Prometheus 指标的智能选择器）为任务选择最优执行节点，
// 将节点 ID 注入 context 中，后续由负载均衡器的 Picker 消费。
// 如果智能选择失败（如 Prometheus 不可用），降级为轮询策略。
func (s *Scheduler) newContext(task domain.Task) context.Context {
	// 使用智能调度选择执行节点
	if nodeID, err := s.executorNodePicker.Pick(s.ctx, task); err == nil && nodeID != "" {
		s.logger.Info("智能调度选择节点成功",
			elog.String("selectedNodeID", nodeID),
			elog.Int64("taskID", task.ID))
		return balancer.WithSpecificNodeID(s.ctx, nodeID)
	} else {
		s.logger.Error("智能调度选择节点失败，使用默认调度",
			elog.Int64("taskID", task.ID),
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

// Stop 停止调度器
func (s *Scheduler) Stop() error {
	s.logger.Info("停止分布式任务调度器", elog.String("nodeID", s.nodeID))
	// 取消上下文
	s.cancel()
	return nil
}

// GracefulStop 优雅停止调度器。
// 先发送取消信号让 scheduleLoop 和 renewLoop 退出循环，
// 然后等待所有 goroutine 真正退出后才返回，确保不会在关停期间丢失正在处理的任务。
func (s *Scheduler) GracefulStop(_ context.Context) error {
	s.logger.Info("优雅停止分布式任务调度器", elog.String("nodeID", s.nodeID))
	s.cancel()
	s.wg.Wait() // 等待 scheduleLoop 和 renewLoop 退出
	s.logger.Info("分布式任务调度器已完全停止", elog.String("nodeID", s.nodeID))
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
