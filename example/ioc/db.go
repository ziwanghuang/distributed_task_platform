package ioc

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"github.com/ecodeclub/ekit/retry"
	"github.com/ego-component/egorm"
	"github.com/gotomicro/ego/core/econf"
)

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

var (
	db         *egorm.Component
	initDBOnce sync.Once
)

func InitDB() *egorm.Component {
	initDBOnce.Do(func() {
		if db != nil {
			return
		}
		econf.Set("mysql", map[string]any{
			// interpolateParams=true 会导致JSON字段发送出问题
			"dsn":   "root:root@tcp(localhost:13316)/task?charset=utf8mb4&collation=utf8mb4_0900_ai_ci&parseTime=True&loc=Local&timeout=1s&readTimeout=3s&writeTimeout=3s&multiStatements=true&interpolateParams=true",
			"debug": true,
		})
		WaitForDBSetup(econf.GetStringMapString("mysql")["dsn"])
		db = egorm.Load("mysql").Build()

	})

	return db
}

func InitDBWithCustomConnPool(cp gorm.ConnPool) *gorm.DB {
	gdb, err := gorm.Open(mysql.New(mysql.Config{
		Conn: cp,
	}), &gorm.Config{
		ConnPool: cp,
	})
	if err != nil {
		panic(err)
	}
	return gdb
}
