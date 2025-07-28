package id

import (
	"sync/atomic"
	"time"
)

const (
	// 位数分配常量
	timestampBits = 41 // 时间戳位数
	bizIdBits     = 12 // 业务ID位数
	sequenceBits  = 12 // 序列号位数

	// 位移常量
	timestampShift = 0
	sequenceShift  = timestampBits
	bizIdShift     = sequenceBits + timestampBits

	// 掩码常量
	sequenceMask   = (1 << sequenceBits) - 1
	shardingIdMask = (1 << bizIdBits) - 1
	timestampMask  = (1 << timestampBits) - 1

	// 基准时间 - 2024年1月1日，可以根据实际需求调整
	epochMillis   = int64(1704067200000) // 2024-01-01 00:00:00 UTC in milliseconds
	number        = int64(1024)
	number1000    = int64(1000)
	number1000000 = int64(1000000)
)

// Generator 是ID生成器结构
type Generator struct {
	sequence int64     // 序列号计数器，使用原子操作访问
	lastTime int64     // 上次生成ID的时间戳
	epoch    time.Time // 基准时间点
}

// NewGenerator 创建一个新的ID生成器
func NewGenerator() *Generator {
	return &Generator{
		sequence: 0,
		lastTime: 0,
		epoch:    time.Unix(epochMillis/number1000, (epochMillis%number1000)*number1000000),
	}
}

// GenerateID 根据雪花算法变种生成ID
// bizID: 业务ID，占12位(0-4095)
func (g *Generator) GenerateID(shardingID int64) int64 {
	timestamp := time.Now().UnixMilli() - epochMillis
	// 确保bizID在10位范围内
	shardingValue := shardingID & shardingIdMask
	// 使用原子操作安全地递增序列号
	sequence := atomic.AddInt64(&g.sequence, 1) - 1 // 减1是因为AddInt64返回递增后的值

	// 组装最终ID
	id := (shardingValue)<<bizIdShift | // 业务ID部分
		(sequence&sequenceMask)<<sequenceShift | // 序列号部分
		(timestamp & timestampMask) // 时间戳部分
	return id
}

// ExtractTimestamp 从ID中提取时间戳
func ExtractTimestamp(id int64) time.Time {
	timestamp := id & timestampMask
	return time.Unix(0, (timestamp+epochMillis)*int64(time.Millisecond))
}

// ExtractShardingID 从ID中提取业务ID
func ExtractShardingID(id int64) int64 {
	return (id >> bizIdShift) & shardingIdMask
}

// ExtractSequence 从ID中提取序列号部分
func ExtractSequence(id int64) int64 {
	return (id >> sequenceShift) & sequenceMask
}
