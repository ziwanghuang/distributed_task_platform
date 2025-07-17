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

type Consumer struct {
	planRunner *runner.PlanTaskRunner
	// 更新
	execSvc tasksvc.ExecutionService
	taskSvc tasksvc.Service
	acquire acquirer.TaskAcquirer
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
