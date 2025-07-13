package ioc

import (
	"github.com/redis/go-redis/v9"
)


var rdb redis.Cmdable

func InitRedis() redis.Cmdable {
	if rdb != nil {
		return rdb
	}
	return InitRedisClient()
}

func InitRedisClient() *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
}
