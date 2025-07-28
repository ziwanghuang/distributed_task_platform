package scheduler

import (
	"context"
	"fmt"
	"gitee.com/flycash/distributed_task_platform/pkg/loopjob"
	"gitee.com/flycash/distributed_task_platform/pkg/sharding"
	"github.com/meoying/dlock-go"
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

var _ server.Server = &SchedulerV2{}

// SchedulerV2 分布式任务调度器
type SchedulerV2 struct {
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
	dclient            dlock.Client
	sem                loopjob.ResourceSemaphore
	taskStr            sharding.ShardingStrategy
}



// NewSchedulerV2 创建调度器实例
func NewSchedulerV2(
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
	dlockClient dlock.Client,
	sem loopjob.ResourceSemaphore,
) *SchedulerV2 {
	ctx, cancel := context.WithCancel(context.Background())
	return &SchedulerV2{
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
		dclient:            dlockClient,
		logger:             elog.DefaultLogger.With(elog.FieldComponentName("SchedulerV2")),
		sem:                sem,
	}
}

func (s *SchedulerV2) NodeID() string {
	return s.nodeID
}

func (s *SchedulerV2) Name() string {
	return fmt.Sprintf("SchedulerV2-%s", s.nodeID)
}

func (s *SchedulerV2) PackageName() string {
	return "scheduler.SchedulerV2"
}

func (s *SchedulerV2) Init() error {
	return nil
}

// Start 启动调度器
func (s *SchedulerV2) Start() error {
	s.logger.Info("启动分布式任务调度器", elog.String("nodeID", s.nodeID))
	const scheduleKey = "scheduleKey"
	// 改成分库调度
	go loopjob.NewShardingLoopJob(s.dclient, scheduleKey, s.scheduleOneLoop, s.taskStr, s.sem)

	// 启动续约循环
	go s.renewLoop()
	return nil
}

func (s *SchedulerV2) scheduleOneLoop(ctx context.Context) error {
	if ctx.Err() != nil {
		s.logger.Info("调度循环结束")
		return nil
	}

	stopRecordExecutionTimeFunc := s.metrics.StartRecordExecutionTime()
	s.logger.Info("开始一次调度")

	// 获取可调度的任务列表
	scheduleCtx, cancelFunc := context.WithTimeout(ctx, s.config.BatchTimeout)
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
		return nil
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
	}
	return nil
}

func (s *SchedulerV2) newContext(task domain.Task) context.Context {
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
func (s *SchedulerV2) renewLoop() {
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
func (s *SchedulerV2) Stop() error {
	s.logger.Info("停止分布式任务调度器", elog.String("nodeID", s.nodeID))
	// 取消上下文
	s.cancel()
	return nil
}

func (s *SchedulerV2) GracefulStop(_ context.Context) error {
	s.logger.Info("停止分布式任务调度器", elog.String("nodeID", s.nodeID))
	s.cancel()
	return nil
}

func (s *SchedulerV2) Info() *server.ServiceInfo {
	info := server.ApplyOptions(
		server.WithName(s.Name()),
		server.WithKind(constant.ServiceProvider),
	)
	info.Healthy = s.ctx.Err() == nil
	return &info
}
