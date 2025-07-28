//go:build e2e

package integration

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/repository/dao"
	"gitee.com/flycash/distributed_task_platform/internal/service/invoker"
	"gitee.com/flycash/distributed_task_platform/internal/test/integration/ioc"
	"gitee.com/flycash/distributed_task_platform/pkg/sharding"
	"github.com/ego-component/egorm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type ShardingTaskExecutionSuite struct {
	suite.Suite
	app                      *ioc.SchedulerApp
	shardingTaskDAO          dao.ShardingTaskDAO
	shardingTaskExecutionDAO dao.ShardingTaskExecutionDAO
	dbs                      map[string]*egorm.Component
}

func (suite *ShardingTaskExecutionSuite) SetupSuite() {
	suite.app = ioc.InitSchedulerApp(map[string]invoker.LocalExecuteFunc{})
	suite.shardingTaskDAO = suite.app.ShardingTaskDAO
	suite.shardingTaskExecutionDAO = suite.app.TaskExecutionDAO
	suite.dbs = ioc.InitDBs()
}

func (suite *ShardingTaskExecutionSuite) TearDownSuite() {
	// Clean up all test data
	suite.cleanupTestData(200)
	suite.cleanupTestData(201)
	suite.cleanupTestData(301)
}

// cleanupTestData removes all test data with the specified bizID from all databases and tables
func (suite *ShardingTaskExecutionSuite) cleanupTestData(bizID int64) {
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

// Helper method to create a task for testing
func (suite *ShardingTaskExecutionSuite) createTask(bizID int64, name string) *dao.Task {
	task := dao.Task{
		BizID:               bizID,
		Name:                name,
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
	return createdTask
}

// Helper method to create a basic execution for testing
func (suite *ShardingTaskExecutionSuite) createBasicExecution(taskID int64) dao.TaskExecution {
	return dao.TaskExecution{
		TaskID:                  taskID,
		TaskName:                "TestTask",
		TaskCronExpr:            "*/5 * * * * ?",
		TaskExecutionMethod:     domain.TaskExecutionMethodLocal.String(),
		TaskMaxExecutionSeconds: 3600,
		TaskVersion:             1,
		TaskScheduleNodeID:      "test-node",
		TaskPlanExecID:          0,
		TaskPlanID:              0,
		Deadline:                time.Now().Add(time.Hour).UnixMilli(),
		Status:                  dao.TaskExecutionStatusPrepare,
	}
}

func (suite *ShardingTaskExecutionSuite) TestCreate() {
	// First create a task
	task := suite.createTask(200, "TestExecutionTask")

	// Create an execution for the task
	execution := suite.createBasicExecution(task.ID)

	// Create the execution record
	createdExecution, err := suite.shardingTaskExecutionDAO.Create(suite.T().Context(), execution)
	require.NoError(suite.T(), err)

	// Verify the execution was created
	assert.NotZero(suite.T(), createdExecution.ID)
}

func (suite *ShardingTaskExecutionSuite) TestCreateShardingParent() {
	// First create a task
	task := suite.createTask(301, "TestShardingParentTask")

	// Create a sharding parent execution
	execution := suite.createBasicExecution(task.ID)

	// Create the sharding parent execution
	createdExecution, err := suite.shardingTaskExecutionDAO.CreateShardingParent(suite.T().Context(), execution)
	require.NoError(suite.T(), err)

	// Verify the execution was created
	assert.NotZero(suite.T(), createdExecution.ID)
	assert.True(suite.T(), createdExecution.ShardingParentID.Valid)
	assert.Equal(suite.T(), int64(0), createdExecution.ShardingParentID.Int64)
}

func (suite *ShardingTaskExecutionSuite) TestBatchCreate() {
	// First create a task
	task := suite.createTask(200, "TestBatchCreateTask")

	// Create multiple executions
	execution1 := suite.createBasicExecution(task.ID)
	execution1.TaskName = "BatchExec1"

	execution2 := suite.createBasicExecution(task.ID)
	execution2.TaskName = "BatchExec2"

	// Batch create the executions
	executions, err := suite.shardingTaskExecutionDAO.BatchCreate(suite.T().Context(), []dao.TaskExecution{execution1, execution2})
	require.NoError(suite.T(), err)

	// Verify the executions were created
	assert.Len(suite.T(), executions, 2)
	for _, exec := range executions {
		assert.NotZero(suite.T(), exec.ID)
	}
}

func (suite *ShardingTaskExecutionSuite) TestGetByID() {
	// First create a task
	task := suite.createTask(200, "TestGetExecutionByIDTask")

	// Create an execution
	execution := suite.createBasicExecution(task.ID)

	// Create the execution record
	createdExecution, err := suite.shardingTaskExecutionDAO.Create(suite.T().Context(), execution)
	require.NoError(suite.T(), err)

	// Get the execution by ID
	retrievedExecution, err := suite.shardingTaskExecutionDAO.GetByID(suite.T().Context(), createdExecution.ID)
	require.NoError(suite.T(), err)

	// Verify the execution was retrieved correctly
	assert.Equal(suite.T(), createdExecution.ID, retrievedExecution.ID)
	assert.Equal(suite.T(), task.ID, retrievedExecution.TaskID)
}

func (suite *ShardingTaskExecutionSuite) TestUpdateStatus() {
	// First create a task
	task := suite.createTask(200, "TestUpdateStatusTask")

	// Create an execution
	execution := suite.createBasicExecution(task.ID)

	// Create the execution record
	createdExecution, err := suite.shardingTaskExecutionDAO.Create(suite.T().Context(), execution)
	require.NoError(suite.T(), err)

	// Update the status
	err = suite.shardingTaskExecutionDAO.UpdateStatus(suite.T().Context(), createdExecution.ID, dao.TaskExecutionStatusRunning)
	require.NoError(suite.T(), err)

	// Verify the status was updated
	updatedExecution, err := suite.shardingTaskExecutionDAO.GetByID(suite.T().Context(), createdExecution.ID)
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), dao.TaskExecutionStatusRunning, updatedExecution.Status)
}

func (suite *ShardingTaskExecutionSuite) TestFindRetryableExecutions() {
	// First create a task
	task := suite.createTask(200, "TestFindRetryableTask")

	// Create an execution that is retryable
	execution := suite.createBasicExecution(task.ID)
	execution.Status = dao.TaskExecutionStatusFailedRetryable
	execution.NextRetryTime = time.Now().Add(-time.Minute).UnixMilli() // Set to past time to make it retryable

	// Create the execution record
	_, err := suite.shardingTaskExecutionDAO.Create(suite.T().Context(), execution)
	require.NoError(suite.T(), err)

	// Get the sharding destination based on the task's ID
	shardingID := task.ID % 1024 // Using the shardingNumber constant from the DAO
	dst := suite.shardingTaskExecutionDAO.GetShardingStrategy().Shard(shardingID)

	// Create a context with the sharding destination
	ctxWithDst := sharding.CtxWithDst(suite.T().Context(), dst)

	// Find retryable executions
	retryableExecutions, err := suite.shardingTaskExecutionDAO.FindRetryableExecutions(ctxWithDst, 10)
	require.NoError(suite.T(), err)

	// There should be at least one retryable execution
	assert.GreaterOrEqual(suite.T(), len(retryableExecutions), 1)
}

func (suite *ShardingTaskExecutionSuite) TestFindShardingParents() {
	// First create a task
	task := suite.createTask(200, "TestFindShardingParentsTask")

	// Create a sharding parent execution
	execution := suite.createBasicExecution(task.ID)
	execution.Status = dao.TaskExecutionStatusRunning
	execution.ShardingParentID = sql.NullInt64{Int64: 0, Valid: true}

	// Create the sharding parent execution
	_, err := suite.shardingTaskExecutionDAO.CreateShardingParent(suite.T().Context(), execution)
	require.NoError(suite.T(), err)

	// Get the sharding destination based on the task's ID
	shardingID := task.ID % 1024 // Using the shardingNumber constant from the DAO
	dst := suite.shardingTaskExecutionDAO.GetShardingStrategy().Shard(shardingID)

	// Create a context with the sharding destination
	ctxWithDst := sharding.CtxWithDst(suite.T().Context(), dst)

	// Find sharding parents
	shardingParents, err := suite.shardingTaskExecutionDAO.FindShardingParents(ctxWithDst, 0, 10)
	require.NoError(suite.T(), err)

	// There should be at least one sharding parent
	assert.GreaterOrEqual(suite.T(), len(shardingParents), 1)
}

func (suite *ShardingTaskExecutionSuite) TestFindShardingChildren() {
	// First create a task
	task := suite.createTask(200, "TestFindShardingChildrenTask")

	// Create a sharding parent execution
	parentExecution := suite.createBasicExecution(task.ID)
	parentExecution.ShardingParentID = sql.NullInt64{Int64: 0, Valid: true}

	// Create the sharding parent execution
	createdParent, err := suite.shardingTaskExecutionDAO.CreateShardingParent(suite.T().Context(), parentExecution)
	require.NoError(suite.T(), err)

	// Create child executions
	childExecution1 := suite.createBasicExecution(task.ID)
	childExecution1.ShardingParentID = sql.NullInt64{Int64: createdParent.ID, Valid: true}

	childExecution2 := suite.createBasicExecution(task.ID)
	childExecution2.ShardingParentID = sql.NullInt64{Int64: createdParent.ID, Valid: true}

	// Create the child executions
	_, err = suite.shardingTaskExecutionDAO.Create(suite.T().Context(), childExecution1)
	require.NoError(suite.T(), err)

	_, err = suite.shardingTaskExecutionDAO.Create(suite.T().Context(), childExecution2)
	require.NoError(suite.T(), err)

	// Find sharding children
	children, err := suite.shardingTaskExecutionDAO.FindShardingChildren(suite.T().Context(), createdParent.ID)
	require.NoError(suite.T(), err)

	// There should be 2 children
	assert.Len(suite.T(), children, 2)
}

func (suite *ShardingTaskExecutionSuite) TestUpdateRetryResult() {
	// First create a task
	task := suite.createTask(200, "TestUpdateRetryResultTask")

	// Create an execution
	execution := suite.createBasicExecution(task.ID)

	// Create the execution record
	createdExecution, err := suite.shardingTaskExecutionDAO.Create(suite.T().Context(), execution)
	require.NoError(suite.T(), err)

	// Update the retry result
	retryCount := int64(1)
	nextRetryTime := time.Now().Add(time.Minute).UnixMilli()
	status := dao.TaskExecutionStatusFailedRetryable
	progress := int32(50)
	endTime := time.Now().UnixMilli()
	executorNodeID := "test-executor"

	err = suite.shardingTaskExecutionDAO.UpdateRetryResult(
		suite.T().Context(),
		createdExecution.ID,
		retryCount,
		nextRetryTime,
		status,
		progress,
		endTime,
		nil,
		executorNodeID,
	)
	require.NoError(suite.T(), err)

	// Verify the retry result was updated
	updatedExecution, err := suite.shardingTaskExecutionDAO.GetByID(suite.T().Context(), createdExecution.ID)
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), retryCount, updatedExecution.RetryCount)
	assert.Equal(suite.T(), nextRetryTime, updatedExecution.NextRetryTime)
	assert.Equal(suite.T(), status, updatedExecution.Status)
	assert.Equal(suite.T(), progress, updatedExecution.RunningProgress)
	assert.Equal(suite.T(), endTime, updatedExecution.Etime)
	assert.Equal(suite.T(), executorNodeID, updatedExecution.ExecutorNodeID.String)
}

func (suite *ShardingTaskExecutionSuite) TestSetRunningState() {
	// First create a task
	task := suite.createTask(200, "TestSetRunningStateTask")

	// Create an execution
	execution := suite.createBasicExecution(task.ID)

	// Create the execution record
	createdExecution, err := suite.shardingTaskExecutionDAO.Create(suite.T().Context(), execution)
	require.NoError(suite.T(), err)

	// Set running state
	progress := int32(25)
	executorNodeID := "test-executor"

	err = suite.shardingTaskExecutionDAO.SetRunningState(
		suite.T().Context(),
		createdExecution.ID,
		progress,
		executorNodeID,
	)
	require.NoError(suite.T(), err)

	// Verify the running state was set
	updatedExecution, err := suite.shardingTaskExecutionDAO.GetByID(suite.T().Context(), createdExecution.ID)
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), dao.TaskExecutionStatusRunning, updatedExecution.Status)
	assert.Equal(suite.T(), progress, updatedExecution.RunningProgress)
	assert.Equal(suite.T(), executorNodeID, updatedExecution.ExecutorNodeID.String)
	assert.NotZero(suite.T(), updatedExecution.Stime)
}

func (suite *ShardingTaskExecutionSuite) TestUpdateProgress() {
	// First create a task
	task := suite.createTask(200, "TestUpdateProgressTask")

	// Create an execution
	execution := suite.createBasicExecution(task.ID)
	execution.Status = dao.TaskExecutionStatusRunning

	// Create the execution record
	createdExecution, err := suite.shardingTaskExecutionDAO.Create(suite.T().Context(), execution)
	require.NoError(suite.T(), err)

	// Update progress
	progress := int32(75)

	err = suite.shardingTaskExecutionDAO.UpdateProgress(
		suite.T().Context(),
		createdExecution.ID,
		progress,
	)
	require.NoError(suite.T(), err)

	// Verify the progress was updated
	updatedExecution, err := suite.shardingTaskExecutionDAO.GetByID(suite.T().Context(), createdExecution.ID)
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), progress, updatedExecution.RunningProgress)
}

//func (suite *ShardingTaskExecutionSuite) TestUpdateScheduleResult() {
//	// First create a task
//	task := suite.createTask(200, "TestUpdateScheduleResultTask")
//
//	// Create an execution
//	execution := suite.createBasicExecution(task.ID)
//
//	// Create the execution record
//	createdExecution, err := suite.shardingTaskExecutionDAO.Create(suite.T().Context(), execution)
//	require.NoError(suite.T(), err)
//
//	// Update schedule result
//	status := dao.TaskExecutionStatusSuccess
//	progress := int32(100)
//	endTime := time.Now().UnixMilli()
//	scheduleParams := map[string]string{"result": "success"}
//	executorNodeID := "test-executor"
//
//	err = suite.shardingTaskExecutionDAO.UpdateScheduleResult(
//		suite.T().Context(),
//		createdExecution.ID,
//		status,
//		progress,
//		endTime,
//		scheduleParams,
//		executorNodeID,
//	)
//	require.NoError(suite.T(), err)
//
//	// Verify the schedule result was updated
//	updatedExecution, err := suite.shardingTaskExecutionDAO.GetByID(suite.T().Context(), createdExecution.ID)
//	require.NoError(suite.T(), err)
//	assert.Equal(suite.T(), status, updatedExecution.Status)
//	assert.Equal(suite.T(), progress, updatedExecution.RunningProgress)
//	assert.Equal(suite.T(), endTime, updatedExecution.Etime)
//	assert.Equal(suite.T(), scheduleParams, updatedExecution.TaskScheduleParams.Val)
//	assert.Equal(suite.T(), executorNodeID, updatedExecution.ExecutorNodeID.String)
//}

func (suite *ShardingTaskExecutionSuite) TestFindReschedulableExecutions() {
	// First create a task
	task := suite.createTask(200, "TestFindReschedulableTask")

	// Create an execution that is reschedulable
	execution := suite.createBasicExecution(task.ID)
	execution.Status = dao.TaskExecutionStatusFailedRescheduled

	// Create the execution record
	_, err := suite.shardingTaskExecutionDAO.Create(suite.T().Context(), execution)
	require.NoError(suite.T(), err)

	// Get the sharding destination based on the task's ID
	shardingID := task.ID % 1024 // Using the shardingNumber constant from the DAO
	dst := suite.shardingTaskExecutionDAO.GetShardingStrategy().Shard(shardingID)

	// Create a context with the sharding destination
	ctxWithDst := sharding.CtxWithDst(suite.T().Context(), dst)

	// Find reschedulable executions
	reschedulableExecutions, err := suite.shardingTaskExecutionDAO.FindReschedulableExecutions(ctxWithDst, 10)
	require.NoError(suite.T(), err)

	// There should be at least one reschedulable execution
	assert.GreaterOrEqual(suite.T(), len(reschedulableExecutions), 1)
}

func (suite *ShardingTaskExecutionSuite) TestFindExecutionByPlanID() {
	// First create a task with a plan ID
	task := dao.Task{
		BizID:               200,
		Name:                "TestPlanTask",
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

	createdTask, err := suite.shardingTaskDAO.Create(suite.T().Context(), task)
	require.NoError(suite.T(), err)

	// Create an execution with a plan execution ID
	planExecID := int64(12345)
	execution := suite.createBasicExecution(createdTask.ID)
	execution.TaskPlanExecID = planExecID

	// Create the execution record
	_, err = suite.shardingTaskExecutionDAO.Create(suite.T().Context(), execution)
	require.NoError(suite.T(), err)

	// Find executions by plan ID
	executions, err := suite.shardingTaskExecutionDAO.FindExecutionByPlanID(suite.T().Context(), planExecID)
	require.NoError(suite.T(), err)

	// There should be at least one execution
	assert.NotEmpty(suite.T(), executions)
	assert.Contains(suite.T(), executions, createdTask.ID)
}

func (suite *ShardingTaskExecutionSuite) TestFindByTaskID() {
	// First create a task
	task := suite.createTask(200, "TestFindByTaskIDTask")

	// Create multiple executions for the task
	execution1 := suite.createBasicExecution(task.ID)
	execution1.TaskName = "Execution1"

	execution2 := suite.createBasicExecution(task.ID)
	execution2.TaskName = "Execution2"

	// Create the execution records
	_, err := suite.shardingTaskExecutionDAO.Create(suite.T().Context(), execution1)
	require.NoError(suite.T(), err)

	_, err = suite.shardingTaskExecutionDAO.Create(suite.T().Context(), execution2)
	require.NoError(suite.T(), err)

	// Find executions by task ID
	executions, err := suite.shardingTaskExecutionDAO.FindByTaskID(suite.T().Context(), task.ID)
	require.NoError(suite.T(), err)

	// There should be at least 2 executions
	assert.GreaterOrEqual(suite.T(), len(executions), 2)
}

func (suite *ShardingTaskExecutionSuite) TestFindTimeoutExecutions() {
	// First create a task
	task := suite.createTask(200, "TestFindTimeoutTask")

	// Create an execution that is running but has timed out
	execution := suite.createBasicExecution(task.ID)
	execution.Status = dao.TaskExecutionStatusRunning
	execution.Deadline = time.Now().Add(-time.Minute).UnixMilli() // Set to past time to make it timed out

	// Create the execution record
	_, err := suite.shardingTaskExecutionDAO.Create(suite.T().Context(), execution)
	require.NoError(suite.T(), err)

	// Get the sharding destination based on the task's ID
	shardingID := task.ID % 1024 // Using the shardingNumber constant from the DAO
	dst := suite.shardingTaskExecutionDAO.GetShardingStrategy().Shard(shardingID)

	// Create a context with the sharding destination
	ctxWithDst := sharding.CtxWithDst(suite.T().Context(), dst)

	// Find timeout executions
	timeoutExecutions, err := suite.shardingTaskExecutionDAO.FindTimeoutExecutions(ctxWithDst, 10)
	require.NoError(suite.T(), err)

	// There should be at least one timeout execution
	assert.GreaterOrEqual(suite.T(), len(timeoutExecutions), 1)
}

func TestShardingTaskExecutionSuite(t *testing.T) {
	suite.Run(t, new(ShardingTaskExecutionSuite))
}
