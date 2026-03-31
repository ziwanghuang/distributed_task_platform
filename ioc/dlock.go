package ioc

import (
	"github.com/meoying/dlock-go"
	dlockRedis "github.com/meoying/dlock-go/redis"
	"github.com/redis/go-redis/v9"
)

// InitDistributedLock 初始化基于 Redis 的分布式锁客户端。
// 分布式锁在以下场景中使用：
//   - V2 版本补偿器中防止多个调度节点同时处理同一批补偿任务
//   - 任务抢占时的并发控制
//
// 底层使用 meoying/dlock-go 库，基于 Redis SET NX + Lua 脚本实现。
func InitDistributedLock(rdb *redis.Client) dlock.Client {
	return dlockRedis.NewClient(rdb)
}
