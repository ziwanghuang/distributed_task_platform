package runner

import (
	"context"
	"fmt"
	"time"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/errs"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"gitee.com/flycash/distributed_task_platform/pkg/acquirer"
	"gitee.com/flycash/distributed_task_platform/pkg/executor"
	"github.com/ecodeclub/ekit/syncx"
	"github.com/gotomicro/ego/core/elog"
	"golang.org/x/sync/semaphore"
)

// Result 表示运行结果
type Result struct {
	State domain.ExecutionState
	Err   error
}

// TaskExecutionContext 执行上下文，包含TaskExecution和相关通道
type TaskExecutionContext struct {
	Execution  *domain.TaskExecution
	ResultChan chan<- *Result
}

type TaskExecutionResultHandleFunc func(ctx context.Context, result *Result, execution *domain.TaskExecution) (stop bool)

type Base struct {
	nodeID           string                                   // 当前调度节点ID
	tokens           *semaphore.Weighted                      // 令牌
	remoteExecutions *syncx.Map[int64, *TaskExecutionContext] // 正在执行的TaskExecution及其上下文
	taskSvc          task.Service                             // 任务服务
	execSvc          task.ExecutionService                    // 任务执行服务
	taskAcquirer     acquirer.TaskAcquirer                    // 任务抢占器
	executors        map[string]executor.Executor             // 执行器

	tokenAcquireTimeout time.Duration // 抢令牌时的超时
	renewInterval       time.Duration // 续约间隔

	logger *elog.Component
}

func NewBase(
	nodeID string,
	tokens *semaphore.Weighted,
	remoteExecutions *syncx.Map[int64, *TaskExecutionContext],
	taskSvc task.Service,
	execSvc task.ExecutionService,
	taskAcquirer acquirer.TaskAcquirer,
	executors map[string]executor.Executor,
	tokenAcquireTimeout,
	renewInterval time.Duration,
) *Base {
	return &Base{nodeID: nodeID, tokens: tokens, remoteExecutions: remoteExecutions, taskSvc: taskSvc, execSvc: execSvc, taskAcquirer: taskAcquirer, executors: executors, tokenAcquireTimeout: tokenAcquireTimeout, renewInterval: renewInterval}
}

// acquireTokenAndTask 抢令牌 + 抢占任务
func (s *Base) acquireTokenAndTask(ctx context.Context, task domain.Task) (*domain.Task, error) {
	// 抢令牌
	tokenCtx, tokenCancel := context.WithTimeout(ctx, s.tokenAcquireTimeout)
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

func (s *Base) handleTaskExecution(ctx context.Context, execution domain.TaskExecution, handleFunc TaskExecutionResultHandleFunc) {
	s.logger.Info("开始处理任务",
		elog.Int64("taskID", execution.Task.ID),
		elog.String("taskName", execution.Task.Name))

	// 续约定时器
	renewTicker := time.NewTicker(s.renewInterval)
	defer renewTicker.Stop()

	// 确保最后释放任务和清理缓存
	defer func() {
		s.releaseTokenAndTask(ctx, execution.Task)
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
	resultChan := make(chan *Result, resultChanBufferSize)

	// 远程任务需要轮询
	if execution.Task.ExecutionMethod.IsRemote() {
		s.remoteExecutions.Store(execution.ID, &TaskExecutionContext{
			Execution: &execution,
			// 远程任务通过这个通道传递结果，主要路径有
			// 下方 selectedExecutor 执行
			// 调度节点的轮询协程定期主动轮询
			// 执行节点通过GPRC或kafka方式主动上报
			ResultChan: resultChan,
		})
		defer s.remoteExecutions.Delete(execution.ID)
	}

	// 启动执行任务
	go func() {
		state, err := selectedExecutor.Run(ctx, execution)
		resultChan <- &Result{State: state, Err: err}
	}()

	for {
		select {
		case res := <-resultChan:
			// 执行失败，执行状态未更新
			if res.Err != nil {
				s.logger.Error("任务执行失败",
					elog.Int64("taskID", execution.Task.ID),
					elog.String("taskName", execution.Task.Name),
					elog.FieldErr(res.Err))
				return
			}
			// 处理结果
			stop := handleFunc(ctx, res, &execution)
			if stop {
				return
			}
		case <-renewTicker.C:
			// 续约
			renewedTask, err := s.taskAcquirer.Renew(ctx, execution.Task.ID, s.nodeID)
			if err != nil {
				s.logger.Error("任务续约失败",
					elog.Int64("taskID", execution.Task.ID),
					elog.String("taskName", execution.Task.Name),
					elog.FieldErr(err))

				// 续约失败，意味着有人在调度了，啥也不干，只更新execution状态
				err = s.execSvc.UpdateStatusAndProgressAndEndTime(ctx, execution.ID, domain.TaskExecutionStatusFailedPreempted, execution.RunningProgress, time.Now().UnixMilli())
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

		case <-ctx.Done():
			return
		}
	}
}

// releaseTokenAndTask 释放令牌和任务锁
func (s *Base) releaseTokenAndTask(ctx context.Context, task domain.Task) {
	s.tokens.Release(1)
	if err := s.taskAcquirer.Release(ctx, task.ID, s.nodeID); err != nil {
		s.logger.Error("释放任务失败",
			elog.Int64("taskID", task.ID),
			elog.String("taskName", task.Name),
			elog.FieldErr(err))
	}
}

func (s *Base) setRunningState(ctx context.Context, res *Result, execution *domain.TaskExecution) (stop bool) {
	s.logger.Info("设置RUNNING状态",
		elog.Int64("executionID", res.State.ID),
		elog.String("currentStatus", execution.Status.String()),
		elog.String("targetStatus", res.State.Status.String()))
	// PREPARE → RUNNING
	// FAILED_RETRYABLE → RUNNING
	err := s.execSvc.SetRunningState(ctx, res.State.ID, res.State.RunningProgress)
	if err != nil {
		return false
	}
	// 更新内存中信息
	execution.Status = res.State.Status
	execution.RunningProgress = res.State.RunningProgress
	return false
}

func (s *Base) updateRunningProgress(ctx context.Context, res *Result, execution *domain.TaskExecution) (stop bool) {
	// RUNNING → RUNNING：更新进度
	err := s.execSvc.UpdateRunningProgress(ctx, res.State.ID, res.State.RunningProgress)
	if err != nil {
		return false
	}
	// 更新内存中信息
	execution.Status = res.State.Status
	execution.RunningProgress = res.State.RunningProgress
	return false
}

func (s *Base) updateTaskState(ctx context.Context, res *Result, execution *domain.TaskExecution) {
	// 更新调度参数
	if res.State.RequestReschedule {
		if updated, err1 := s.taskSvc.UpdateScheduleParams(ctx, execution.Task, res.State.RescheduleParams); err1 != nil {
			s.logger.Error("更新任务调度参数失败",
				elog.Int64("taskID", execution.Task.ID),
				elog.String("taskName", execution.Task.Name),
				elog.Any("state", res.State),
				elog.FieldErr(err1))
		} else {
			execution.Task = updated
		}
	}

	// 更新任务执行时间
	if _, err2 := s.taskSvc.UpdateNextTime(ctx, execution.Task); err2 != nil {
		s.logger.Error("更新任务下次执行时间失败",
			elog.Int64("taskID", execution.Task.ID),
			elog.String("taskName", execution.Task.Name),
			elog.FieldErr(err2))
	}
}
