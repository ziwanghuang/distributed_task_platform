//go:build e2e

package integration

import (
	"context"
	"gitee.com/flycash/distributed_task_platform/internal/repository/dao"
	"gitee.com/flycash/distributed_task_platform/pkg/executor"
	"gitee.com/flycash/distributed_task_platform/scheduler"
	"github.com/ecodeclub/ekit/list"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
	"time"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	tasksvc "gitee.com/flycash/distributed_task_platform/internal/service/task"
	"gitee.com/flycash/distributed_task_platform/internal/test/integration/ioc"
	testioc "gitee.com/flycash/distributed_task_platform/internal/test/ioc"
	"github.com/ego-component/egorm"
	"github.com/stretchr/testify/suite"
)

type PlanSuite struct {
	suite.Suite
	db          *egorm.Component
	execService tasksvc.ExecutionService
	taskService tasksvc.Service
	scheduler   *scheduler.Scheduler
	ans         *list.ConcurrentList[MockTaskAns]
}
type MockTaskAns struct {
	Name      string
	TimeStamp int64
}

func (s *PlanSuite) SetupSuite() {
	db := testioc.InitDBAndTables()
	s.db = db
	app := ioc.InitSchedulerApp(map[string]executor.LocalExecuteFunc{
		"TaskA":   s.mockFuncTask,
		"TaskB":   s.mockFuncTask,
		"TaskC":   s.mockFuncTask,
		"TaskD":   s.mockFuncTask,
		"TaskE":   s.mockFuncTask,
		"TaskF":   s.mockFuncTask,
		"TaskEnd": s.mockFuncTask,
	})
	s.execService = app.ExecutionSvc
	s.taskService = app.TaskSvc
}

func (s *PlanSuite) TearDownSuite() {
	// 设置任务
	s.setUpTasks()
	//

}

func (s *PlanSuite) setUpTasks() {
	//A->B->(C&&D)；C->(E||F)->end; D->E->end;

	now := time.Now()
	// 创建主计划任务
	plan := dao.Task{
		Name:            "TestPlan",
		CronExpr:        "0 0 * * * ?", // 每小时执行一次
		ExecExpr:        "A->B->(C&&D)；C->(E||F)->end; D->E->end;",
		Type:            domain.PlanTaskType.String(),
		ExecutionMethod: domain.TaskExecutionMethodRemote.String(),
		Status:          domain.TaskStatusActive.String(),
		NextTime:        now.Add(-3 * time.Second).UnixMilli(),
		Ctime:           now.Add(-3 * time.Second).UnixMilli(),
		Utime:           now.Add(-3 * time.Second).UnixMilli(),
		Version:         1,
	}

	err := s.db.WithContext(s.T().Context()).Create(&plan).Error
	require.NoError(s.T(), err)

	// 创建子任务 A
	taskA := domain.Task{
		Name:            "TaskA",
		CronExpr:        "0 0 * * * ?",
		ExecExpr:        "",
		Type:            domain.NormalTaskType,
		ExecutionMethod: domain.TaskExecutionMethodLocal,
		Status:          domain.TaskStatusActive,
		PlanID:          plan.ID,
		Version:         1,
	}

	// 创建子任务 B
	taskB := domain.Task{
		Name:            "TaskB",
		CronExpr:        "0 0 * * * ?",
		ExecExpr:        "",
		Type:            domain.NormalTaskType,
		ExecutionMethod: domain.TaskExecutionMethodLocal,
		Status:          domain.TaskStatusActive,
		PlanID:          plan.ID,
		Version:         1,
	}

	// 创建子任务 C
	taskC := domain.Task{
		Name:            "TaskC",
		CronExpr:        "0 0 * * * ?",
		ExecExpr:        "",
		Type:            domain.NormalTaskType,
		ExecutionMethod: domain.TaskExecutionMethodLocal,
		Status:          domain.TaskStatusActive,
		PlanID:          plan.ID,
		Version:         1,
	}

	// 创建子任务 D
	taskD := domain.Task{
		Name:            "TaskD",
		CronExpr:        "0 0 * * * ?",
		ExecExpr:        "",
		Type:            domain.NormalTaskType,
		ExecutionMethod: domain.TaskExecutionMethodLocal,
		Status:          domain.TaskStatusActive,
		PlanID:          plan.ID,
		Version:         1,
	}

	// 创建子任务 E
	taskE := domain.Task{
		Name:            "TaskE",
		CronExpr:        "0 0 * * * ?",
		ExecExpr:        "",
		Type:            domain.NormalTaskType,
		ExecutionMethod: domain.TaskExecutionMethodLocal,
		Status:          domain.TaskStatusActive,
		PlanID:          plan.ID,
		Version:         1,
	}

	// 创建子任务 F
	taskF := domain.Task{
		Name:            "TaskF",
		CronExpr:        "0 0 * * * ?",
		ExecExpr:        "",
		Type:            domain.NormalTaskType,
		ExecutionMethod: domain.TaskExecutionMethodLocal,
		Status:          domain.TaskStatusActive,
		PlanID:          plan.ID,
		Version:         1,
	}
	taskEnd := domain.Task{
		Name:            "TaskEnd",
		ExecExpr:        "",
		CronExpr:        "0 0 * * * ?",
		Type:            domain.NormalTaskType,
		ExecutionMethod: domain.TaskExecutionMethodLocal,
		Status:          domain.TaskStatusActive,
		PlanID:          plan.ID,
		Version:         1,
	}
	ctx := s.T().Context()
	// 创建所有子任务
	_, err = s.taskService.Create(ctx, taskA)
	s.Require().NoError(err)

	_, err = s.taskService.Create(ctx, taskB)
	s.Require().NoError(err)

	_, err = s.taskService.Create(ctx, taskC)
	s.Require().NoError(err)

	_, err = s.taskService.Create(ctx, taskD)
	s.Require().NoError(err)

	_, err = s.taskService.Create(ctx, taskE)
	s.Require().NoError(err)

	_, err = s.taskService.Create(ctx, taskF)
	s.Require().NoError(err)

	_, err = s.taskService.Create(ctx, taskEnd)
	s.Require().NoError(err)
}

// TestPlanExecution 测试计划执行
func (s *PlanSuite) TestPlanExecution() {
	// 设置任务
	s.setUpTasks()
	// 开始

	// 这里可以添加更多的测试逻辑来验证计划执行
	// 例如：验证任务依赖关系、执行顺序等
	s.T().Log("计划任务创建成功，可以开始执行测试")
}

func (s *PlanSuite) mockFuncTask(_ context.Context, execution domain.TaskExecution) (domain.ExecutionState, error) {
	s.T().Helper()
	s.T().Log("开始执行", execution.Task.Name, time.Now().UnixMilli())
	if strings.Contains(execution.Task.Name, "C") {
		time.Sleep(500 * time.Millisecond)
	} else {
		time.Sleep(1 * time.Second)
	}
	s.ans.Append(MockTaskAns{
		Name:      execution.Task.Name,
		TimeStamp: time.Now().UnixMilli(),
	})

	s.T().Log("执行结束", execution.Task.Name, time.Now().UnixMilli())
	return domain.ExecutionState{
		ID:              execution.ID,
		TaskID:          execution.Task.ID,
		TaskName:        execution.Task.Name,
		Status:          domain.TaskExecutionStatusSuccess,
		RunningProgress: 100,
	}, nil

}

func TestPlanSuite(t *testing.T) {
	suite.Run(t, new(PlanSuite))
}
