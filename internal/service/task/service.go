// Package task 提供任务（Task）和执行记录（TaskExecution）的核心业务服务。
//
// 本包是调度平台的业务核心层，位于领域模型（domain）和数据访问层（repository/dao）之间，
// 封装了任务生命周期管理、执行状态处理、DAG 工作流等核心业务逻辑。
//
// 主要服务接口：
//   - Service: 任务定义的 CRUD 和调度时间管理
//   - ExecutionService: 执行记录的状态机管理、状态上报处理
//   - PlanService: DAG 工作流（Plan）的图构建和任务创建
package task

import (
	"context"
	"fmt"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/errs"
	"gitee.com/flycash/distributed_task_platform/internal/repository"
)

// Service 定义了任务定义层面的业务服务接口。
// 主要职责：
//   - 创建任务定义（含 Cron 表达式校验和下次执行时间计算）
//   - 查询可调度任务列表（供调度循环使用）
//   - 更新任务的下次执行时间（任务执行完成后调用）
//   - 按 ID 查询任务详情
type Service interface {
	// Create 创建任务定义。会校验 Cron 表达式并计算首次执行时间。
	Create(ctx context.Context, task domain.Task) (domain.Task, error)
	// SchedulableTasks 查询当前可被调度的任务列表。
	// preemptedTimeoutMs: 已被抢占但超过此时间未执行的任务视为可重新调度（防止节点宕机导致任务卡死）。
	// limit: 单次最多返回的任务数量。
	SchedulableTasks(ctx context.Context, preemptedTimeoutMs int64, limit int) ([]domain.Task, error)
	// UpdateNextTime 根据 Cron 表达式计算并更新任务的下一次执行时间。
	// 任务每次执行完成后调用，使用乐观锁（Version）保证并发安全。
	UpdateNextTime(ctx context.Context, id int64) (domain.Task, error)
	// GetByID 根据任务 ID 查询任务定义的完整信息。
	GetByID(ctx context.Context, id int64) (domain.Task, error)
}

// service 是 Service 接口的默认实现。
type service struct {
	repo repository.TaskRepository // 任务数据仓库，负责持久化操作
}

// NewService 创建任务服务实例。
func NewService(repo repository.TaskRepository) Service {
	return &service{
		repo: repo,
	}
}

// Create 创建任务定义。
// 流程：
//  1. 根据 Cron 表达式计算首次执行时间
//  2. 如果表达式无效或计算出的时间为零值，返回错误
//  3. 将计算出的 NextTime 设置到任务对象上
//  4. 调用 repo.Create 持久化到数据库
func (s *service) Create(ctx context.Context, task domain.Task) (domain.Task, error) {
	// 计算并设置下次执行时间
	nextTime, err := task.CalculateNextTime()
	if err != nil {
		return domain.Task{}, fmt.Errorf("%w: %w", errs.ErrInvalidTaskCronExpr, err)
	}
	if nextTime.IsZero() {
		return domain.Task{}, errs.ErrInvalidTaskCronExpr
	}
	task.NextTime = nextTime.UnixMilli()
	return s.repo.Create(ctx, task)
}

func (s *service) SchedulableTasks(ctx context.Context, preemptedTimeoutMs int64, limit int) ([]domain.Task, error) {
	return s.repo.SchedulableTasks(ctx, preemptedTimeoutMs, limit)
}

// UpdateNextTime 更新任务的下次执行时间。
// 流程：
//  1. 先查询任务最新数据（含当前 Version）
//  2. 根据 Cron 表达式计算下一次执行时间
//  3. 如果时间为零值（如 Cron 表达式表示一次性任务且已执行），不更新直接返回
//  4. 使用乐观锁（Version）更新 NextTime，防止并发冲突
func (s *service) UpdateNextTime(ctx context.Context, id int64) (domain.Task, error) {
	task, err := s.GetByID(ctx, id)
	if err != nil {
		return domain.Task{}, err
	}
	// 计算并设置下次执行时间
	nextTime, err := task.CalculateNextTime()
	if err != nil {
		return domain.Task{}, fmt.Errorf("%w: %w", errs.ErrInvalidTaskCronExpr, err)
	}
	if nextTime.IsZero() {
		// 不需要继续执行了
		return task, nil
	}
	task.NextTime = nextTime.UnixMilli()
	return s.repo.UpdateNextTime(ctx, task.ID, task.Version, task.NextTime)
}

func (s *service) GetByID(ctx context.Context, id int64) (domain.Task, error) {
	return s.repo.GetByID(ctx, id)
}

// UpdateScheduleParams 更新任务的调度参数。
// 调度参数是一个 key-value 的 map，用于在调度过程中传递业务元数据
// （如分片任务的 Prepare 阶段获取的业务总数等）。
// 先将新参数合并到现有参数上，再通过乐观锁更新到数据库。
func (s *service) UpdateScheduleParams(ctx context.Context, task domain.Task, params map[string]string) (domain.Task, error) {
	task.UpdateScheduleParams(params)
	return s.repo.UpdateScheduleParams(ctx, task.ID, task.Version, task.ScheduleParams)
}
