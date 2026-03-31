// Package id 提供分布式唯一 ID 生成器，基于 Snowflake 算法的变种实现。
//
// ID 位布局（共 65 位，使用 int64 存储，最高位为符号位）：
//
//	| bizID (12bit) | sequence (12bit) | timestamp (41bit) |
//	| 高位           | 中间              | 低位               |
//
// 与标准 Snowflake 的区别：
//   - 标准 Snowflake 使用 workerID 标识机器，本实现使用 bizID（shardingID）标识分库分表位置
//   - bizID 嵌入 ID 中，使得从 ID 可以直接提取出数据所在的分片位置，无需额外查询路由表
//   - 这是分库分表场景下的关键设计：ID 自带路由信息
//
// 时间范围：基准时间 2024-01-01，41 位时间戳可用约 69 年
// 序列号：12 位支持每毫秒 4096 个 ID（注意：当前实现使用全局递增序列号，不按毫秒重置）
// bizID：12 位支持 0~4095 个分片
package id

import (
	"sync/atomic"
	"time"
)

const (
	// 位数分配常量
	timestampBits = 41 // 时间戳位数，约 69 年
	bizIDBits     = 12 // 业务ID（分片ID）位数，支持 0~4095
	sequenceBits  = 12 // 序列号位数，支持每毫秒 4096 个 ID

	// 位移常量 —— 决定各部分在 int64 中的位置
	timestampShift = 0                          // 时间戳在最低位
	sequenceShift  = timestampBits              // 序列号紧接时间戳之上
	bizIDShift     = sequenceBits + timestampBits // bizID 在最高位

	// 掩码常量 —— 用于提取各部分的值
	sequenceMask   = (1 << sequenceBits) - 1   // 0xFFF，12位全1
	shardingIDMask = (1 << bizIDBits) - 1      // 0xFFF，12位全1
	timestampMask  = (1 << timestampBits) - 1  // 41位全1

	// 基准时间（epoch） - 2024年1月1日 00:00:00 UTC
	// 所有时间戳都是相对于此基准时间的毫秒偏移量
	epochMillis   = int64(1704067200000) // 2024-01-01 00:00:00 UTC in milliseconds
	number        = int64(1024)
	number1000    = int64(1000)
	number1000000 = int64(1000000)
)

// Generator 是 Snowflake 变种 ID 生成器。
//
// 核心字段：
//   - sequence: 全局递增序列号，使用 atomic 保证并发安全。
//     注意：与标准 Snowflake 不同，序列号不按毫秒重置，而是持续递增后取模（通过掩码截断）。
//   - lastTime: 上次生成 ID 的时间戳（当前实现中未使用时钟回拨检测）
//   - epoch:    基准时间点，用于计算时间戳偏移量
type Generator struct {
	sequence int64     // 序列号计数器，使用原子操作访问
	lastTime int64     // 上次生成ID的时间戳
	epoch    time.Time // 基准时间点
}

// NewGenerator 创建一个新的 ID 生成器实例。
func NewGenerator() *Generator {
	return &Generator{
		sequence: 0,
		lastTime: 0,
		epoch:    time.Unix(epochMillis/number1000, (epochMillis%number1000)*number1000000),
	}
}

// GenerateID 根据 shardingID 生成全局唯一 ID。
//
// 参数：
//   - shardingID: 分片标识，占 12 位（0~4095），通常由任务的分片策略决定。
//     该值会被嵌入到 ID 的高位，使得从 ID 可以直接推导出数据所在的分库分表位置。
//
// 生成流程：
//  1. 获取当前时间相对于 epoch 的毫秒偏移量
//  2. 将 shardingID 截断为 12 位
//  3. 原子递增序列号并截断为 12 位
//  4. 按位布局组装：bizID(高12) | sequence(中12) | timestamp(低41)
func (g *Generator) GenerateID(shardingID int64) int64 {
	timestamp := time.Now().UnixMilli() - epochMillis
	// 确保bizID在12位范围内
	shardingValue := shardingID & shardingIDMask
	// 使用原子操作安全地递增序列号
	sequence := atomic.AddInt64(&g.sequence, 1) - 1 // 减1是因为AddInt64返回递增后的值

	// 组装最终ID：高12位=bizID，中12位=sequence，低41位=timestamp
	id := (shardingValue)<<bizIDShift | // 业务ID部分
		(sequence&sequenceMask)<<sequenceShift | // 序列号部分
		(timestamp & timestampMask) // 时间戳部分
	return id
}

// ExtractTimestamp 从 ID 中提取生成时间。
// 取出低 41 位时间戳偏移量，加上 epoch 基准时间得到绝对时间。
func ExtractTimestamp(id int64) time.Time {
	timestamp := id & timestampMask
	return time.Unix(0, (timestamp+epochMillis)*int64(time.Millisecond))
}

// ExtractShardingID 从 ID 中提取分片标识。
// 取出高 12 位 bizID，可用于确定该 ID 对应的数据所在的分库分表位置。
func ExtractShardingID(id int64) int64 {
	return (id >> bizIDShift) & shardingIDMask
}

// ExtractSequence 从 ID 中提取序列号部分。
func ExtractSequence(id int64) int64 {
	return (id >> sequenceShift) & sequenceMask
}
