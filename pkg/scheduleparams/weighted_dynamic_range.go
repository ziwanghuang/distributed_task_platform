package scheduleparams

import (
	"context"
	"fmt"
	"strconv"

	"gitee.com/flycash/distributed_task_platform/internal/service/invoker"
)

var _ Builder = &WeightedDynamicRangeBuilder{}

type WeightedDynamicRangeBuilder struct {
	invoker invoker.Invoker
}

func NewWeightedDynamicRangeBuilder(invoker invoker.Invoker) *WeightedDynamicRangeBuilder {
	return &WeightedDynamicRangeBuilder{
		invoker: invoker,
	}
}

func (w *WeightedDynamicRangeBuilder) Build(ctx context.Context, info Info) ([]map[string]string, error) {
	// 调用任一节点的 Prepare 接口，获取任务总数
	params, err := w.invoker.Prepare(ctx, info.TaskExecution)
	if err != nil {
		return nil, fmt.Errorf("执行Prepare GRPC失败: %w", err)
	}
	totalTasks, err := strconv.ParseInt(params["total_tasks"], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("解析业务方总任务数失败: %w", err)
	}

	// 计算总权重
	var totalWeight int64
	for i := range info.ExecutorNodeInstances {
		totalWeight += info.ExecutorNodeInstances[i].Weight
	}
	if totalWeight == 0 {
		return nil, fmt.Errorf("所有执行节点的总权重为0，无法进行分片")
	}

	// 计算每个执行节点对应的分片区间
	scheduleParams := make([]map[string]string, 0, len(info.ExecutorNodeInstances))

	var start, step, end int64
	// 遍历 N-1 个节点，计算它们的分片
	for i := 0; i < len(info.ExecutorNodeInstances)-1; i++ {
		// 根据权重计算当前节点应处理的任务数
		// 使用浮点数保证精度，然后转换为整数
		share := float64(totalTasks) * (float64(info.ExecutorNodeInstances[i].Weight) / float64(totalWeight))
		step = int64(share)
		end = start + step

		scheduleParams = append(scheduleParams, map[string]string{
			"start": strconv.FormatInt(start, 10),
			"end":   strconv.FormatInt(end, 10),
		})
		start = end // 下一个分片的起点是当前分片的终点
	}

	// 最后一个节点获得所有剩余的任务，以避免舍入误差导致任务丢失
	scheduleParams = append(scheduleParams, map[string]string{
		"start": strconv.FormatInt(start, 10),
		"end":   strconv.FormatInt(totalTasks, 10), // 终点即为任务总数
	})
	return scheduleParams, nil
}
