package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/errs"
	"gitee.com/flycash/distributed_task_platform/internal/event"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"gitee.com/flycash/distributed_task_platform/scheduler/job"
	"github.com/ecodeclub/mq-api"
	"github.com/gotomicro/ego/core/elog"
	"github.com/gotomicro/ego/core/standard"
)

var _ standard.Component = &Scheduler{}

// Scheduler 分布式任务调度器
type Scheduler struct {
	nodeID       string                     // 当前调度节点ID
	svc          task.Service               // 任务服务
	taskAcquirer TaskAcquirer               // 任务抢占器
	jobManager   *job.Manager               // 任务管理器
	consumers    map[string]*event.Consumer // 消费者
	config       *Config                    // 配置
	ctx          context.Context
	cancel       context.CancelFunc
	logger       *elog.Component
}

// Config 调度器配置
type Config struct {
	BatchTimeout     time.Duration
	BatchSize        int           // 批量获取任务数量
	PreemptedTimeout time.Duration // 表示处于 PREEMPTED 状态任务的超时时间（毫秒）
	ScheduleInterval time.Duration // 调度间隔
	RenewInterval    time.Duration // 续约间隔
}

// NewScheduler 创建调度器实例
func NewScheduler(
	nodeID string,
	svc task.Service,
	acquirer TaskAcquirer,
	taskManager *job.Manager,
	consumers map[string]*event.Consumer,
	config *Config,
) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &Scheduler{
		nodeID:       nodeID,
		svc:          svc,
		taskAcquirer: acquirer,
		jobManager:   taskManager,
		consumers:    consumers,
		config:       config,
		ctx:          ctx,
		cancel:       cancel,
		logger:       elog.DefaultLogger.With(elog.FieldComponentName("scheduler.Scheduler")),
	}
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

	// 初始化推送消息消费者
	var err error
	for key := range s.consumers {
		switch key {
		case "executionReport":
			err = s.consumers[key].Start(s.ctx, s.consumeExecutionReportEvent)
			if err != nil {
				return err
			}
		case "executionBatchReport":
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
	ticker := time.NewTicker(s.config.ScheduleInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
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
	tasks, err := s.svc.SchedulableTasks(ctx, s.config.PreemptedTimeout.Milliseconds(), s.config.BatchSize)
	cancelFunc()
	if err != nil {
		return fmt.Errorf("获取可调度任务失败: %w", err)
	}
	if len(tasks) == 0 {
		s.logger.Debug("没有可调度的任务")
		return nil
	}

	s.logger.Info("发现可调度任务", elog.Int("count", len(tasks)))

	// 遍历任务，尝试抢占并启动
	acquired := 0
	for i := range tasks {
		// 填充调度节点ID
		tasks[i].ScheduleNodeID = s.nodeID
		if err = s.taskAcquirer.Acquire(s.ctx, tasks[i]); err != nil {
			s.logger.Debug("任务抢占失败",
				elog.Int64("taskID", tasks[i].ID),
				elog.String("taskName", tasks[i].Name),
				elog.FieldErr(err))
			continue
		}

		// 抢占成功，启动异步任务管理
		acquired++
		go s.handleAcquiredTask(tasks[i])
	}

	s.logger.Info("抢占任务信息",
		elog.Int("acquired", acquired),
		elog.Int("total", len(tasks)))
	return nil
}

// handleAcquiredTask 处理已抢占的任务
func (s *Scheduler) handleAcquiredTask(task domain.Task) {
	s.logger.Info("开始处理任务",
		elog.Int64("taskID", task.ID),
		elog.String("taskName", task.Name))

	// 确保最后释放任务和清理缓存
	defer func() {
		if err := s.taskAcquirer.Release(s.ctx, task); err != nil {
			s.logger.Error("释放任务失败",
				elog.Int64("taskID", task.ID),
				elog.String("taskName", task.Name),
				elog.FieldErr(err))
		}
	}()

	// 启动任务执行
	chans, cleanup, err := s.jobManager.Run(s.ctx, task)
	if err != nil {
		s.logger.Error("运行任务失败",
			elog.Int64("taskID", task.ID),
			elog.String("taskName", task.Name),
			elog.FieldErr(err))
		return
	}
	defer cleanup()

	// 续约定时器
	renewTicker := time.NewTicker(s.config.RenewInterval)
	defer renewTicker.Stop()

	for {
		select {
		case err := <-chans.Error:
			// 任务完成
			if err != nil {
				s.logger.Error("任务执行失败",
					elog.Int64("taskID", task.ID),
					elog.String("taskName", task.Name),
					elog.FieldErr(err))
			} else {
				s.logger.Info("任务执行成功",
					elog.Int64("taskID", task.ID),
					elog.String("taskName", task.Name))
			}

			// 更新下次执行时间
			err = s.svc.UpdateNextTime(s.ctx, task)
			if err != nil {
				s.logger.Error("更新下次执行时间失败",
					elog.Int64("taskID", task.ID),
					elog.String("taskName", task.Name),
					elog.FieldErr(err))
			}
			return
		case <-renewTicker.C:
			// 续约
			if err := s.taskAcquirer.Renew(s.ctx, task); err != nil {
				s.logger.Error("任务续约失败",
					elog.Int64("taskID", task.ID),
					elog.String("taskName", task.Name),
					elog.FieldErr(err))

				// 续约失败，通知任务管理器
				_ = s.jobManager.RenewFailed(s.ctx, task)
				return
			}
			s.logger.Debug("任务续约成功",
				elog.Int64("taskID", task.ID),
				elog.String("taskName", task.Name))
		case <-s.ctx.Done():
			// 调度器停止
			return
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

func (s *Scheduler) HandleReports(ctx context.Context, reposts []*domain.Report) error {
	// for i := range reposts {
	// 	if reposts[i].RequestReschedule {
	// 		go s.RescheduleNow(ctx, reposts[i].TaskID, reposts[i].RescheduleParams)
	// 	}
	// }
	return s.jobManager.HandleReports(ctx, reposts)
}

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

func (s *Scheduler) RescheduleNow(ctx context.Context, taskID int64, rescheduleParams map[string]string) {
	tk, err := s.svc.GetByID(ctx, taskID)
	if err != nil {
		s.logger.Error("查找Task失败", elog.FieldErr(err))
		return
	}
	// 更新调度参数
	err = s.svc.UpdateScheduleParams(ctx, tk.ID, tk.Version, rescheduleParams)
	if err != nil {
		s.logger.Error("更新调度信息失败", elog.FieldErr(err))
		return
	}
	// 开始抢占并调度
	tk.ScheduleNodeID = s.nodeID
	err = s.taskAcquirer.Acquire(ctx, tk)
	if err != nil {
		s.logger.Debug("重调度任务抢占失败",
			elog.Int64("taskID", tk.ID),
			elog.String("taskName", tk.Name),
			elog.FieldErr(err))
		return
	}
	go s.handleAcquiredTask(tk)
}
