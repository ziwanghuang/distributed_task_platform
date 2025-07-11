package runner

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/event"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"github.com/gotomicro/ego/core/elog"
)

var errTaskHasAcquired = errors.New("task has acquired")

const (
	defaultRetrySleepTime = 500 * time.Millisecond
	defaultTimeout        = 5 * time.Second
)

type PlanRunner struct {
	planService task.PlanService
	*SingleTaskRunner
}

func (p *PlanRunner) Retry(_ context.Context, _ domain.TaskExecution) error {
	// TODO implement me
	panic("implement me")
}

func (p *PlanRunner) acquireTask(ctx context.Context, task domain.Task) (*domain.Task, error) {
	acquiredTask, err := p.taskAcquirer.Acquire(ctx, task.ID, p.nodeID)
	if err != nil {
		return nil, fmt.Errorf("任务抢占失败: %w", err)
	}
	return acquiredTask, nil
}

func (p *PlanRunner) releaseTask(ctx context.Context, task domain.Task) {
	if err := p.taskAcquirer.Release(ctx, task.ID, p.nodeID); err != nil {
		p.logger.Error("释放任务失败",
			elog.Int64("taskID", task.ID),
			elog.String("taskName", task.Name),
			elog.FieldErr(err))
	}
}

func (p *PlanRunner) Run(ctx context.Context, task domain.Task) error {
	// 只需要抢占任务就行
	ta, err := p.acquireTask(ctx, task)
	if err != nil {
		return err
	}
	// 抢占成功，立即创建TaskExecution记录
	exec, err := p.execSvc.Create(ctx, domain.TaskExecution{
		Task:      *ta,
		StartTime: time.Now().UnixMilli(),
		Status:    domain.TaskExecutionStatusRunning,
	})
	if err != nil {
		p.releaseTask(ctx, *ta)
		return err
	}
	plan, err := p.planService.GetPlan(ctx, task.ID)
	if err != nil {
		return err
	}
	// 找到入口任务
	rootTasks := plan.RootTask()
	for idx := range rootTasks {
		rootTask := rootTasks[idx]
		go p.run(ctx, rootTask.Task, exec.ID)
	}
	return nil
}

func (p *PlanRunner) NextStep(ctx context.Context, task domain.Task) error {
	plan, err := p.planService.GetPlan(ctx, task.PlanID)
	if err != nil {
		return err
	}
	planTask, ok := plan.GetTask(task.Name)
	if !ok {
		return fmt.Errorf("当前任务%s 不属于plan%s", task.Name, plan.Name)
	}
	// 获取后继节点
	tasks := planTask.NextStep()
	if len(tasks) == 0 {
		// 说明没有后继任务,发送plan的结束事件
		err = p.producer.Produce(ctx, event.Event{
			TaskID:         plan.ID,
			Version:        plan.Version,
			ScheduleNodeID: plan.ScheduleNodeID,
			ExecID:         plan.Execution.ID,
			Type:           domain.PlanTaskType,
			ExecStatus:     planTask.TaskExecution.Status,
		})
		if err != nil {
			elog.Error("发送结束plan事件失败", elog.FieldErr(err))
		}
		return err
	}
	//
	for idx := range tasks {
		nextPlanTask := tasks[idx]
		// 所有前驱任务都完成了就可以运行
		if nextPlanTask.CheckPre() {
			go p.run(ctx, nextPlanTask.Task, plan.Execution.ID)
		}
	}
	return nil
}

// 单个任务的逻辑：不断抢占，直至抢占成功或者被其他节点抢占。
func (p *PlanRunner) run(ctx context.Context, task domain.Task, planExecID int64) {
	exec, err := p.acquireLoop(ctx, task, planExecID)
	if err != nil {
		if errors.Is(err, errTaskHasAcquired) {
			// 说明其他节点已经运行不用返回报错了
			return
		}
		p.logger.Error("抢占失败", elog.Int64("taskID", task.ID), elog.FieldErr(err))
		return
	}
	p.handleTaskExecution(ctx, exec, p.handleSchedulingTaskExecutionFunc)
}

// 不断抢占直至成功
func (p *PlanRunner) acquireLoop(ctx context.Context, task domain.Task, planExecID int64) (domain.TaskExecution, error) {
	for {
		if ctx.Err() != nil {
			return domain.TaskExecution{}, ctx.Err()
		}
		nctx, cancel := context.WithTimeout(ctx, defaultTimeout)
		_, exec, err := p.acquireAndCreateExecution(nctx, task, planExecID)
		if err == nil {
			cancel()
			return exec, nil
		}
		// 没抢到，查看有没有被其他节点抢走了
		_, err = p.execSvc.FindExecutionByTaskIDAndPlanExecID(nctx, task.ID, task.PlanID)
		if err == nil {
			cancel()
			// 任务已被抢占不用执行了
			return domain.TaskExecution{}, errTaskHasAcquired
		}
		cancel()
		time.Sleep(defaultRetrySleepTime)
	}
}
