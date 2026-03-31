package task

import (
	"context"
	"fmt"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/dsl/parser"
	"gitee.com/flycash/distributed_task_platform/internal/errs"
	"gitee.com/flycash/distributed_task_platform/internal/repository"
	"golang.org/x/sync/errgroup"
)

type PlanService interface {
	GetPlan(ctx context.Context, planID int64) (domain.Plan, error)
	CreateTask(ctx context.Context, planID int64, task domain.Task) error
}

type planService struct {
	repo          repository.TaskRepository
	executionRepo repository.TaskExecutionRepository
}

func NewPlanService(repo repository.TaskRepository, executionRepo repository.TaskExecutionRepository) PlanService {
	return &planService{
		repo:          repo,
		executionRepo: executionRepo,
	}
}

func (p planService) CreateTask(ctx context.Context, planID int64, task domain.Task) error {
	task.PlanID = planID
	nextTime, err := task.CalculateNextTime()
	if err != nil {
		return fmt.Errorf("%w: %w", errs.ErrInvalidTaskCronExpr, err)
	}
	if nextTime.IsZero() {
		return errs.ErrInvalidTaskCronExpr
	}
	task.NextTime = nextTime.UnixMilli()
	_, err = p.repo.Create(ctx, task)
	return err
}

// getPlanData 并发获取 Plan 相关的所有数据
func (p planService) getPlanData(ctx context.Context, planID int64) (plan domain.Task, planExection domain.TaskExecution, planTasks []domain.Task, planTaskExecs map[int64]domain.TaskExecution, err error) {
	var g errgroup.Group
	// 并发获取 Plan 基本信息
	g.Go(func() error {
		var eerr error
		plan, eerr = p.repo.GetByID(ctx, planID)
		return eerr
	})

	// 并发获取 Plan 下所有任务
	g.Go(func() error {
		var eerr error
		planTasks, eerr = p.repo.FindByPlanID(ctx, planID)
		return eerr
	})

	g.Go(func() error {
		execs, eerr := p.executionRepo.FindByTaskID(ctx, planID)
		if eerr != nil {
			return err
		}
		// 找到最新的
		if len(execs) > 0 {
			planExection = execs[0]
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		return domain.Task{}, domain.TaskExecution{}, nil, nil, err
	}
	// 获取 Plan 的执行记录，用于获取 planExecID
	planTaskExecs, err = p.executionRepo.FindExecutionsByPlanExecID(ctx, planExection.ID)
	return plan, planExection, planTasks, planTaskExecs, err
}

// GetPlan 构建并返回完整的 DAG 工作流模型。
// 流程：并发获取数据 → 转换为 Plan 领域模型（含 DAG 图结构）。
func (p planService) GetPlan(ctx context.Context, planID int64) (domain.Plan, error) {
	// 并发获取 Plan 相关的所有数据
	repoPlan, repoPlanExec, tasks, executions, err := p.getPlanData(ctx, planID)
	if err != nil {
		return domain.Plan{}, err
	}
	return p.taskToPlan(repoPlan, repoPlanExec, tasks, executions)
}

// taskToPlan 将任务转换为计划
func (p planService) taskToPlan(ta domain.Task, exec domain.TaskExecution, tasks []domain.Task, executions map[int64]domain.TaskExecution) (domain.Plan, error) {
	plan := domain.Plan{
		ID:             ta.ID,
		Name:           ta.Name,
		CronExpr:       ta.CronExpr,
		ExecExpr:       ta.ExecExpr,
		Execution:      exec,
		ScheduleNodeID: ta.ScheduleNodeID,
		Status:         ta.Status,
		ScheduleParams: ta.ScheduleParams,
		NextTime:       ta.NextTime,
		Version:        ta.Version,
		CTime:          ta.CTime,
		UTime:          ta.UTime,
	}

	astPlan, err := parser.NewAstPlan(plan.ExecExpr)
	if err != nil {
		return domain.Plan{}, fmt.Errorf("解析执行表达式失败: %w", err)
	}
	plan.ExecPlan = astPlan

	// 组装 PlanTask 列表
	planTasks := make([]*domain.PlanTask, 0, len(tasks))
	taskMap := make(map[string]*domain.PlanTask)
	// 先创建所有 PlanTask，设置基本信息和执行状态
	for idx := range tasks {
		task := tasks[idx]
		planTask := &domain.PlanTask{
			Task: task,
		}
		if execution, exists := executions[task.ID]; exists {
			planTask.TaskExecution = execution
		}
		planNode, ok := astPlan.AdjoiningNode(task.Name)
		if !ok {
			return domain.Plan{}, errs.ErrInitPlanFailed
		}
		planTask.AstPlanNode = planNode
		planTasks = append(planTasks, planTask)
		taskMap[task.Name] = planTask
	}
	//
	// 设置前驱后继
	for idx := range planTasks {
		planTask := planTasks[idx]
		err = planTask.SetPre(taskMap)
		if err != nil {
			return domain.Plan{}, err
		}
		err = planTask.SetNext(taskMap)
		if err != nil {
			return domain.Plan{}, err
		}
		if len(planTask.PreTask) == 0 {
			plan.Root = append(plan.Root, planTask)
		}
	}
	plan.Steps = planTasks
	return plan, nil
}
