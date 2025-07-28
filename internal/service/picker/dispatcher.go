package picker

import (
	"context"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"github.com/gotomicro/ego/core/elog"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
)

// 确保 Dispatcher 实现了 ExecutorNodePicker 接口。
var _ ExecutorNodePicker = &Dispatcher{}

// Dispatcher 是一个高级选择器，它作为所有策略的统一入口 (门面/策略模式)。
// 它内部根据配置决定是使用CPU优先策略还是内存优先策略。
type Dispatcher struct {
	basePicker *prometheusPicker
	config     Config
	logger     *elog.Component
}

// NewDispatcher 是创建Dispatcher的唯一入口。
func NewDispatcher(promClient v1.API, config Config) *Dispatcher {
	config.validateAndSetDefaults()
	return &Dispatcher{
		basePicker: newPrometheusPicker(promClient, config),
		config:     config,
		logger:     elog.DefaultLogger.With(elog.FieldComponentName("picker.Dispatcher")),
	}
}

// Name 返回分发器的名称。
func (d *Dispatcher) Name() string {
	return "DISPATCHER"
}

// Pick 根据配置的策略，调用底层的prometheusPicker来选择最优执行节点。
func (d *Dispatcher) Pick(ctx context.Context, task domain.Task) (string, error) {
	d.logger.Info("开始智能调度选择节点")
	var metricName string
	switch {
	case task.SchedulingStrategy.IsCPUPriority():
		d.logger.Info("使用CPU优先策略")
		metricName = MetricCPUIdlePercent
	case task.SchedulingStrategy.IsMemoryPriority():
		d.logger.Info("使用内存优先策略")
		metricName = MetricMemoryAvailableBytes
	default:
		// 如果策略未配置或配置错误，提供一个安全的回退（fallback）机制。
		d.logger.Warn("不支持的调度策略，将默认使用CPU优先",
			elog.String("unsupportedStrategy", task.SchedulingStrategy.String()))
		metricName = MetricCPUIdlePercent
	}
	return d.basePicker.pickByMetric(ctx, metricName)
}
