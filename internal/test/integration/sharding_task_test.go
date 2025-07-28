//go:build e2e

package integration

import (
	"fmt"
	"testing"
	"time"

	"github.com/ego-component/egorm"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/repository/dao"
	"gitee.com/flycash/distributed_task_platform/internal/service/invoker"
	"gitee.com/flycash/distributed_task_platform/internal/test/integration/ioc"
	"gitee.com/flycash/distributed_task_platform/pkg/sharding"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type ShardingTaskSuite struct {
	suite.Suite
	app             *ioc.SchedulerApp
	shardingTaskDAO dao.ShardingTaskDAO
	dbs             map[string]*egorm.Component
}

func (suite *ShardingTaskSuite) SetupSuite() {
	suite.app = ioc.InitSchedulerApp(map[string]invoker.LocalExecuteFunc{})
	suite.shardingTaskDAO = suite.app.ShardingTaskDAO
	suite.dbs = ioc.InitDBs()
}

func (suite *ShardingTaskSuite) TearDownSuite() {
	// Clean up all test data
	suite.cleanupTestData(200)
	suite.cleanupTestData(201)
	suite.cleanupTestData(301)
}

// cleanupTestData removes all test data with the specified bizID from all databases and tables
func (suite *ShardingTaskSuite) cleanupTestData(bizID int64) {

	// For each database
	for dbName, db := range suite.dbs {
		// Clean up task tables
		for i := 0; i < 2; i++ {
			tableName := fmt.Sprintf("task_%d", i)
			err := db.Exec(fmt.Sprintf("DELETE FROM %s WHERE biz_id = ?", tableName), bizID).Error
			if err != nil {
				suite.T().Logf("Error cleaning up %s.%s: %v", dbName, tableName, err)
			}
		}

		// Clean up task_execution tables
		for i := 0; i < 4; i++ {
			tableName := fmt.Sprintf("task_execution_%d", i)
			// First, find all task IDs with our bizID
			var taskIDs []int64
			err := db.Raw("SELECT id FROM task_0 WHERE biz_id = ? UNION SELECT id FROM task_1 WHERE biz_id = ?",
				bizID, bizID).Pluck("id", &taskIDs).Error
			if err != nil {
				suite.T().Logf("Error finding task IDs for bizID %d: %v", bizID, err)
				continue
			}

			// If we found any tasks, delete their executions
			if len(taskIDs) > 0 {
				query := db.Table(tableName).Where("task_id IN ?", taskIDs).Delete(&dao.TaskExecution{})
				if query.Error != nil {
					suite.T().Logf("Error cleaning up %s.%s: %v", dbName, tableName, query.Error)
				} else {
					suite.T().Logf("Deleted %d executions from %s.%s", query.RowsAffected, dbName, tableName)
				}
			}
		}
	}

	suite.T().Logf("Cleaned up all test data with bizID %d", bizID)
}

func (suite *ShardingTaskSuite) TestCreate() {
	// Create a task
	task := dao.Task{
		BizID:               201,
		Name:                "TestShardingTask_1",
		CronExpr:            "*/5 * * * * ?",
		ExecutionMethod:     domain.TaskExecutionMethodLocal.String(),
		MaxExecutionSeconds: 3600,
		NextTime:            time.Now().UnixMilli(),
		Status:              domain.TaskStatusActive.String(),
		Version:             1,
		Type:                domain.NormalTaskType.String(),
		Ctime:               time.Now().UnixMilli(),
		Utime:               time.Now().UnixMilli(),
	}

	createdTask, err := suite.shardingTaskDAO.Create(suite.T().Context(), task)
	require.NoError(suite.T(), err)
	require.NotNil(suite.T(), createdTask)
	assert.NotZero(suite.T(), createdTask.ID)
	assert.Equal(suite.T(), task.Name, createdTask.Name)
	assert.Equal(suite.T(), task.BizID, createdTask.BizID)
}

func (suite *ShardingTaskSuite) TestGetByID() {
	// First create a task
	task := dao.Task{
		BizID:               301,
		Name:                "TestShardingGetByID",
		CronExpr:            "*/5 * * * * ?",
		ExecutionMethod:     domain.TaskExecutionMethodLocal.String(),
		MaxExecutionSeconds: 3600,
		NextTime:            time.Now().UnixMilli(),
		Status:              domain.TaskStatusActive.String(),
		Version:             1,
		Type:                domain.NormalTaskType.String(),
		Ctime:               time.Now().UnixMilli(),
		Utime:               time.Now().UnixMilli(),
	}

	createdTask, err := suite.shardingTaskDAO.Create(suite.T().Context(), task)
	require.NoError(suite.T(), err)

	// Then get it by ID
	retrievedTask, err := suite.shardingTaskDAO.GetByID(suite.T().Context(), createdTask.ID)
	require.NoError(suite.T(), err)
	require.NotNil(suite.T(), retrievedTask)
	assert.Equal(suite.T(), createdTask.ID, retrievedTask.ID)
	assert.Equal(suite.T(), task.Name, retrievedTask.Name)
	assert.Equal(suite.T(), task.BizID, retrievedTask.BizID)
}

func (suite *ShardingTaskSuite) TestFindByPlanID() {
	// First create a plan task
	planTask := dao.Task{
		BizID:               200,
		Name:                "TestShardingPlanTask",
		CronExpr:            "*/5 * * * * ?",
		ExecutionMethod:     domain.TaskExecutionMethodLocal.String(),
		MaxExecutionSeconds: 3600,
		NextTime:            time.Now().UnixMilli(),
		Status:              domain.TaskStatusActive.String(),
		Version:             1,
		Type:                domain.PlanTaskType.String(),
		Ctime:               time.Now().UnixMilli(),
		Utime:               time.Now().UnixMilli(),
	}

	createdPlanTask, err := suite.shardingTaskDAO.Create(suite.T().Context(), planTask)
	require.NoError(suite.T(), err)

	// Create tasks with this plan ID
	childTask1 := dao.Task{
		BizID:               200,
		Name:                "TestShardingChildTask1",
		CronExpr:            "*/5 * * * * ?",
		ExecutionMethod:     domain.TaskExecutionMethodLocal.String(),
		MaxExecutionSeconds: 3600,
		NextTime:            time.Now().UnixMilli(),
		Status:              domain.TaskStatusActive.String(),
		Version:             1,
		Type:                domain.NormalTaskType.String(),
		PlanID:              createdPlanTask.ID,
		Ctime:               time.Now().UnixMilli(),
		Utime:               time.Now().UnixMilli(),
	}

	childTask2 := dao.Task{
		BizID:               200,
		Name:                "TestShardingChildTask2",
		CronExpr:            "*/5 * * * * ?",
		ExecutionMethod:     domain.TaskExecutionMethodLocal.String(),
		MaxExecutionSeconds: 3600,
		NextTime:            time.Now().UnixMilli(),
		Status:              domain.TaskStatusActive.String(),
		Version:             1,
		Type:                domain.NormalTaskType.String(),
		PlanID:              createdPlanTask.ID,
		Ctime:               time.Now().UnixMilli(),
		Utime:               time.Now().UnixMilli(),
	}

	_, err = suite.shardingTaskDAO.Create(suite.T().Context(), childTask1)
	require.NoError(suite.T(), err)

	_, err = suite.shardingTaskDAO.Create(suite.T().Context(), childTask2)
	require.NoError(suite.T(), err)

	// Find tasks by plan ID
	tasks, err := suite.shardingTaskDAO.FindByPlanID(suite.T().Context(), createdPlanTask.ID)
	require.NoError(suite.T(), err)
	require.Len(suite.T(), tasks, 2)
}

func (suite *ShardingTaskSuite) TestFindSchedulableTasks() {
	// Create a task first
	task := dao.Task{
		BizID:               200,
		Name:                "TestSchedulableTask",
		CronExpr:            "*/5 * * * * ?",
		ExecutionMethod:     domain.TaskExecutionMethodLocal.String(),
		MaxExecutionSeconds: 3600,
		NextTime:            time.Now().Add(-time.Minute).UnixMilli(), // Set to past time to make it schedulable
		Status:              domain.TaskStatusActive.String(),
		Version:             1,
		Type:                domain.NormalTaskType.String(),
		Ctime:               time.Now().UnixMilli(),
		Utime:               time.Now().UnixMilli(),
	}

	_, err := suite.shardingTaskDAO.Create(suite.T().Context(), task)
	require.NoError(suite.T(), err)

	// Get the sharding destination based on the task's BizID
	dst := suite.shardingTaskDAO.GetShardingStrategy().Shard(task.BizID)

	// Create a context with the sharding destination
	ctxWithDst := sharding.CtxWithDst(suite.T().Context(), dst)

	// Find schedulable tasks
	tasks, err := suite.shardingTaskDAO.FindSchedulableTasks(ctxWithDst, 60000, 10)
	require.NoError(suite.T(), err)
	assert.GreaterOrEqual(suite.T(), len(tasks), 1)
}

func (suite *ShardingTaskSuite) TestAcquire() {
	// Create a task
	task := dao.Task{
		BizID:               200,
		Name:                "TestAcquireTask",
		CronExpr:            "*/5 * * * * ?",
		ExecutionMethod:     domain.TaskExecutionMethodLocal.String(),
		MaxExecutionSeconds: 3600,
		NextTime:            time.Now().UnixMilli(),
		Status:              domain.TaskStatusActive.String(),
		Version:             1,
		Type:                domain.NormalTaskType.String(),
		Ctime:               time.Now().UnixMilli(),
		Utime:               time.Now().UnixMilli(),
	}

	createdTask, err := suite.shardingTaskDAO.Create(suite.T().Context(), task)
	require.NoError(suite.T(), err)

	// Acquire the task
	nodeID := "test-node-1"
	acquiredTask, err := suite.shardingTaskDAO.Acquire(suite.T().Context(), createdTask.ID, nodeID)
	require.NoError(suite.T(), err)
	require.NotNil(suite.T(), acquiredTask)
	assert.Equal(suite.T(), domain.TaskStatusPreempted.String(), acquiredTask.Status)
	assert.Equal(suite.T(), nodeID, acquiredTask.ScheduleNodeID.String)
	assert.Equal(suite.T(), int64(2), acquiredTask.Version) // Version should be incremented
}

func (suite *ShardingTaskSuite) TestRenew() {
	// Create a task
	task := dao.Task{
		BizID:               200,
		Name:                "TestRenewTask",
		CronExpr:            "*/5 * * * * ?",
		ExecutionMethod:     domain.TaskExecutionMethodLocal.String(),
		MaxExecutionSeconds: 3600,
		NextTime:            time.Now().UnixMilli(),
		Status:              domain.TaskStatusActive.String(),
		Version:             1,
		Type:                domain.NormalTaskType.String(),
		Ctime:               time.Now().UnixMilli(),
		Utime:               time.Now().UnixMilli(),
	}

	createdTask, err := suite.shardingTaskDAO.Create(suite.T().Context(), task)
	require.NoError(suite.T(), err)

	// Acquire the task first
	nodeID := "test-node-2"
	_, err = suite.shardingTaskDAO.Acquire(suite.T().Context(), createdTask.ID, nodeID)
	require.NoError(suite.T(), err)

	// Renew the task
	err = suite.shardingTaskDAO.Renew(suite.T().Context(), nodeID)
	require.NoError(suite.T(), err)

	// Verify the task has been renewed (version incremented)
	renewedTask, err := suite.shardingTaskDAO.GetByID(suite.T().Context(), createdTask.ID)
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), int64(3), renewedTask.Version) // Version should be incremented again
}

func (suite *ShardingTaskSuite) TestRelease() {
	// Create a task
	task := dao.Task{
		BizID:               200,
		Name:                "TestReleaseTask",
		CronExpr:            "*/5 * * * * ?",
		ExecutionMethod:     domain.TaskExecutionMethodLocal.String(),
		MaxExecutionSeconds: 3600,
		NextTime:            time.Now().UnixMilli(),
		Status:              domain.TaskStatusActive.String(),
		Version:             1,
		Type:                domain.NormalTaskType.String(),
		Ctime:               time.Now().UnixMilli(),
		Utime:               time.Now().UnixMilli(),
	}

	createdTask, err := suite.shardingTaskDAO.Create(suite.T().Context(), task)
	require.NoError(suite.T(), err)

	// Acquire the task first
	nodeID := "test-node-3"
	_, err = suite.shardingTaskDAO.Acquire(suite.T().Context(), createdTask.ID, nodeID)
	require.NoError(suite.T(), err)

	// Release the task
	releasedTask, err := suite.shardingTaskDAO.Release(suite.T().Context(), createdTask.ID, nodeID)
	require.NoError(suite.T(), err)
	require.NotNil(suite.T(), releasedTask)
	assert.Equal(suite.T(), domain.TaskStatusActive.String(), releasedTask.Status)
	assert.False(suite.T(), releasedTask.ScheduleNodeID.Valid) // ScheduleNodeID should be NULL
	assert.Equal(suite.T(), int64(3), releasedTask.Version)    // Version should be incremented again
}

func (suite *ShardingTaskSuite) TestUpdateNextTime() {
	// Create a task
	task := dao.Task{
		BizID:               200,
		Name:                "TestUpdateNextTimeTask",
		CronExpr:            "*/5 * * * * ?",
		ExecutionMethod:     domain.TaskExecutionMethodLocal.String(),
		MaxExecutionSeconds: 3600,
		NextTime:            time.Now().UnixMilli(),
		Status:              domain.TaskStatusActive.String(),
		Version:             1,
		Type:                domain.NormalTaskType.String(),
		Ctime:               time.Now().UnixMilli(),
		Utime:               time.Now().UnixMilli(),
	}

	createdTask, err := suite.shardingTaskDAO.Create(suite.T().Context(), task)
	require.NoError(suite.T(), err)

	// Update next time
	nextTime := time.Now().Add(time.Hour).UnixMilli()
	updatedTask, err := suite.shardingTaskDAO.UpdateNextTime(suite.T().Context(), createdTask.ID, createdTask.Version, nextTime)
	require.NoError(suite.T(), err)
	require.NotNil(suite.T(), updatedTask)
	assert.Equal(suite.T(), nextTime, updatedTask.NextTime)
	assert.Equal(suite.T(), int64(2), updatedTask.Version) // Version should be incremented
}

func (suite *ShardingTaskSuite) TestUpdateScheduleParams() {
	// Create a task
	task := dao.Task{
		BizID:               200,
		Name:                "TestUpdateParamsTask",
		CronExpr:            "*/5 * * * * ?",
		ExecutionMethod:     domain.TaskExecutionMethodLocal.String(),
		MaxExecutionSeconds: 3600,
		NextTime:            time.Now().UnixMilli(),
		Status:              domain.TaskStatusActive.String(),
		Version:             1,
		Type:                domain.NormalTaskType.String(),
		Ctime:               time.Now().UnixMilli(),
		Utime:               time.Now().UnixMilli(),
	}

	createdTask, err := suite.shardingTaskDAO.Create(suite.T().Context(), task)
	require.NoError(suite.T(), err)

	// Update schedule params

	updatedTask, err := suite.shardingTaskDAO.UpdateScheduleParams(suite.T().Context(), createdTask.ID, createdTask.Version, nil)
	require.NoError(suite.T(), err)
	require.NotNil(suite.T(), updatedTask)
	assert.Equal(suite.T(), int64(2), updatedTask.Version) // Version should be incremented
}

func TestShardingTaskSuite(t *testing.T) {
	suite.Run(t, new(ShardingTaskSuite))
}
