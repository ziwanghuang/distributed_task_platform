package scheduler

import (
	"context"
	"fmt"
	"time"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	v2 "gitee.com/flycash/distributed_task_platform/scheduler/job/v2"
	"github.com/gotomicro/ego/core/elog"
	"github.com/gotomicro/ego/core/standard"
)

var _ standard.Component = &Scheduler{}

// Scheduler 分布式任务调度器
type Scheduler struct {
	nodeID       string       // 当前调度节点ID
	svc          task.Service // 任务服务
	taskAcquirer TaskAcquirer // 任务抢占器
	jobManager   *v2.Manager  // 任务管理器
	config       *Config      // 配置
	ctx          context.Context
	cancel       context.CancelFunc
	logger       *elog.Component
}

// Config 调度器配置
type Config struct {
	BatchTimeout     time.Duration
	BatchSize        int           // 批量获取任务数量
	ScheduleInterval time.Duration // 调度间隔
	RenewInterval    time.Duration // 续约间隔
}

// NewScheduler 创建调度器实例
func NewScheduler(
	nodeID string,
	svc task.Service,
	acquirer TaskAcquirer,
	taskManager *v2.Manager,
	config *Config,
) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &Scheduler{
		nodeID:       nodeID,
		svc:          svc,
		taskAcquirer: acquirer,
		jobManager:   taskManager,
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
	tasks, err := s.svc.SchedulableTasks(ctx, s.config.BatchSize)
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

	// 确保最后释放任务
	defer func() {
		if err := s.taskAcquirer.Release(s.ctx, task); err != nil {
			s.logger.Error("释放任务失败",
				elog.Int64("taskID", task.ID),
				elog.String("taskName", task.Name),
				elog.FieldErr(err))
		}
	}()

	// 启动任务执行
	resultChan := s.jobManager.Run(s.ctx, task)

	// 续约定时器
	renewTicker := time.NewTicker(s.config.RenewInterval)
	defer renewTicker.Stop()

	for {
		select {
		case err := <-resultChan:
			// 任务完成
			if err != nil {
				s.logger.Error("任务执行失败",
					elog.Int64("taskID", task.ID),
					elog.String("taskName", task.Name),
					elog.FieldErr(err))
				return
			}
			s.logger.Info("任务执行成功",
				elog.Int64("taskID", task.ID),
				elog.String("taskName", task.Name))
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

	// 关闭任务管理器
	// if err := s.jobManager.Close(); err != nil {
	// 	s.logger.Warn("关闭任务管理器失败", elog.FieldErr(err))
	// 	return err
	// }
	return nil
}
