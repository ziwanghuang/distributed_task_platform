package ioc

import (
	"time"

	"gitee.com/flycash/distributed_task_platform/pkg/loadchecker"
	"github.com/gotomicro/ego/core/econf"
	prometheusapi "github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
)

// InitClusterLoadChecker 初始化集群负载检查器
//
//nolint:mnd //忽略
func InitClusterLoadChecker(nodeID string, client prometheusapi.Client) *loadchecker.ClusterLoadChecker {
	cfg := loadchecker.ClusterLoadConfig{
		ThresholdRatio:     1.2,
		TimeWindow:         5 * time.Minute,
		SlowdownMultiplier: 2.0,
		MinBackoffDuration: 5 * time.Second,
	}

	// 尝试从配置文件读取，如果失败则使用默认配置
	var configFromFile loadchecker.ClusterLoadConfig
	err := econf.UnmarshalKey("loadChecker.cluster", &configFromFile)
	if err == nil {
		// 配置文件存在，使用配置文件的值
		cfg = configFromFile
	}

	return loadchecker.NewClusterLoadChecker(nodeID, v1.NewAPI(client), cfg)
}

// InitLoadCompositeChecker 初始化组合负载检查器（限流器和数据库负载检查器）
func InitLoadCompositeChecker(client prometheusapi.Client) *loadchecker.CompositeChecker {
	return loadchecker.NewCompositeChecker(loadchecker.StrategyAND, InitLimiterLoadChecker(), InitDatabaseLoadChecker(client))
}

// InitLimiterLoadChecker 初始化仅使用限流检查器的配置（测试环境使用默认配置）
//
//nolint:mnd //忽略
func InitLimiterLoadChecker() *loadchecker.LimiterChecker {
	// 测试环境使用默认配置
	cfg := loadchecker.LimiterConfig{
		RateLimit:    10.0,            // 每秒允许10次调度
		BurstSize:    20,              // 突发容量20
		WaitDuration: 5 * time.Second, // 限流时等待5秒
	}

	// 尝试从配置文件读取，如果失败则使用默认配置
	var configFromFile loadchecker.LimiterConfig
	err := econf.UnmarshalKey("loadChecker.limiter", &configFromFile)
	if err == nil && configFromFile.RateLimit > 0 {
		// 配置文件存在且有效，使用配置文件的值
		cfg = configFromFile
	}

	return loadchecker.NewLimiterChecker(cfg)
}

//
//nolint:mnd //忽略
func InitDatabaseLoadChecker(client prometheusapi.Client) *loadchecker.DatabaseLoadChecker {
	// 测试环境使用默认配置
	cfg := loadchecker.DatabaseLoadConfig{
		Threshold:       100 * time.Millisecond, // 数据库响应时间阈值100ms
		TimeWindow:      5 * time.Minute,        // 查询时间窗口5分钟
		BackoffDuration: 10 * time.Second,       // 负载过高时退避10秒
	}

	// 尝试从配置文件读取，如果失败则使用默认配置
	var configFromFile loadchecker.DatabaseLoadConfig
	err := econf.UnmarshalKey("loadChecker.database", &configFromFile)
	if err == nil {
		// 配置文件存在，使用配置文件的值
		cfg = configFromFile
	}

	return loadchecker.NewDatabaseLoadChecker(v1.NewAPI(client), cfg)
}
