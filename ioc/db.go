package ioc

import (
	"context"
	"database/sql"
	"time"

	"github.com/gotomicro/ego/core/econf"

	"gitee.com/flycash/distributed_task_platform/internal/repository/dao"
	"gitee.com/flycash/distributed_task_platform/pkg/prometheus"

	"github.com/ecodeclub/ekit/retry"
	"github.com/ego-component/egorm"
)

// InitDB 初始化 MySQL 数据库连接。
// 流程：
//  1. 等待数据库就绪（指数退避重试 Ping）
//  2. 通过 ego 的 egorm 组件加载 MySQL 配置并建立连接
//  3. 注册 Prometheus GORM 插件，采集慢查询和错误指标
//  4. 自动迁移表结构（task、task_execution 等表）
func InitDB() *egorm.Component {
	WaitForDBSetup(econf.GetString("mysql.dsn"))
	db := egorm.Load("mysql").Build()

	// 注册 Prometheus 指标插件，用于采集数据库操作耗时和错误率
	plugin := prometheus.NewGormMetricsPlugin()
	if err := db.Use(plugin); err != nil {
		panic(err)
	}

	err := dao.InitTables(db)
	if err != nil {
		panic(err)
	}
	return db
}

// WaitForDBSetup 等待 MySQL 数据库就绪。
// 使用指数退避重试策略（初始 1s，最大 10s，最多 10 次），
// 每次尝试通过 PingContext 检测连接是否可用。
// 适用于 Docker 环境下 MySQL 容器启动较慢的场景。
func WaitForDBSetup(dsn string) {
	sqlDB, err := sql.Open("mysql", dsn)
	if err != nil {
		panic(err)
	}
	const maxInterval = 10 * time.Second
	const maxRetries = 10
	strategy, err := retry.NewExponentialBackoffRetryStrategy(time.Second, maxInterval, maxRetries)
	if err != nil {
		panic(err)
	}

	const timeout = 5 * time.Second
	for {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		err = sqlDB.PingContext(ctx)
		cancel()
		if err == nil {
			break
		}
		next, ok := strategy.Next()
		if !ok {
			panic("WaitForDBSetup 重试失败......")
		}
		time.Sleep(next)
	}
}
