package ioc

import (
	"gitee.com/flycash/distributed_task_platform/pkg/loadchecker"
	"github.com/gotomicro/ego/core/econf"
	prometheusapi "github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
)

// InitClusterLoadChecker 初始化集群负载检查器
func InitClusterLoadChecker(nodeID string, client prometheusapi.Client) *loadchecker.ClusterLoadChecker {
	var cfg loadchecker.ClusterLoadConfig
	err := econf.UnmarshalKey("loadChecker.cluster", &cfg)
	if err != nil {
		panic(err)
	}
	return loadchecker.NewClusterLoadChecker(nodeID, v1.NewAPI(client), cfg)
}

// InitLoadCompositeChecker 初始化组合负载检查器（限流器和数据库负载检查器）
func InitLoadCompositeChecker(client prometheusapi.Client) *loadchecker.CompositeChecker {
	return loadchecker.NewCompositeChecker(loadchecker.StrategyAND, InitLimiterLoadChecker(), InitDatabaseLoadChecker(client))
}

// InitLimiterLoadChecker 初始化仅使用限流检查器的配置
func InitLimiterLoadChecker() *loadchecker.LimiterChecker {
	var cfg loadchecker.LimiterConfig
	err := econf.UnmarshalKey("loadChecker.limiter", &cfg)
	if err != nil {
		panic(err)
	}
	if cfg.RateLimit <= 0 {
		panic("loadChecker.limiter 配置非法")
	}
	return loadchecker.NewLimiterChecker(cfg)
}

func InitDatabaseLoadChecker(client prometheusapi.Client) *loadchecker.DatabaseLoadChecker {
	var cfg loadchecker.DatabaseLoadConfig
	err := econf.UnmarshalKey("loadChecker.database", &cfg)
	if err != nil {
		panic(err)
	}
	return loadchecker.NewDatabaseLoadChecker(v1.NewAPI(client), cfg)
}
