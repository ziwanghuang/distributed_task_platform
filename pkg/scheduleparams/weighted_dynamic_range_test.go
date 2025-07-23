package scheduleparams_test

import (
	"context"
	"strconv"
	"testing"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/pkg/grpc/registry"
	"gitee.com/flycash/distributed_task_platform/pkg/scheduleparams"
	"github.com/stretchr/testify/assert"
)

func TestWeightedDynamicRangeBuilder(t *testing.T) {
	t.Parallel()

	builder := scheduleparams.NewDispatcherBuilder(
		nil, scheduleparams.NewWeightedDynamicRangeBuilder(&MockInvoker{totalTasks: 1000}))

	actual, err := builder.Build(t.Context(), scheduleparams.Info{
		Rule: domain.ShardingRule{
			Type: domain.ShardingRuleTypeWeightedDynamicRange,
		},
		ExecutorNodeInstances: []registry.ServiceInstance{
			{
				ID:     "A",
				Weight: 20,
			},
			{
				ID:     "B",
				Weight: 30,
			},
			{
				ID:     "C",
				Weight: 50,
			},
		},
	})
	assert.NoError(t, err)
	assert.Equal(t, []map[string]string{
		{"start": "0", "end": "200"},
		{"start": "200", "end": "500"},
		{"start": "500", "end": "1000"},
	}, actual)
}

type MockInvoker struct {
	totalTasks int64
}

func (m *MockInvoker) Name() string {
	return "mockInvoker"
}

func (m *MockInvoker) Run(_ context.Context, _ domain.TaskExecution) (domain.ExecutionState, error) {
	// TODO implement me
	panic("implement me")
}

func (m *MockInvoker) Prepare(_ context.Context, _ domain.TaskExecution) (map[string]string, error) {
	return map[string]string{
		"total_tasks": strconv.FormatInt(m.totalTasks, 10),
	}, nil
}
