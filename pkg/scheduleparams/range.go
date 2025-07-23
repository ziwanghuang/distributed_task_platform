package scheduleparams

import (
	"context"
	"strconv"
)

var _ Builder = &RangeBuilder{}

type RangeBuilder struct{}

func NewRangeBuilder() *RangeBuilder {
	return &RangeBuilder{}
}

func (r *RangeBuilder) Build(_ context.Context, info Info) ([]map[string]string, error) {
	scheduleParams := make([]map[string]string, 0)
	step, _ := strconv.ParseInt(info.Rule.Params["step"], 10, 64)
	totalNums, _ := strconv.ParseInt(info.Rule.Params["totalNums"], 10, 64)
	for i := range totalNums {
		mp := make(map[string]string)
		mp["start"] = strconv.FormatInt(i*step, 10)
		mp["end"] = strconv.FormatInt((i+1)*step, 10)
		scheduleParams = append(scheduleParams, mp)
	}
	return scheduleParams, nil
}
