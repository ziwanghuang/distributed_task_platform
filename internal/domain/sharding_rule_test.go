//go:build unit

package domain_test

import (
	"testing"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/pkg/grpc/registry"
	"github.com/stretchr/testify/assert"
)

func TestRangeShardingRule(t *testing.T) {
	t.Parallel()
	sr := domain.ShardingRule{
		Type: domain.ShardingRuleTypeRange,
		Params: map[string]string{
			"totalNums": "3",
			"step":      "100000",
		},
	}
	assert.Equal(t, []map[string]string{
		{"start": "0", "end": "100000"},
		{"start": "100000", "end": "200000"},
		{"start": "200000", "end": "300000"},
	}, sr.ToScheduleParams())
}

func TestWeightedDynamicRange(t *testing.T) {
	t.Parallel()

	sr := domain.ShardingRule{
		Type: domain.ShardingRuleTypeWeightedDynamicRange,
		Params: map[string]string{
			"total_tasks": "1000",
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
	}
	assert.Equal(t, []map[string]string{
		{"start": "0", "end": "200"},
		{"start": "200", "end": "500"},
		{"start": "500", "end": "1000"},
	}, sr.ToScheduleParams())

	sr = domain.ShardingRule{
		Type: domain.ShardingRuleTypeWeightedDynamicRange,
		Params: map[string]string{
			"total_tasks": "1000",
		},
		ExecutorNodeInstances: []registry.ServiceInstance{
			{
				ID:     "A",
				Weight: 20,
			},
		},
	}
	assert.Equal(t, []map[string]string{
		{"start": "0", "end": "1000"},
	}, sr.ToScheduleParams())
}
