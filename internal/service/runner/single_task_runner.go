package runner

import (
	"context"
	"fmt"
	"time"

	"gitee.com/flycash/distributed_task_platform/internal/service/acquirer"
	"gitee.com/flycash/distributed_task_platform/internal/service/invoker"

	"gitee.com/flycash/distributed_task_platform/internal/event"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"gitee.com/flycash/distributed_task_platform/pkg/retry"
	"github.com/gotomicro/ego/core/elog"
)

var _ Runner = &SingleTaskRunner{}

type SingleTaskRunner struct {
	nodeID       string                 // 当前调度节点ID
	taskSvc      task.Service           // 任务服务
	execSvc      task.ExecutionService  // 任务执行服务
	taskAcquirer acquirer.TaskAcquirer  // 任务抢占器
	invoker      invoker.Invoker        // 这里一般来说就是 invoker.Dispatcher
	producer     event.CompleteProducer // 任务完成事件生产者

	logger *elog.Component
}

func NewSingleTaskRunner(
	nodeID string,
	taskSvc task.Service,
	execSvc task.ExecutionService,
	taskAcquirer acquirer.TaskAcquirer,
	invoker invoker.Invoker,
	producer event.CompleteProducer,
) *SingleTaskRunner {
	return &SingleTaskRunner{
		nodeID:       nodeID,
		taskSvc:      taskSvc,
		execSvc:      execSvc,
		taskAcquirer: taskAcquirer,
		invoker:      invoker,
		producer:     producer,
		logger:       elog.DefaultLogger.With(elog.FieldComponentName("runner.SingleTaskRunner")),
	}
}

func (s *SingleTaskRunner) Run(ctx context.Context, task domain.Task) error {
	// 抢占任务
	acquiredTask, err := s.acquireTask(ctx, task)
	if err != nil {
		s.logger.Error("任务抢占失败",
			elog.Int64("taskID", task.ID),
			elog.String("taskName", task.Name),
			elog.FieldErr(err))
		return err
	}
	// 这里使用传入的execid，因为execid还没有入库。
	acquiredTask.PlanExecID = task.PlanExecID
	// 根据分片规则区分任务类型
	if acquiredTask.ShardingRule == nil {
		return s.handleNormalTask(ctx, acquiredTask)
	} else {
		return s.handleShardingTask(ctx, acquiredTask)
	}
}

func (s *SingleTaskRunner) handleNormalTask(ctx context.Context, task domain.Task) error {
	// 抢占成功，立即创建TaskExecution记录
	execution, err := s.execSvc.Create(ctx, domain.TaskExecution{
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
		// 释放任务
		s.releaseTask(ctx, task)
		return err
	}

	// 抢占和创建都成功，异步触发任务
	go func() {
		// 执行任务
		result, err1 := s.invoker.Run(ctx, execution)
		if err1 != nil {
			s.logger.Error("执行器执行任务失败", elog.FieldErr(err))
			return
		}

		// 执行任务，并传入上下文
		// result, err1 := s.invoker.Run(ctx, execution, map[string]string{
		// 	"type": domain.ExecutionTypeNormal.String(),
		// })

		err1 = s.execSvc.HandleState(ctx, result)
		if err1 != nil {
			s.logger.Error("正常调度任务失败",
				elog.Any("execution", execution),
				elog.FieldErr(err1))
		}
	}()
	return nil
}

func (s *SingleTaskRunner) handleShardingTask(ctx context.Context, task domain.Task) error {
	// 抢占成功，立即创建子任务执行记录
	executions, err := s.execSvc.CreateShardingChildren(ctx, domain.TaskExecution{
		Task: task,
		// 可以认为开始执行了，防止执行节点直接返回"终态"状态Failed，Success等
		StartTime: time.Now().UnixMilli(),
		Status:    domain.TaskExecutionStatusRunning,
	})
	if err != nil {
		s.logger.Error("创建子任务执行记录失败",
			elog.Int64("taskID", task.ID),
			elog.String("taskName", task.Name),
			elog.FieldErr(err))
		// 释放任务
		s.releaseTask(ctx, task)
		return err
	}

	// 抢占和创建都成功，异步触发任务
	for i := range executions {
		execution := executions[i]
		go func() {
			// 执行任务
			result, err1 := s.invoker.Run(ctx, execution)
			if err1 != nil {
				s.logger.Error("执行器执行任务失败", elog.FieldErr(err))
				return
			}
			err1 = s.execSvc.HandleState(ctx, result)
			if err1 != nil {
				s.logger.Error("正常调度子任务失败",
					elog.Any("execution", execution),
					elog.FieldErr(err1))
			}
		}()
	}
	return nil
}

// acquireTask 抢占任务
func (s *SingleTaskRunner) acquireTask(ctx context.Context, task domain.Task) (domain.Task, error) {
	// 抢占任务
	acquiredTask, err := s.taskAcquirer.Acquire(ctx, task.ID, s.nodeID)
	if err != nil {
		return domain.Task{}, fmt.Errorf("任务抢占失败: %w", err)
	}
	// 抢占成功
	return acquiredTask, nil
}

// releaseTask 释放任务
func (s *SingleTaskRunner) releaseTask(ctx context.Context, task domain.Task) {
	if err := s.taskAcquirer.Release(ctx, task.ID, s.nodeID); err != nil {
		s.logger.Error("释放任务失败",
			elog.Int64("taskID", task.ID),
			elog.String("taskName", task.Name),
			elog.FieldErr(err))
	}
}

func (s *SingleTaskRunner) Retry(ctx context.Context, execution domain.TaskExecution) error {
	// 是否有重试配置
	if execution.Task.RetryConfig == nil {
		execution.RetryCount++
		_ = s.execSvc.UpdateRetryResult(ctx,
			execution.ID,
			execution.RetryCount,
			execution.NextRetryTime,
			domain.TaskExecutionStatusFailed,
			execution.RunningProgress,
			time.Now().UnixMilli(),
			execution.Task.ScheduleParams)
		return fmt.Errorf("任务重试配置为空")
	}

	// 是否能够正常初始化重试器
	_, err := retry.NewRetry(execution.Task.RetryConfig.ToRetryComponentConfig())
	if err != nil {
		execution.RetryCount++
		_ = s.execSvc.UpdateRetryResult(ctx,
			execution.ID,
			execution.RetryCount,
			execution.NextRetryTime,
			domain.TaskExecutionStatusFailed,
			execution.RunningProgress,
			time.Now().UnixMilli(),
			execution.Task.ScheduleParams)
		return fmt.Errorf("创建重试策略失败: %w", err)
	}

	// 抢占任务
	acquiredTask, err := s.acquireTask(ctx, execution.Task)
	if err != nil {
		s.logger.Error("重试：任务抢占失败",
			elog.Int64("taskID", execution.Task.ID),
			elog.String("taskName", execution.Task.Name),
			elog.FieldErr(err))
		return err
	}
	execution.Task = acquiredTask

	// 抢占和创建都成功，异步触发任务
	go func() {
		// 执行任务
		result, err1 := s.invoker.Run(ctx, execution)
		if err1 != nil {
			s.logger.Error("执行器执行任务失败", elog.FieldErr(err))
			return
		}

		// 执行任务，并传入上下文
		// result, err1 := s.invoker.Run(ctx, execution, map[string]string{
		// 	"type": domain.ExecutionTypeRetry.String(),
		// })

		err1 = s.execSvc.HandleState(ctx, result)
		if err1 != nil {
			s.logger.Error("重试任务失败",
				elog.Any("execution", execution),
				elog.FieldErr(err1))
		}

		// err1 = s.retryExecutionStateHandler(ctx, result, retryStrategy)
		// if err1 != nil {
		// 	s.logger.Error("重试任务失败",
		// 		elog.Any("execution", execution),
		// 		elog.FieldErr(err1))
		// }
	}()
	return nil
}

func (s *SingleTaskRunner) Reschedule(ctx context.Context, execution domain.TaskExecution) error {
	// 抢占任务
	acquiredTask, err := s.acquireTask(ctx, execution.Task)
	if err != nil {
		s.logger.Error("重调度：任务抢占失败",
			elog.Int64("taskID", execution.Task.ID),
			elog.String("taskName", execution.Task.Name),
			elog.FieldErr(err))
		return err
	}
	execution.Task = acquiredTask

	// 抢占和创建都成功，异步触发任务
	go func() {
		// 执行任务
		result, err1 := s.invoker.Run(ctx, execution)
		if err1 != nil {
			s.logger.Error("执行器执行任务失败", elog.FieldErr(err))
			return
		}

		// 执行任务，并传入上下文
		// result, err1 := s.invoker.Run(ctx, execution, map[string]string{
		// 	"type": domain.ExecutionTypeReschedule.String(),
		// })

		err1 = s.execSvc.HandleState(ctx, result)
		if err1 != nil {
			s.logger.Error("重调度任务失败",
				elog.Any("execution", execution),
				elog.FieldErr(err1))
		}

		// err1 = s.rescheduleExecutionStateHandler(ctx, result)
		// if err1 != nil {
		// 	s.logger.Error("正常调度任务失败",
		// 		elog.Any("execution", execution),
		// 		elog.FieldErr(err1))
		// }
	}()
	return nil
}
