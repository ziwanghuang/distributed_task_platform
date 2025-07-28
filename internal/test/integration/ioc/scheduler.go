package ioc

import (
	"context"
	"time"

	executorv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/executor/v1"
	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/service/acquirer"
	"gitee.com/flycash/distributed_task_platform/internal/service/picker"
	"gitee.com/flycash/distributed_task_platform/internal/service/runner"
	"gitee.com/flycash/distributed_task_platform/internal/service/scheduler"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	grpcpkg "gitee.com/flycash/distributed_task_platform/pkg/grpc"
	registry "gitee.com/flycash/distributed_task_platform/pkg/grpc/registry/etcd"
	"gitee.com/flycash/distributed_task_platform/pkg/loadchecker"
	"gitee.com/flycash/distributed_task_platform/pkg/prometheus"
	"github.com/pborman/uuid"
	"google.golang.org/grpc"
)

func InitNodeID() string {
	return uuid.New()
}

func InitScheduler(
	nodeID string,
	execRunner runner.Runner,
	taskSvc task.Service,
	execSvc task.ExecutionService,
	acquirer acquirer.TaskAcquirer,
	registry *registry.Registry,
	lc *loadchecker.ClusterLoadChecker,
) *scheduler.Scheduler {
	const batchTimeout = 30 * time.Second
	const batchSize = 10
	const preemptedTimeout = 10 * time.Second
	const scheduleInterval = 10 * time.Second
	const renewInterval = 3 * time.Second
	conf := scheduler.Config{
		BatchTimeout:     batchTimeout,
		BatchSize:        batchSize,
		PreemptedTimeout: preemptedTimeout,
		ScheduleInterval: scheduleInterval,
		RenewInterval:    renewInterval,
	}
	grpcClients := grpcpkg.NewClientsV2(registry, time.Second, func(conn *grpc.ClientConn) executorv1.ExecutorServiceClient {
		return executorv1.NewExecutorServiceClient(conn)
	})
	// 创建测试用的 mock picker
	mockPicker := &mockPicker{}

	return scheduler.NewScheduler(
		nodeID,
		execRunner,
		taskSvc,
		execSvc,
		acquirer,
		grpcClients,
		conf,
		lc,
		prometheus.NewSchedulerMetrics(nodeID),
		mockPicker,
	)
}

// mockPicker 测试用的简单 picker 实现，总是返回空（随机选择）
type mockPicker struct{}

// 确保 mockPicker 实现了 picker.ExecutorNodePicker 接口
var _ picker.ExecutorNodePicker = &mockPicker{}

func (m *mockPicker) Pick(_ context.Context, _ domain.Task) (string, error) {
	// 返回空字符串，让调度器使用默认的随机选择
	return "", nil
}

func (m *mockPicker) Name() string {
	return "MOCK_PICKER"
}
