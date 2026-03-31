package ioc

import (
	"gitee.com/flycash/distributed_task_platform/internal/compensator"
	"gitee.com/flycash/distributed_task_platform/internal/event/reportevt"
)

// InitTasks 组装所有需要在后台持续运行的长任务列表。
// 这些任务通过 SchedulerApp.StartTasks 在独立 goroutine 中启动，
// 运行直到上下文取消（服务优雅停机）。
// 包含以下 6 个长任务：
//   - t1: 重试补偿器 - 扫描可重试的失败执行，重新调度
//   - t2: 重调度补偿器 - 处理执行节点请求的重调度
//   - t3: 分片补偿器 - 汇总分片任务子分片执行结果
//   - t4: 中断补偿器 - 检测超时执行并通知执行节点停止
//   - t5: 批量上报事件消费者 - 消费 Kafka 批量状态上报消息
//   - t6: 单条上报事件消费者 - 消费 Kafka 单条状态上报消息
func InitTasks(
	t1 *compensator.RetryCompensator,
	t2 *compensator.RescheduleCompensator,
	t3 *compensator.ShardingCompensator,
	t4 *compensator.InterruptCompensator,
	t5 *reportevt.BatchReportEventConsumer,
	t6 *reportevt.ReportEventConsumer,
) []Task {
	return []Task{
		t1,
		t2,
		t3,
		t4,
		t5,
		t6,
	}
}
