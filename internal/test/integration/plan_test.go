//go:build e2e

package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"gitee.com/flycash/distributed_task_platform/internal/repository/dao"
	"gitee.com/flycash/distributed_task_platform/internal/service/invoker"
	"github.com/stretchr/testify/assert"

	"gitee.com/flycash/distributed_task_platform/internal/service/scheduler"
	"github.com/ecodeclub/ekit/list"
	"github.com/stretchr/testify/require"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	tasksvc "gitee.com/flycash/distributed_task_platform/internal/service/task"
	"gitee.com/flycash/distributed_task_platform/internal/test/integration/ioc"
	testioc "gitee.com/flycash/distributed_task_platform/internal/test/ioc"
	"github.com/ego-component/egorm"
	"github.com/stretchr/testify/suite"
)

type PlanSuite struct {
	suite.Suite
	db               *egorm.Component
	execService      tasksvc.ExecutionService
	taskService      tasksvc.Service
	scheduler        *scheduler.Scheduler
	completeConsumer *ioc.CompleteConsumer
	ans              *list.ConcurrentList[MockTaskAns]
}
type MockTaskAns struct {
	Name string
}

func (s *PlanSuite) SetupSuite() {
	db := testioc.InitDBAndTables()
	s.db = db
	app := ioc.InitSchedulerApp(map[string]invoker.LocalExecuteFunc{
		"TaskA":   s.mockFuncTask,
		"TaskB":   s.mockFuncTask,
		"TaskC":   s.mockFuncTask,
		"TaskD":   s.mockFuncTask,
		"TaskE":   s.mockFuncTask,
		"TaskF":   s.mockFuncTask,
		"TaskEnd": s.mockFuncTask,
	})
	s.completeConsumer = app.Consumer
	s.execService = app.ExecutionSvc
	s.taskService = app.TaskSvc
	s.scheduler = app.Scheduler
	s.ans = &list.ConcurrentList[MockTaskAns]{
		List: list.NewArrayList[MockTaskAns](20),
	}
}

func (s *PlanSuite) setUpTasks() int64 {
	now := time.Now()
	// 创建主计划任务
	plan := dao.Task{
		Name:            "TaskPlan",
		CronExpr:        "0 0 * * * ?", // 每小时执行一次
		ExecExpr:        "TaskA->TaskB->(TaskC&&TaskD);TaskC->(TaskE&&TaskF)->TaskEnd;TaskD->TaskE->TaskEnd;",
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
	return int64(plan.ID)
}

// TestPlanExecution 测试计划执行
func (s *PlanSuite) TestPlanExecution() {
	// 设置任务
	planID := s.setUpTasks()
	// 开始
	err := s.scheduler.Start()
	go s.completeConsumer.Start()

	s.T().Log("计划任务创建成功，可以开始执行测试")
	require.NoError(s.T(), err)
	time.Sleep(20 * time.Second)

	// 验证任务执行顺序
	assert.Equal(s.T(), []MockTaskAns{
		{
			Name: "TaskA",
		},
		{
			Name: "TaskB",
		},
		{
			Name: "TaskC",
		},
		{
			Name: "TaskD",
		},
		{
			// 因为C睡眠的时间比较少所以
			Name: "TaskF",
		},
		{
			Name: "TaskE",
		},
		{
			Name: "TaskEnd",
		},
	}, s.ans.AsSlice())

	// 判断plan的状态
	plan, err := s.taskService.GetByID(s.T().Context(), planID)
	require.NoError(s.T(), err)
	s.T().Logf("Plan状态: %s, 下次执行时间: %d", plan.Status, plan.NextTime)

	// 验证plan状态应该是ACTIVE（可调度）
	assert.Equal(s.T(), domain.TaskStatusActive, plan.Status)

	// 验证下次执行时间已经更新（应该大于当前时间）
	now := time.Now().UnixMilli()
	assert.Greater(s.T(), plan.NextTime, now, "下次执行时间应该大于当前时间")

	// 判断plan execution的状态
	var planExecutions []dao.TaskExecution
	err = s.db.WithContext(s.T().Context()).Where("task_id = ?", planID).Order("ctime DESC").Find(&planExecutions).Error
	require.NoError(s.T(), err)
	require.NotEmpty(s.T(), planExecutions, "应该存在plan的执行记录")

	// 获取最新的plan执行记录
	latestPlanExec := planExecutions[0]
	s.T().Logf("Plan执行状态: %s, 开始时间: %d, 结束时间: %d",
		latestPlanExec.Status, latestPlanExec.Stime, latestPlanExec.Etime)

	// 验证plan执行状态应该是SUCCESS
	assert.Equal(s.T(), "SUCCESS", latestPlanExec.Status)
	assert.NotZero(s.T(), latestPlanExec.Stime, "开始时间不应该为0")
	assert.NotZero(s.T(), latestPlanExec.Etime, "结束时间不应该为0")

	// 验证所有子任务的执行状态
	var subTasks []dao.Task
	err = s.db.WithContext(s.T().Context()).Where("plan_id = ?", planID).Find(&subTasks).Error
	require.NoError(s.T(), err)
	require.Len(s.T(), subTasks, 7, "应该有7个子任务")

	for _, subTask := range subTasks {
		// 获取每个子任务的执行记录
		var subTaskExecutions []dao.TaskExecution
		err = s.db.WithContext(s.T().Context()).Where("task_id = ?", subTask.ID).Order("ctime DESC").Find(&subTaskExecutions).Error
		require.NoError(s.T(), err)
		require.NotEmpty(s.T(), subTaskExecutions, "子任务 %s 应该有执行记录", subTask.Name)

		latestSubTaskExec := subTaskExecutions[0]
		s.T().Logf("子任务 %s 执行状态: %s", subTask.Name, latestSubTaskExec.Status)

		// 验证所有子任务都应该执行成功
		assert.Equal(s.T(), "SUCCESS", latestSubTaskExec.Status,
			"子任务 %s 应该执行成功", subTask.Name)
		assert.NotZero(s.T(), latestSubTaskExec.Stime, "子任务 %s 开始时间不应该为0", subTask.Name)
		assert.NotZero(s.T(), latestSubTaskExec.Etime, "子任务 %s 结束时间不应该为0", subTask.Name)
	}

	// 清理数据
	s.db.WithContext(s.T().Context()).Where("plan_id = ? or id = ?", planID, planID).Delete(&dao.Task{})
	s.db.WithContext(s.T().Context()).Where("task_id =? or task_plan_id", planID, planID).Delete(&dao.TaskExecution{})
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
		Name: execution.Task.Name,
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
