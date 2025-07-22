package ioc

import (
	"gitee.com/flycash/distributed_task_platform/internal/service/picker"
	"github.com/gotomicro/ego/core/econf"
	prometheusapi "github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
)

// InitExecutorNodePicker 初始化执行节点选择器
func InitExecutorNodePicker(promClient prometheusapi.Client) picker.ExecutorNodePicker {
	// 从配置文件读取智能调度配置
	var config picker.Config
	err := econf.UnmarshalKey("intelligentScheduling", &config)
	if err != nil {
		panic(err)
	}
	// 创建 Prometheus v1 API 客户端
	v1API := v1.NewAPI(promClient)

	// 创建并返回选择器分发器
	return picker.NewDispatcher(v1API, config)
}
