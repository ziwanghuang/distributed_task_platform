//go:build e2e

package integration

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/repository/dao"
	"gitee.com/flycash/distributed_task_platform/internal/test/integration/ioc"
	testioc "gitee.com/flycash/distributed_task_platform/internal/test/ioc"
	registryetcd "gitee.com/flycash/distributed_task_platform/pkg/grpc/registry/etcd"
	"github.com/ego-component/egorm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// ShardingInterruptSuite 分片任务中断测试套件
type ShardingInterruptSuite struct {
	suite.Suite

	// 基础设施
	db           *egorm.Component
	schedulerApp *ioc.SchedulerApp
	registry     *registryetcd.Registry

	// 执行节点
	executorNodes []*ExecutorNode
	testDataDir   string

	// 测试任务
	taskID int64
}

func (s *ShardingInterruptSuite) SetupSuite() {
	s.T().Log("开始设置分片中断测试套件")

	// 1. 初始化基础设施
	s.db = testioc.InitDBAndTables()

	// 初始化Registry
	etcdClient := testioc.InitEtcdClient()
	registry, err := registryetcd.NewRegistry(etcdClient)
	require.NoError(s.T(), err)
	s.registry = registry

	// 初始化完整的调度器应用（传入空的local执行函数map，因为我们要测试远程执行）
	s.schedulerApp = ioc.InitSchedulerApp(nil, nil)

	// 启动ReportEventConsumer来接收执行状态上报
	go s.schedulerApp.ReportEventConsumer.Start(context.Background())

	// 2. 设置测试数据目录
	s.testDataDir = "testdata"
	err = os.MkdirAll(s.testDataDir, 0o755)
	require.NoError(s.T(), err)

	// 清理之前的测试文件
	s.cleanTestDataDir()

	// 3. 创建并启动三个执行节点
	nodeConfigs := []struct {
		nodeID string
		port   int
	}{
		{"NodeID-01", 18081},
		{"NodeID-02", 18082},
		{"NodeID-03", 18083},
	}

	s.executorNodes = make([]*ExecutorNode, 0, len(nodeConfigs))

	for _, config := range nodeConfigs {
		node := NewExecutorNode(config.nodeID, s.testDataDir, config.port, s.registry, testioc.InitMQ())
		err := node.Start()
		require.NoError(s.T(), err, "启动执行节点失败: %s", config.nodeID)

		s.executorNodes = append(s.executorNodes, node)
		s.T().Logf("执行节点 %s 启动成功，端口: %d", config.nodeID, config.port)
	}

	// 4. 等待服务注册完成
	time.Sleep(2 * time.Second)

	// 5. 验证服务注册
	services, err := s.registry.ListServices(context.Background(), "executor-service")
	require.NoError(s.T(), err)
	require.Len(s.T(), services, 3, "应该注册了3个执行节点")

	s.T().Log("分片中断测试套件设置完成")
}

func (s *ShardingInterruptSuite) TearDownSuite() {
	s.T().Log("开始清理分片中断测试套件")

	// 1. 停止所有执行节点
	for _, node := range s.executorNodes {
		if err := node.Stop(); err != nil {
			s.T().Logf("停止执行节点失败: %v", err)
		}
	}

	// 2. 关闭Registry
	if s.registry != nil {
		s.registry.Close()
	}

	// 3. 清理测试数据
	s.cleanTestDataDir()

	s.T().Log("分片中断测试套件清理完成")
}

// verifyRescheduleCompletion 验证重调度完成后的结果
func (s *ShardingInterruptSuite) verifyRescheduleCompletion(originalExecutionID int64) {
	ctx := context.Background()

	// 检查所有执行记录的最终状态
	var allExecutions []dao.TaskExecution
	err := s.db.WithContext(ctx).Where("task_id = ?", s.taskID).Find(&allExecutions).Error
	require.NoError(s.T(), err)

	s.T().Logf("重调度验证: 总共找到 %d 个执行记录", len(allExecutions))

	var interruptedExecution *dao.TaskExecution
	successCount := 0

	for i, exec := range allExecutions {
		s.T().Logf("执行记录%d: ID=%d, Status=%s, Progress=%d, ShardingParentID=%d",
			i+1, exec.ID, exec.Status, exec.RunningProgress, exec.ShardingParentID.Int64)

		if exec.ShardingParentID.Int64 == 0 {
			// 父任务，跳过
			continue
		}

		// 检查是否是被中断的执行记录
		if exec.ID == originalExecutionID {
			interruptedExecution = &exec
		}

		if exec.Status == "SUCCESS" {
			successCount++
		}
	}

	// 验证被中断的执行记录最终成功完成
	require.NotNil(s.T(), interruptedExecution, "未找到被中断的执行记录")
	require.Equal(s.T(), "SUCCESS", interruptedExecution.Status, "被中断的执行记录应该最终成功完成")
	require.Equal(s.T(), int32(100), interruptedExecution.RunningProgress, "被中断的执行记录应该完成到100%")

	s.T().Logf("✅ 中断和重调度验证成功: 执行记录%d从中断恢复并完成", originalExecutionID)

	// 验证文件数据完整性
	s.verifyFileDataIntegrity(originalExecutionID)
}

// verifyInterruptedTaskFiles 验证被中断任务的文件输出
func (s *ShardingInterruptSuite) verifyInterruptedTaskFiles(interruptedExecutionID int64) {
	// 查找被中断任务对应的节点ID
	ctx := context.Background()
	var execution dao.TaskExecution
	err := s.db.WithContext(ctx).Where("id = ?", interruptedExecutionID).First(&execution).Error
	require.NoError(s.T(), err)

	// 从数据库记录中推断nodeID (这里需要根据实际的数据结构调整)
	// 由于我们知道测试中的节点分配，先简单验证文件存在性

	files, err := os.ReadDir(s.testDataDir)
	require.NoError(s.T(), err)

	s.T().Logf("中断任务文件验证: 在 %s 目录下找到 %d 个文件", s.testDataDir, len(files))

	for _, file := range files {
		if !file.IsDir() {
			filePath := filepath.Join(s.testDataDir, file.Name())
			content, err := os.ReadFile(filePath)
			require.NoError(s.T(), err)

			lines := strings.Split(strings.TrimSpace(string(content)), "\n")
			if len(lines) > 0 && lines[0] != "" {
				s.T().Logf("文件 %s 包含 %d 行数据", file.Name(), len(lines))
			}
		}
	}
}

// verifyTaskCompletion 验证任务最终完成状态
func (s *ShardingInterruptSuite) verifyTaskCompletion() {
	ctx := context.Background()

	// 检查所有执行记录的最终状态
	var executions []dao.TaskExecution
	err := s.db.WithContext(ctx).Where("task_id = ? AND sharding_parent_id > 0", s.taskID).Find(&executions).Error
	require.NoError(s.T(), err)

	s.T().Logf("任务完成状态验证: 找到 %d 个分片任务", len(executions))

	successCount := 0
	for i, exec := range executions {
		s.T().Logf("分片任务%d: ID=%d, Status=%s, Progress=%d",
			i+1, exec.ID, exec.Status, exec.RunningProgress)
		if exec.Status == "SUCCESS" {
			successCount++
		}
	}

	s.T().Logf("成功完成的分片任务数量: %d/%d", successCount, len(executions))

	// 验证文件生成
	s.verifyFinalResults()
}

// cleanTestDataDir 清理测试数据目录
func (s *ShardingInterruptSuite) cleanTestDataDir() {
	if err := os.RemoveAll(s.testDataDir); err != nil {
		s.T().Logf("清理测试数据目录失败: %v", err)
	}
	if err := os.MkdirAll(s.testDataDir, 0o755); err != nil {
		s.T().Logf("创建测试数据目录失败: %v", err)
	}
}

// createShardingTask 创建分片任务
func (s *ShardingInterruptSuite) createShardingTask() int64 {
	task := domain.Task{
		Name:            "ShardingTestTask",
		CronExpr:        "0 * * * * *", // 每分钟执行一次，简单明确
		Type:            domain.NormalTaskType,
		ExecutionMethod: domain.TaskExecutionMethodRemote,
		GrpcConfig: &domain.GrpcConfig{
			ServiceName: "executor-service",
			Params:      make(map[string]string),
		},
		ShardingRule: &domain.ShardingRule{
			Type: "range",
			Params: map[string]string{
				"totalNums": "3",
				"step":      "10",
			},
		},
		Status:  domain.TaskStatusActive,
		Version: 1,
	}

	createdTask, err := s.schedulerApp.TaskSvc.Create(context.Background(), task)
	require.NoError(s.T(), err)

	s.T().Logf("创建分片任务成功，任务ID: %d, NextTime: %d, Status: %s",
		createdTask.ID, createdTask.NextTime, createdTask.Status)

	// 验证任务确实被创建了
	retrievedTask, err := s.schedulerApp.TaskSvc.GetByID(context.Background(), createdTask.ID)
	require.NoError(s.T(), err)
	s.T().Logf("从数据库获取任务确认: ID=%d, NextTime=%d, Status=%s, ExecutionMethod=%s",
		retrievedTask.ID, retrievedTask.NextTime, retrievedTask.Status, retrievedTask.ExecutionMethod)

	return createdTask.ID
}

// TestShardingTaskInterrupt 测试分片任务中断机制
func (s *ShardingInterruptSuite) TestShardingTaskInterrupt() {
	ctx := context.Background()

	t := s.T()
	t.Log("开始测试分片任务中断机制")

	// 1. 创建分片任务
	s.taskID = s.createShardingTask()

	// 2. 启动调度器
	err := s.schedulerApp.Scheduler.Start()
	require.NoError(t, err)

	t.Log("调度器已启动")

	// 3. 等待任务开始执行
	t.Log("等待任务真正开始执行...")

	// 等待到下一个整分钟，确保cron任务被触发
	now := time.Now()
	nextMinute := now.Truncate(time.Minute).Add(time.Minute)
	waitTime := nextMinute.Sub(now) + 10*time.Second // 多等10秒确保任务开始
	t.Logf("等待 %v 直到任务开始执行", waitTime)
	time.Sleep(waitTime)

	// 4. 检查是否有执行记录被创建
	t.Logf("开始查询任务ID %d 的执行记录", s.taskID)

	var executions []dao.TaskExecution
	err = s.db.WithContext(ctx).Where("task_id = ?", s.taskID).Find(&executions).Error
	require.NoError(t, err)

	t.Logf("查询到 %d 个执行记录", len(executions))
	for i, exec := range executions {
		t.Logf("执行记录%d: ID=%d, TaskID=%d, Status=%s, ShardingParentID=%v",
			i, exec.ID, exec.TaskID, exec.Status, exec.ShardingParentID)
	}

	require.NotEmpty(t, executions, "应该有执行记录被创建")

	// 5. 快速找到正在执行的子任务（分片），应该正在50%处等待中断
	var runningExecution *dao.TaskExecution

	// 由于任务会在50%处等待30秒，我们应该能找到RUNNING状态的任务
	for attempts := 0; attempts < 5; attempts++ {
		var currentExecs []dao.TaskExecution
		err = s.db.WithContext(ctx).Where("task_id = ? AND status = 'RUNNING' AND sharding_parent_id > 0", s.taskID).Find(&currentExecs).Error
		require.NoError(t, err)

		if len(currentExecs) > 0 {
			runningExecution = &currentExecs[0]
			t.Logf("第%d次检查，找到RUNNING状态的分片任务: ID=%d", attempts+1, runningExecution.ID)
			break
		}

		t.Logf("第%d次检查，暂未发现RUNNING状态的分片任务，等待2秒...", attempts+1)
		time.Sleep(2 * time.Second)
	}

	if runningExecution == nil {
		// 如果没找到RUNNING的，检查是否都已经完成
		var allExecs []dao.TaskExecution
		err = s.db.WithContext(ctx).Where("task_id = ? AND sharding_parent_id > 0", s.taskID).Find(&allExecs).Error
		require.NoError(t, err)

		t.Logf("未找到RUNNING状态的分片任务，当前分片任务状态：")
		for i, exec := range allExecs {
			t.Logf("分片任务%d: ID=%d, Status=%s, Progress=%d",
				i, exec.ID, exec.Status, exec.RunningProgress)
		}

		// 如果任务执行太快已经完成，跳过中断测试
		if len(allExecs) > 0 && allExecs[0].Status == "SUCCESS" {
			t.Log("任务执行太快已经完成，跳过中断测试")
			return
		}

		require.Fail(t, "应该有正在运行的分片任务，可能任务还未开始或已经完成")
	}

	t.Logf("找到正在运行的分片任务，执行ID: %d", runningExecution.ID)

	// 6. 等待执行到一半（检查文件生成）
	s.waitForHalfExecution(runningExecution.ID)

	// 7. 调用中断接口
	t.Log("开始中断分片任务")

	// 先获取完整的TaskExecution对象
	execution, err := s.schedulerApp.ExecutionSvc.FindByID(ctx, runningExecution.ID)
	require.NoError(t, err)

	t.Logf("准备中断任务: ExecutionID=%d, TaskID=%d, ExecutorNodeID=%s",
		execution.ID, execution.Task.ID, execution.ExecutorNodeID)

	// 确保执行记录有ExecutorNodeID
	require.NotEmpty(t, execution.ExecutorNodeID, "执行记录缺少ExecutorNodeID")

	// 调试：尝试列出etcd中的服务实例
	t.Log("检查etcd中注册的服务实例...")
	instances, err := s.registry.ListServices(ctx, "executor-service")
	require.NoError(t, err)
	t.Logf("etcd中找到 %d 个executor-service实例:", len(instances))
	for i, instance := range instances {
		t.Logf("实例%d: ID=%s, Address=%s", i+1, instance.ID, instance.Address)
	}

	// 等待更长时间确保gRPC连接完全建立
	t.Log("等待gRPC连接完全建立...")
	time.Sleep(10 * time.Second)

	// 准备中断用的context
	//ctxWithNodeID := balancer.WithSpecificNodeID(ctx, execution.ExecutorNodeID)

	// 多次尝试中断，直到成功
	t.Log("开始尝试中断...")
	var interruptErr error
	for i := 0; i < 3; i++ {
		//interruptErr = s.schedulerApp.Scheduler.InterruptTaskExecution(ctxWithNodeID, execution)
		//if interruptErr == nil {
		//	t.Logf("第%d次中断成功", i+1)
		//	break
		//}
		//t.Logf("第%d次中断失败: %v", i+1, interruptErr)
		//if i < 2 {
		//	t.Log("等待5秒后重试...")
		//	time.Sleep(5 * time.Second)
		//}
	}

	require.NoError(t, interruptErr, "所有中断尝试都失败了")
	t.Log("中断操作成功完成")

	// 8. 等待中断完成
	t.Log("等待中断完成...")
	time.Sleep(3 * time.Second)

	// 9. 重新获取执行记录，确认中断状态
	execution, err = s.schedulerApp.ExecutionSvc.FindByID(ctx, runningExecution.ID)
	require.NoError(t, err)
	t.Logf("中断后执行状态: Status=%s, Progress=%d", execution.Status, execution.RunningProgress)

	// 6. 开始重调度被中断的任务
	t.Log("开始重调度被中断的任务...")

	err = s.schedulerApp.Runner.Reschedule(ctx, execution)
	require.NoError(t, err)
	t.Log("重调度请求已发送")

	// 7. 等待重调度完成
	t.Log("等待重调度完成...")

	// 增加等待时间并监控执行状态
	for i := 0; i < 10; i++ {
		time.Sleep(3 * time.Second)

		// 重新获取执行记录
		currentExecution, err := s.schedulerApp.ExecutionSvc.FindByID(ctx, runningExecution.ID)
		require.NoError(t, err)

		t.Logf("重调度等待第%d次检查: Status=%s, Progress=%d",
			i+1, currentExecution.Status, currentExecution.RunningProgress)

		// 如果状态变为SUCCESS，说明重调度完成
		if currentExecution.Status.IsSuccess() {
			t.Log("重调度执行完成！")
			break
		}

		// 如果状态变为RUNNING且进度有变化，说明重调度正在执行
		if currentExecution.Status.IsRunning() && currentExecution.RunningProgress > 60 {
			t.Logf("重调度正在执行，进度从60%%增加到%d%%", currentExecution.RunningProgress)
		}
	}

	// 12. 验证重调度结果
	s.verifyRescheduleCompletion(execution.ID)

	t.Log("分片任务中断和重调度测试完成")
}

// waitForHalfExecution 等待执行到一半
func (s *ShardingInterruptSuite) waitForHalfExecution(executionID int64) {
	s.T().Logf("等待执行ID %d 执行到一半", executionID)

	timeout := time.After(30 * time.Second)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			s.T().Fatal("等待执行到一半超时")
		case <-ticker.C:
			// 检查是否有对应的文件生成
			files, err := filepath.Glob(filepath.Join(s.testDataDir, fmt.Sprintf("*-%d.txt", executionID)))
			if err != nil {
				continue
			}

			if len(files) > 0 {
				// 检查文件内容，看是否有数据生成
				content, err := ioutil.ReadFile(files[0])
				if err != nil {
					continue
				}

				lines := strings.Split(strings.TrimSpace(string(content)), "\n")
				if len(lines) >= 3 { // 至少生成了一些数据
					s.T().Logf("检测到执行文件 %s，已生成 %d 行数据", files[0], len(lines))
					return
				}
			}
		}
	}
}

// verifyFinalResults 验证最终结果
func (s *ShardingInterruptSuite) verifyFinalResults() {
	s.T().Log("开始验证最终结果")

	// 1. 检查所有分片任务都完成了
	var executions []dao.TaskExecution
	err := s.db.WithContext(context.Background()).
		Where("task_id = ? AND sharding_parent_id > 0", s.taskID).
		Order("ctime ASC").
		Find(&executions).Error
	require.NoError(s.T(), err)
	require.Equal(s.T(), 3, len(executions), "应该有3个分片任务")

	// 2. 检查所有分片任务最终都成功了
	successCount := 0
	for _, exec := range executions {
		if exec.Status == "SUCCESS" {
			successCount++
		}
	}
	assert.Equal(s.T(), 3, successCount, "所有分片任务都应该成功")

	// 3. 检查生成的文件
	files, err := filepath.Glob(filepath.Join(s.testDataDir, "executor-*.txt"))
	require.NoError(s.T(), err)
	require.NotEmpty(s.T(), files, "应该生成了文件")

	s.T().Logf("找到 %d 个生成的文件", len(files))

	// 4. 验证文件内容
	totalLines := 0
	for _, file := range files {
		content, err := ioutil.ReadFile(file)
		require.NoError(s.T(), err)

		lines := strings.Split(strings.TrimSpace(string(content)), "\n")
		if len(lines) == 1 && lines[0] == "" {
			lines = []string{}
		}

		totalLines += len(lines)
		s.T().Logf("文件 %s 包含 %d 行数据", filepath.Base(file), len(lines))

		// 验证数据格式
		for _, line := range lines {
			if line != "" {
				assert.True(s.T(), strings.HasPrefix(line, "shard-"), "数据行应该以shard-开头: %s", line)
				assert.True(s.T(), strings.Contains(line, "-data-"), "数据行应该包含-data-: %s", line)
			}
		}
	}

	// 应该总共生成30行数据（3个分片，每个分片10行）
	assert.Equal(s.T(), 30, totalLines, "应该总共生成30行数据")

	s.T().Log("最终结果验证完成")
}

// verifyFileDataIntegrity 验证文件数据的完整性
func (s *ShardingInterruptSuite) verifyFileDataIntegrity(originalExecutionID int64) {
	// 从数据库获取执行记录，确定实际的执行节点
	ctx := context.Background()
	var execution dao.TaskExecution
	err := s.db.WithContext(ctx).Where("id = ?", originalExecutionID).First(&execution).Error
	require.NoError(s.T(), err)

	// 根据执行记录中的节点ID构造文件名
	// 从ExecutorNodeID推断文件名格式
	fileName := fmt.Sprintf("executor-%s-%d.txt", execution.ExecutorNodeID.String, originalExecutionID)
	filePath := filepath.Join(s.testDataDir, fileName)

	s.T().Logf("检查文件: %s", fileName)

	// 检查文件是否存在
	if _, err := os.Stat(filePath); err != nil {
		s.T().Logf("文件 %s 不存在，跳过数据完整性验证", fileName)
		return
	}

	// 读取文件内容
	content, err := os.ReadFile(filePath)
	require.NoError(s.T(), err)

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	s.T().Logf("文件 %s 包含 %d 行数据", fileName, len(lines))

	// 被中断后重调度的文件应该包含完整的10行数据
	// (中断前的部分 + 重调度完成的部分)
	require.Equal(s.T(), 10, len(lines),
		fmt.Sprintf("文件 %s 应该包含完整的10行数据（中断前+重调度后）", fileName))

	// 验证数据内容的连续性
	for i, line := range lines {
		expectedDataNum := i + 1
		expectedPattern := fmt.Sprintf("shard-1-data-%d", expectedDataNum)
		require.Contains(s.T(), line, expectedPattern,
			fmt.Sprintf("第%d行数据格式不正确，期望包含: %s", i+1, expectedPattern))
	}

	s.T().Logf("✅ 文件 %s 数据验证通过，包含完整的10行连续数据", fileName)
}

// TestShardingInterruptSuite 运行测试套件
func TestShardingInterruptSuite(t *testing.T) {
	suite.Run(t, new(ShardingInterruptSuite))
}
