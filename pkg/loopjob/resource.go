// Package loopjob 提供分片循环任务框架（ShardingLoopJob）和资源控制能力。
//
// 本包是调度平台 V2 补偿器体系的底层执行框架，核心解决两个问题：
//  1. 分片并发控制：通过 ResourceSemaphore 限制单个节点同时处理的分片数量，防止过载
//  2. 分布式锁 + 业务循环：为每个分片（db+table）获取分布式锁后，在锁保护下持续执行业务逻辑
//
// 使用方式：
//   - 创建 ShardingLoopJob 实例，传入分片策略、分布式锁客户端、业务函数和信号量
//   - 调用 Run(ctx) 进入无限循环：遍历所有分片 → 获取信号量 → 竞争分布式锁 → 启动业务循环
//   - 通过 cancel ctx 退出
//
// 与 V1 补偿器的区别：
//   - V1（如 retry.go）直接查询全量数据，无分片概念
//   - V2（如 retryv2.go）通过 ShardingLoopJob 框架，每个分片独立加锁独立处理
package loopjob

import (
	"context"
	"sync"

	"gitee.com/flycash/distributed_task_platform/internal/errs"
)

// ResourceSemaphore 资源信号量接口，控制并发资源的获取和释放。
//
// 在 ShardingLoopJob 中的作用：
//   - 每个节点同时只能处理有限数量的分片（如 3 个）
//   - 获取信号量成功才能尝试竞争分布式锁
//   - 分片处理完毕后释放信号量，让其他分片有机会被处理
type ResourceSemaphore interface {
	// Acquire 获取一个资源许可。如果已达上限，返回 errs.ErrExceedLimit。
	Acquire(ctx context.Context) error
	// Release 释放一个资源许可。
	Release(ctx context.Context) error
}

// MaxCntResourceSemaphore 基于最大计数的资源信号量实现。
//
// 通过 mutex 保护的 curCount/maxCount 实现简单的计数信号量。
// 相比 channel-based 信号量，优点是支持动态调整 maxCount（UpdateMaxCount）。
//
// 字段说明：
//   - maxCount: 允许同时持有的最大许可数
//   - curCount: 当前已发出的许可数
//   - mu:       读写锁，保护 curCount 和 maxCount 的并发访问
type MaxCntResourceSemaphore struct {
	maxCount int
	curCount int
	mu       *sync.RWMutex
}

// Acquire 尝试获取一个资源许可。
// 如果当前已发出的许可数 >= 最大许可数，返回 errs.ErrExceedLimit。
// 否则将 curCount +1 并返回 nil。
func (r *MaxCntResourceSemaphore) Acquire(context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.curCount >= r.maxCount {
		return errs.ErrExceedLimit
	}
	r.curCount++
	return nil
}

// Release 释放一个资源许可，将 curCount -1。
func (r *MaxCntResourceSemaphore) Release(context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.curCount--
	return nil
}

// UpdateMaxCount 动态更新最大许可数。
// 可在运行时根据系统负载动态调整节点能处理的分片并发数。
func (r *MaxCntResourceSemaphore) UpdateMaxCount(maxCount int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.maxCount = maxCount
}

// NewResourceSemaphore 创建一个基于最大计数的资源信号量。
// maxCount 指定允许同时持有的最大许可数。
func NewResourceSemaphore(maxCount int) *MaxCntResourceSemaphore {
	return &MaxCntResourceSemaphore{
		maxCount: maxCount,
		mu:       &sync.RWMutex{},
		curCount: 0,
	}
}
