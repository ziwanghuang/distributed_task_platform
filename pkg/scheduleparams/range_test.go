//go:build unit

package scheduleparams_test

import (
	"testing"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/pkg/scheduleparams"
	"github.com/stretchr/testify/assert"
)

func TestRangeBuilder(t *testing.T) {
	t.Parallel()

	builder := scheduleparams.NewDispatcherBuilder(
		scheduleparams.NewRangeBuilder(), nil)

	actual, err := builder.Build(t.Context(), scheduleparams.Info{
		Rule: domain.ShardingRule{
			Type: "range",
			Params: map[string]string{
				"totalNums": "3",
				"step":      "100000",
			},
		},
	})
	assert.NoError(t, err)
	assert.Equal(t, []map[string]string{
		{"start": "0", "end": "100000"},
		{"start": "100000", "end": "200000"},
		{"start": "200000", "end": "300000"},
	}, actual)
}
