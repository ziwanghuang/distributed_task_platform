package domain_test

import (
	"testing"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestShardingRule(t *testing.T) {
	t.Parallel()
	sr := domain.ShardingRule{
		Type: "range",
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
