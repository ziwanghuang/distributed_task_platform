package complete

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"gitee.com/flycash/distributed_task_platform/internal/service/acquirer"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/event"
	"gitee.com/flycash/distributed_task_platform/internal/service/runner"
	tasksvc "gitee.com/flycash/distributed_task_platform/internal/service/task"
	"github.com/ecodeclub/mq-api"
	"github.com/pkg/errors"
)

const (
	number100 = 100
	number0   = 0
)

// Consumer 任务完成事件的消费者。
// 负责处理三种类型的完成事件，驱动系统的后续流程：
//   - NormalTaskType + PlanID > 0：DAG 工作流中的子任务完成，驱动后继任务
//   - NormalTaskType + PlanID = 0：独立任务完成，目前无额外处理
//   - PlanTaskType：整个 DAG Plan 完成，更新 Plan 执行记录状态并释放锁
type Consumer struct {
	planRunner *runner.PlanTaskRunner     // DAG 执行器，用于驱动后继任务
	execSvc    tasksvc.ExecutionService   // 执行服务，更新 Plan 执行记录
	taskSvc    tasksvc.Service            // 任务服务，更新下次执行时间
	acquire    acquirer.TaskAcquirer      // 任务抢占器，释放 Plan 锁
}

func NewConsumer(planRunner *runner.PlanTaskRunner, execSvc tasksvc.ExecutionService,
	taskSvc tasksvc.Service,
	acquirer acquirer.TaskAcquirer,
) *Consumer {
	return &Consumer{
		planRunner: planRunner,
		taskSvc:    taskSvc,
		execSvc:    execSvc,
		acquire:    acquirer,
	}
}

func (c *Consumer) Consume(ctx context.Context, message *mq.Message) error {
	var evt event.Event
	err := json.Unmarshal(message.Value, &evt)
	if err != nil {
		return fmt.Errorf("序列化失败 %w", err)
	}

	return c.handle(ctx, evt)
}

func (c *Consumer) handlePlanTask(ctx context.Context, evt event.Event) error {
	return c.planRunner.NextStep(ctx, domain.Task{
		ID:     evt.TaskID,
		PlanID: evt.PlanID,
		Name:   evt.Name,
	})
}

func (c *Consumer) handleTask(_ context.Context, _ event.Event) error {
	// 普通的任务,暂时啥也不做
	return nil
}

// handlePlan 处理 Plan（DAG 工作流）整体完成事件。
// 流程：
//  1. 根据最后一个叶子节点的执行状态判定 Plan 整体状态（Success/Failed）
//  2. 更新 Plan 的执行记录状态和结束时间
//  3. 更新 Plan 任务的下次执行时间（用于周期性 Plan）
//  4. 释放 Plan 任务的抢占锁，让下一次调度可以启动
func (c *Consumer) handlePlan(ctx context.Context, evt event.Event) error {
	var err error
	if evt.ExecStatus.IsSuccess() {
		err = c.execSvc.UpdateScheduleResult(ctx, evt.ExecID, domain.TaskExecutionStatusSuccess, number100, time.Now().UnixMilli(), nil, "")
	} else {
		err = c.execSvc.UpdateScheduleResult(ctx, evt.ExecID, domain.TaskExecutionStatusFailed, number0, time.Now().UnixMilli(), nil, "")
	}
	if err != nil {
		return err
	}
	_, err = c.taskSvc.UpdateNextTime(ctx, evt.TaskID)
	if err != nil {
		return err
	}
	// 释放plan
	err = c.acquire.Release(ctx, evt.TaskID, evt.ScheduleNodeID)
	return err
}

// handle 事件路由方法，根据事件类型分发到不同的处理器。
// 路由规则：
//   - NormalTaskType + PlanID > 0 → handlePlanTask：DAG 子任务完成，触发后继任务
//   - NormalTaskType + PlanID = 0 → handleTask：独立任务完成（当前为空实现）
//   - PlanTaskType → handlePlan：Plan 整体完成，更新状态并释放锁
func (c *Consumer) handle(ctx context.Context, evt event.Event) error {
	switch evt.Type {
	case domain.NormalTaskType:
		if evt.PlanID > 0 {
			return c.handlePlanTask(ctx, evt)
		}
		return c.handleTask(ctx, evt)
	case domain.PlanTaskType:
		return c.handlePlan(ctx, evt)
	default:
		return errors.New("unknown event type")
	}
}
