package runner

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gitee.com/flycash/distributed_task_platform/internal/errs"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/event"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"github.com/gotomicro/ego/core/elog"
)

const (
	defaultRetrySleepTime = 500 * time.Millisecond
	// maxRunRetries 单个 DAG 节点任务在 run 循环中的最大重试次数。
	// 超过此次数后放弃执行并记录错误，防止因非抢占类错误（如 DB 故障）导致的无限循环。
	maxRunRetries = 10
)

// PlanTaskRunner DAG 工作流（Plan）任务的执行器。
// 嵌入 NormalTaskRunner，复用其抢占+执行逻辑。
// 职责：
//  1. 抢占 Plan 任务 → 创建执行记录
//  2. 解析 DAG 图，找到入口任务（无前驱依赖的根节点）
//  3. 并行启动各根节点任务
//  4. 每个节点任务完成后，通过 NextStep 驱动后继节点执行
//  5. 所有叶子节点完成后，通过事件机制通知 Plan 执行完成
type PlanTaskRunner struct {
	planService task.PlanService
	*NormalTaskRunner
}

// NewPlanRunner 创建 PlanTaskRunner 实例。
// planService 提供 DAG 图的查询能力，singleRunner 提供单任务的抢占+执行能力。
func NewPlanRunner(planService task.PlanService, singleRunner *NormalTaskRunner) *PlanTaskRunner {
	return &PlanTaskRunner{
		planService:      planService,
		NormalTaskRunner: singleRunner,
	}
}

// Run 执行 Plan 工作流。
// 流程：
//  1. 抢占 Plan 任务（保证同一时刻只有一个调度节点执行该 Plan）
//  2. 创建 Plan 级别的执行记录（状态 = Running）
//  3. 从 PlanService 获取完整的 DAG 图
//  4. 找到所有根节点（无前驱的入口任务），并行启动执行
//
// 注意：每个根节点在独立 goroutine 中运行，通过 p.run 方法循环抢占直到成功。
func (p *PlanTaskRunner) Run(ctx context.Context, task domain.Task) error {
	// 只需要抢占任务就行
	ta, err := p.acquireTask(ctx, task)
	if err != nil {
		return err
	}
	// 抢占成功，立即创建 Plan 级别的 TaskExecution 记录
	exec, err := p.execSvc.Create(ctx, domain.TaskExecution{
		Task:      ta,
		StartTime: time.Now().UnixMilli(),
		Status:    domain.TaskExecutionStatusRunning,
	})
	if err != nil {
		p.releaseTask(ctx, ta)
		return err
	}
	// 从服务层获取 DAG 图的完整定义
	plan, err := p.planService.GetPlan(ctx, task.ID)
	if err != nil {
		return err
	}
	// 找到 DAG 的入口任务（所有无前驱依赖的根节点）
	rootTasks := plan.RootTask()
	for idx := range rootTasks {
		rootTask := rootTasks[idx]
		// 携带 Plan 执行 ID，用于后续关联子任务到 Plan
		rootTask.PlanExecID = exec.ID
		// 每个根节点在独立 goroutine 中并行执行
		go p.run(ctx, rootTask.Task)
	}
	return nil
}

// acquireTask 抢占 Plan 任务，保证同一时刻只有一个调度节点在执行同一个 Plan。
func (p *PlanTaskRunner) acquireTask(ctx context.Context, task domain.Task) (domain.Task, error) {
	acquiredTask, err := p.taskAcquirer.Acquire(ctx, task.ID, task.Version, p.nodeID)
	if err != nil {
		return domain.Task{}, fmt.Errorf("任务抢占失败: %w", err)
	}
	return acquiredTask, nil
}

func (p *PlanTaskRunner) releaseTask(ctx context.Context, task domain.Task) {
	if err := p.taskAcquirer.Release(ctx, task.ID, p.nodeID); err != nil {
		p.logger.Error("释放任务失败",
			elog.Int64("taskID", task.ID),
			elog.String("taskName", task.Name),
			elog.FieldErr(err))
	}
}

// NextStep 驱动 DAG 工作流的下一步执行。
// 当某个节点任务完成后被调用，流程：
//  1. 从 PlanService 获取 DAG 图
//  2. 找到当前任务在 DAG 中的位置
//  3. 获取所有后继任务
//  4. 如果没有后继（叶子节点），发送 Plan 完成事件
//  5. 如果有后继，检查每个后继任务的所有前驱是否都已完成（DAG 依赖判断）
//  6. 前驱全部完成的后继任务启动执行
func (p *PlanTaskRunner) NextStep(ctx context.Context, task domain.Task) error {
	plan, err := p.planService.GetPlan(ctx, task.PlanID)
	if err != nil {
		return err
	}
	planTask, ok := plan.GetTask(task.Name)
	if !ok {
		return fmt.Errorf("当前任务%s 不属于plan%s", task.Name, plan.Name)
	}
	// 获取 DAG 图中的后继节点
	tasks := planTask.NextStep()
	if len(tasks) == 0 {
		// 没有后继任务，说明当前是叶子节点。
		// 发送 Plan 完成事件，由事件消费者更新 Plan 执行记录的最终状态。
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

	// 遍历所有后继任务，检查前驱依赖是否满足
	for idx := range tasks {
		nextPlanTask := tasks[idx]
		// 关联 Plan 执行 ID，让子任务能追溯到所属的 Plan 执行实例
		nextPlanTask.PlanExecID = plan.Execution.ID
		// CheckPre() 检查该任务的所有前驱是否都已完成（DAG 依赖判断）
		// 只有所有前驱任务都成功完成，才启动后继任务
		if nextPlanTask.CheckPre() {
			go p.run(ctx, nextPlanTask.Task)
		}
	}
	return nil
}

// run 执行单个 DAG 节点任务的循环。
// 不断尝试抢占并执行任务，直到：
//   - 抢占成功并执行完成（正常退出）
//   - 抢占失败且错误为 ErrTaskPreemptFailed（说明被其他节点抢占，正常退出）
//   - 达到最大重试次数（防止非抢占类错误导致无限循环）
//   - context 被取消（优雅停机）
func (p *PlanTaskRunner) run(ctx context.Context, task domain.Task) {
	for retries := 0; retries < maxRunRetries; retries++ {
		// 检查 context 是否已取消，防止优雅停机期间继续重试
		if ctx.Err() != nil {
			p.logger.Warn("context已取消，停止任务重试",
				elog.Int64("taskID", task.ID),
				elog.String("taskName", task.Name))
			return
		}

		err := p.NormalTaskRunner.Run(ctx, task)
		if err == nil {
			// 执行成功，正常退出
			return
		}
		if errors.Is(err, errs.ErrTaskPreemptFailed) {
			// 被其他节点抢占，正常退出（不是错误）
			return
		}
		// 非抢占类错误（如 DB 故障），短暂等待后重试
		p.logger.Error("运行任务失败，准备重试",
			elog.Int64("taskID", task.ID),
			elog.String("taskName", task.Name),
			elog.Int("retryAttempt", retries+1),
			elog.Int("maxRetries", maxRunRetries),
			elog.FieldErr(err))
		time.Sleep(defaultRetrySleepTime)
	}
	// 达到最大重试次数，放弃执行
	p.logger.Error("任务达到最大重试次数，放弃执行",
		elog.Int64("taskID", task.ID),
		elog.String("taskName", task.Name),
		elog.Int("maxRetries", maxRunRetries))
}
