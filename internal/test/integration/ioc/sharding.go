package ioc

import (
	"context"
	"database/sql"
	"time"

	"gitee.com/flycash/distributed_task_platform/internal/repository/dao"
	idPkg "gitee.com/flycash/distributed_task_platform/pkg/id_generator"
	"gitee.com/flycash/distributed_task_platform/pkg/sharding"
	"github.com/ecodeclub/ekit/retry"
	"github.com/ego-component/egorm"
	"github.com/gotomicro/ego/core/econf"
)

func InitIDGenerator() *idPkg.Generator {
	return idPkg.NewGenerator()
}

//nolint:mnd //忽略测试用
func InitShardingTaskDAO(dbs map[string]*egorm.Component, idgen *idPkg.Generator) dao.ShardingTaskDAO {
	str := sharding.NewShardingStrategy("task", "task", 2, 2)
	return dao.NewShardingTaskDAO(dbs, idgen, str)
}

//nolint:mnd //忽略测试用
func InitShardingTaskExecutionDAO(dbs map[string]*egorm.Component, idgen *idPkg.Generator) dao.ShardingTaskExecutionDAO {
	str := sharding.NewShardingStrategy("task", "task_execution", 2, 4)
	return dao.NewShardingTaskExecution(dbs, idgen, str)
}

func InitDBs() map[string]*egorm.Component {
	task0dsn := "root:root@tcp(localhost:13316)/task_0?collation=utf8mb4_general_ci&parseTime=True&loc=Local&timeout=1s&readTimeout=3s&writeTimeout=3s&multiStatements=true&interpolateParams=true&charset=utf8mb4"
	task1dsn := "root:root@tcp(localhost:13316)/task_1?collation=utf8mb4_general_ci&parseTime=True&loc=Local&timeout=1s&readTimeout=3s&writeTimeout=3s&multiStatements=true&interpolateParams=true&charset=utf8mb4"

	econf.Set("mysql1", map[string]any{
		"dsn":   task0dsn,
		"debug": true,
	})
	WaitForDBSetup(task0dsn)
	econf.Set("mysql2", map[string]any{
		"dsn":   task1dsn,
		"debug": true,
	})
	WaitForDBSetup(task1dsn)
	task0db := egorm.Load("mysql1").Build()
	task1db := egorm.Load("mysql2").Build()
	return map[string]*egorm.Component{
		"task_0": task0db,
		"task_1": task1db,
	}
}

func WaitForDBSetup(dsn string) {
	sqlDB, err := sql.Open("mysql", dsn)
	if err != nil {
		panic(err)
	}
	if err1 := ping(sqlDB); err1 != nil {
		panic(err1)
	}
}

func ping(sqlDB *sql.DB) error {
	const maxInterval = 10 * time.Second
	const maxRetries = 10
	strategy, err := retry.NewExponentialBackoffRetryStrategy(time.Second, maxInterval, maxRetries)
	if err != nil {
		return err
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
			panic("Ping DB 重试失败......")
		}
		time.Sleep(next)
	}
	return nil
}
