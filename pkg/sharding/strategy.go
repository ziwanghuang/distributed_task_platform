// Package sharding 提供分库分表路由策略和上下文辅助函数。
//
// 本包是调度平台分库分表体系的核心，定义了：
//   - ShardingStrategy: 分片路由策略，根据 ID 计算数据所在的库和表
//   - Dst: 分片目标，包含目标库名、表名及其后缀编号
//   - Context 辅助函数：在 context 中传递分片目标信息
//
// 路由算法：
//
//	dbHash  = id % dbSharding           → 数据库后缀
//	tabHash = (id / dbSharding) % tableSharding → 表后缀
//
// 例如 dbSharding=2, tableSharding=2：
//
//	id=0 → db_0.table_0    id=1 → db_1.table_0
//	id=2 → db_0.table_1    id=3 → db_1.table_1
//
// 本包与 pkg/id_generator 配合使用：
//   - ID 生成时将 shardingID 嵌入高位（id_generator.GenerateID）
//   - 路由时从 ID 提取 shardingID 并计算分片（ShardingStrategy.Shard）
//   - 也可以直接用任务的 ShardingID 字段进行路由
//
// Context 传递机制：
//   - ShardingLoopJob 在启动分片处理时通过 CtxWithDst 将 Dst 写入 context
//   - DAO 层通过 DstFromCtx 从 context 获取 Dst，决定查询哪个库和表
package sharding

import (
	"context"
	"fmt"
)

// ShardingStrategy 分库分表路由策略。
//
// 字段说明：
//   - dbPrefix:      数据库名前缀（如 "task"），实际库名为 task_0, task_1 ...
//   - tablePrefix:   表名前缀（如 "task"），实际表名为 task_0, task_1 ...
//   - tableSharding: 每个库中的表数量
//   - dbSharding:    数据库数量
type ShardingStrategy struct {
	dbPrefix      string
	tablePrefix   string
	tableSharding int64
	dbSharding    int64
}

// NewShardingStrategy 创建分库分表路由策略。
//
// 参数：
//   - dbPrefix:      数据库名前缀
//   - tablePrefix:   表名前缀
//   - tableSharding: 分表数量（每个库中的表数）
//   - dbSharding:    分库数量
func NewShardingStrategy(dbPrefix, tablePrefix string, tableSharding, dbSharding int64) ShardingStrategy {
	return ShardingStrategy{
		dbPrefix:      dbPrefix,
		tablePrefix:   tablePrefix,
		tableSharding: tableSharding,
		dbSharding:    dbSharding,
	}
}

// Dst 描述一个分片目标（具体的库和表）。
//
// 字段说明：
//   - Table:       完整表名（如 "task_0"）
//   - DB:          完整库名（如 "task_0"）
//   - TableSuffix: 表后缀编号（如 0、1）
//   - DBSuffix:    库后缀编号（如 0、1）
type Dst struct {
	Table string
	DB    string

	TableSuffix int64
	DBSuffix    int64
}

// Shard 根据 ID 计算分片目标。
//
// 路由算法：
//   - dbHash  = id % dbSharding           → 确定数据库
//   - tabHash = (id / dbSharding) % tableSharding → 确定表
//
// 这种两级哈希确保数据在所有 db×table 组合中均匀分布。
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

// Broadcast 返回所有分片目标的完整列表（db × table 的笛卡尔积）。
//
// 用于 ShardingLoopJob 遍历所有分片进行补偿处理。
// 返回 dbSharding × tableSharding 个 Dst。
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

// TablePrefix 返回表名前缀。
func (s ShardingStrategy) TablePrefix() string {
	return s.tablePrefix
}

// dstKey 是 context 中存储 Dst 的私有 key 类型（避免与其他包冲突）。
type dstKey struct{}

// DstFromCtx 从 context 中提取分片目标信息。
// 如果 context 中未设置分片信息，返回零值 Dst 和 false。
// DAO 层使用此函数获取当前操作的目标库和表。
func DstFromCtx(ctx context.Context) (Dst, bool) {
	val := ctx.Value(dstKey{})
	res, ok := val.(Dst)
	return res, ok
}

// CtxWithDst 将分片目标信息写入 context。
// ShardingLoopJob 在启动分片处理时调用此函数，
// 使得下游的 DAO 操作能够通过 DstFromCtx 获取正确的分片信息。
func CtxWithDst(ctx context.Context, dst Dst) context.Context {
	return context.WithValue(ctx, dstKey{}, dst)
}
