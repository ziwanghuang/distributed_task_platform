package scheduleparams

import (
	"context"

	"gitee.com/flycash/distributed_task_platform/internal/errs"
)

var _ Builder = &DispatcherBuilder{}

type DispatcherBuilder struct {
	rangeBuilder                *RangeBuilder
	weightedDynamicRangeBuilder *WeightedDynamicRangeBuilder
}

func NewDispatcherBuilder(rangeBuilder *RangeBuilder,
	weightedDynamicRangeBuilder *WeightedDynamicRangeBuilder,
) Builder {
	return &DispatcherBuilder{
		rangeBuilder:                rangeBuilder,
		weightedDynamicRangeBuilder: weightedDynamicRangeBuilder,
	}
}

func (d *DispatcherBuilder) Build(ctx context.Context, info Info) ([]map[string]string, error) {
	switch {
	case info.Rule.Type.IsRange():
		return d.rangeBuilder.Build(ctx, info)
	case info.Rule.Type.IsWeightedDynamicRange():
		return d.weightedDynamicRangeBuilder.Build(ctx, info)
	default:
		return nil, errs.ErrInvalidTaskShardingRule
	}
}
