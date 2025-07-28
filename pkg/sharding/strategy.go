package sharding

import (
	"context"
	"fmt"
)

type ShardingStrategy struct {
	dbPrefix      string
	tablePrefix   string
	tableSharding int64
	dbSharding    int64
}

func NewShardingStrategy(dbPrefix, tablePrefix string, tableSharding, dbSharding int64) ShardingStrategy {
	return ShardingStrategy{
		dbPrefix:      dbPrefix,
		tablePrefix:   tablePrefix,
		tableSharding: tableSharding,
		dbSharding:    dbSharding,
	}
}

type Dst struct {
	Table string
	DB    string

	TableSuffix int64
	DBSuffix    int64
}

func (s ShardingStrategy) Shard(id int64) Dst {
	dbHash := id % s.dbSharding
	tabHash := (id / s.dbSharding) % s.tableSharding
	return Dst{
		TableSuffix: tabHash,
		Table:       fmt.Sprintf("%s_%d", s.tablePrefix, tabHash),
		DBSuffix:    dbHash,
		DB:          fmt.Sprintf("%s_%d", s.dbPrefix, dbHash),
	}
}

func (s ShardingStrategy) Broadcast() []Dst {
	ans := make([]Dst, 0, s.tableSharding*s.dbSharding)
	for i := 0; i < int(s.dbSharding); i++ {
		for j := 0; j < int(s.tableSharding); j++ {
			ans = append(ans, Dst{
				TableSuffix: int64(j),
				Table:       fmt.Sprintf("%s_%d", s.tablePrefix, j),
				DB:          fmt.Sprintf("%s_%d", s.dbPrefix, i),
				DBSuffix:    int64(i),
			})
		}
	}
	return ans
}

func (s ShardingStrategy) TablePrefix() string {
	return s.tablePrefix
}

type dstKey struct{}

func DstFromCtx(ctx context.Context) (Dst, bool) {
	val := ctx.Value(dstKey{})
	res, ok := val.(Dst)
	return res, ok
}

func CtxWithDst(ctx context.Context, dst Dst) context.Context {
	return context.WithValue(ctx, dstKey{}, dst)
}
