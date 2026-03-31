package ioc

import (
	"github.com/gotomicro/ego/core/econf"
	prometheusapi "github.com/prometheus/client_golang/api"
)

// InitPrometheusClient 初始化 Prometheus HTTP API 客户端。
// 从 config.yaml 的 prometheus.url 读取 Prometheus 服务地址，
// 该客户端用于：
//   - 智能调度：查询执行节点的 CPU/内存指标，选择最优节点
//   - 集群负载检查：查询当前节点的调度速率是否超过集群平均值
//   - 数据库负载检查：查询 MySQL 响应时间是否超过阈值
func InitPrometheusClient() prometheusapi.Client {
	type Config struct {
		URL string `yaml:"url"`
	}
	var cfg Config
	err := econf.UnmarshalKey("prometheus", &cfg)
	if err != nil {
		panic(err)
	}

	client, err := prometheusapi.NewClient(prometheusapi.Config{
		Address: cfg.URL,
	})
	if err != nil {
		panic(err)
	}
	return client
}
