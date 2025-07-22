//go:build e2e

package picker

import (
	"context"
	"fmt"
	"testing"
	"time"

	"gitee.com/flycash/distributed_task_platform/internal/service/picker"
	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// PickerIntegrationSuite picker 集成测试套件
type PickerIntegrationSuite struct {
	suite.Suite
	nodes      []*MockExecutorNode // 所有模拟执行节点
	promClient v1.API
}

// SetupSuite 在所有测试开始前运行一次，负责环境搭建
func (s *PickerIntegrationSuite) SetupSuite() {
	t := s.T()
	t.Log("开始设置 Picker 集成测试套件")
	t.Log("注意：此测试需要运行 docker-compose up prometheus，请确保Prometheus服务在localhost:9090运行")

	// 1. 初始化Prometheus客户端
	client, err := api.NewClient(api.Config{Address: "http://localhost:9090"})
	require.NoError(t, err, "创建Prometheus API Client失败")
	s.promClient = v1.NewAPI(client)

	// 2. 创建所有模拟执行节点
	s.nodes = []*MockExecutorNode{
		NewMockExecutorNode("cpu-node-01", CPUMetricType),
		NewMockExecutorNode("cpu-node-02", CPUMetricType),
		NewMockExecutorNode("cpu-node-03", CPUMetricType),
		NewMockExecutorNode("memory-node-01", MemoryMetricType),
		NewMockExecutorNode("memory-node-02", MemoryMetricType),
		NewMockExecutorNode("memory-node-03", MemoryMetricType),
	}

	// 3. 启动所有节点的指标上报
	for _, node := range s.nodes {
		err := node.StartMetricsReporting()
		require.NoError(t, err)
	}

	// 4. 使用Eventually轮询，等待Prometheus抓取到所有节点的'up'指标
	t.Log("等待Prometheus抓取到初始指标...")
	require.Eventually(t, func() bool {
		query := fmt.Sprintf(`count(up{job="executors"} == 1)`)
		result, _, err := s.promClient.Query(context.Background(), query, time.Now())
		if err != nil {
			t.Logf("轮询中... Prometheus查询失败: %v", err)
			return false
		}
		t.Logf("轮询中... Prometheus 'up' 指标查询结果: %v", result)
		return err == nil
	}, 45*time.Second, 5*time.Second, "Prometheus未能在45秒内连接成功或抓取到所有节点的'up'指标")

	t.Log("Picker 集成测试套件设置完成")
}

// TearDownSuite 在所有测试结束后运行一次，负责环境清理
func (s *PickerIntegrationSuite) TearDownSuite() {
	s.T().Log("开始清理 Picker 集成测试套件")
	for _, node := range s.nodes {
		node.Stop()
	}
	s.T().Log("Picker 集成测试套件清理完成")
}

// TestPickerStrategies 使用表驱动测试统一验证所有策略
func (s *PickerIntegrationSuite) TestPickerStrategies() {
	// 为测试定义一个更短的时间窗口，以快速响应指标变化
	const testTimeWindow = 15 * time.Second

	testCases := []struct {
		name                 string
		config               picker.Config
		setupMetrics         func()   // 用于设置本次测试指标的函数
		expectedCandidates   []string // 期望被选中的节点池
		unexpectedCandidates []string // 明确不应该被选中的节点
	}{
		{
			name: "CPU优先策略应只选择Top2节点",
			config: picker.Config{
				Strategy:       picker.StrategyCPUPriority,
				JobName:        "executors",
				TopNCandidates: 2,
				TimeWindow:     testTimeWindow, // 使用为测试优化的短窗口
				QueryTimeout:   5 * time.Second,
			},
			setupMetrics: func() {
				s.findNode("cpu-node-01").SetCPUIdlePercent(95) // 最优
				s.findNode("cpu-node-02").SetCPUIdlePercent(85) // 次优
				s.findNode("cpu-node-03").SetCPUIdlePercent(30) // 最差
			},
			expectedCandidates:   []string{"cpu-node-01", "cpu-node-02"},
			unexpectedCandidates: []string{"cpu-node-03"},
		},
		{
			name: "内存优先策略应只选择Top2节点",
			config: picker.Config{
				Strategy:       picker.StrategyMemoryPriority,
				JobName:        "executors",
				TopNCandidates: 2,
				TimeWindow:     testTimeWindow, // 使用为测试优化的短窗口
				QueryTimeout:   5 * time.Second,
			},
			setupMetrics: func() {
				s.findNode("memory-node-01").SetAvailableMemoryBytes(8e9) // 8GB - 最优
				s.findNode("memory-node-02").SetAvailableMemoryBytes(4e9) // 4GB - 次优
				s.findNode("memory-node-03").SetAvailableMemoryBytes(1e9) // 1GB - 最差
			},
			expectedCandidates:   []string{"memory-node-01", "memory-node-02"},
			unexpectedCandidates: []string{"memory-node-03"},
		},
		{
			name: "无效策略应降级为CPU优先并选择Top2节点",
			config: picker.Config{
				Strategy:       "invalid_strategy", // 无效策略
				JobName:        "executors",
				TopNCandidates: 2,
				TimeWindow:     testTimeWindow, // 使用为测试优化的短窗口
				QueryTimeout:   5 * time.Second,
			},
			setupMetrics: func() {
				s.findNode("cpu-node-01").SetCPUIdlePercent(99) // 确保CPU节点有明确的排序
				s.findNode("cpu-node-02").SetCPUIdlePercent(88)
				s.findNode("cpu-node-03").SetCPUIdlePercent(22)
			},
			expectedCandidates:   []string{"cpu-node-01", "cpu-node-02"},
			unexpectedCandidates: []string{"cpu-node-03"},
		},
	}

	// 统一的测试执行逻辑
	for _, tc := range testCases {
		s.T().Run(tc.name, func(t *testing.T) {
			// 1. 设置本次测试场景的指标
			tc.setupMetrics()

			// 增加短暂等待，确保至少一个新指标数据点已被推送
			t.Log("等待指标预热...")
			time.Sleep(6 * time.Second) // 等待时间略长于mock节点的推送间隔(5s)

			// 2. 创建Picker Dispatcher
			dispatcher := picker.NewDispatcher(s.promClient, tc.config)

			// 3. 使用Eventually轮询，等待并验证选择结果的稳定性
			iterations := 20 // 多次选择以验证随机性和稳定性
			require.Eventually(t, func() bool {
				selectedNodes := make(map[string]int)
				for i := 0; i < iterations; i++ {
					nodeID, err := dispatcher.Pick(context.Background())
					if err != nil {
						t.Logf("轮询中... Pick失败: %v", err)
						return false // 在Eventually中，任何错误都应视为“还未就绪”，返回false让其继续尝试
					}
					selectedNodes[nodeID]++
				}

				t.Logf("策略 [%s] 的选择结果: %+v", tc.name, selectedNodes)

				// 4. 进行精确验证，使用无副作用的assert
				// a. 必须有选择结果
				if !assert.NotEmpty(t, selectedNodes, "选择结果不应为空") {
					return false
				}
				// b. 随机选择的结果应该覆盖所有期望的候选节点（验证防惊群）
				if !assert.Len(t, selectedNodes, len(tc.expectedCandidates), "选择范围不等于期望的候选池大小") {
					return false
				}
				// c. 所有被选中的节点都必须在期望的候选池中
				for nodeID := range selectedNodes {
					if !assert.Contains(t, tc.expectedCandidates, nodeID, "选中了非预期的节点") {
						return false
					}
				}
				// d. 绝不应该选中不期望的节点
				for _, unexpectedNode := range tc.unexpectedCandidates {
					if !assert.NotContains(t, selectedNodes, unexpectedNode, "选中了明确不该选择的节点") {
						return false
					}
				}
				// 如果所有断言都通过，!t.Failed()会是true，Eventually成功退出
				return !t.Failed()
			}, 45*time.Second, 3*time.Second, "在45秒内未能稳定地选择到正确的节点组合")
		})
	}
}

// findNode 是一个辅助函数，方便通过ID查找节点
func (s *PickerIntegrationSuite) findNode(id string) *MockExecutorNode {
	for _, n := range s.nodes {
		if n.NodeID == id {
			return n
		}
	}
	s.T().Fatalf("测试代码错误：未能找到节点 %s", id)
	return nil
}

// TestPickerIntegrationSuite 运行测试套件的入口
func TestPickerIntegrationSuite(t *testing.T) {
	suite.Run(t, new(PickerIntegrationSuite))
}
