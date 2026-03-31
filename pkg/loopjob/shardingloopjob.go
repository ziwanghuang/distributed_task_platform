package loopjob

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gitee.com/flycash/distributed_task_platform/pkg/sharding"

	"github.com/gotomicro/ego/core/elog"
	"github.com/meoying/dlock-go"
)

// CtxKey 是 context 中存储业务数据的 key 类型。
type CtxKey string

// ShardingLoopJob 是分片循环任务框架的核心结构。
//
// 设计思想：
//   将 N 个分片（db×table 的笛卡尔积）视为 N 个独立的"工作单元"，
//   多个 scheduler 节点通过分布式锁竞争这些工作单元，
//   每个节点同时最多处理 resourceSemaphore.maxCount 个分片。
//
// 执行流程：
//   Run() → 外层无限循环
//     └─ 遍历所有分片（Broadcast）
//         ├─ Acquire 信号量（控制并发上限）
//         ├─ NewLock + Lock（竞争分布式锁）
//         └─ go tableLoop（启动独立 goroutine 处理该分片）
//              └─ bizLoop（在锁保护下持续执行业务）
//                   ├─ biz(ctx)（执行一次业务逻辑）
//                   └─ lock.Refresh（续约分布式锁）
//
// 关键设计：
//   - 分布式锁的 key 格式：{baseKey}:{db}:{table}，确保每个分片独立加锁
//   - 业务超时（bizTimeout=50s）< 锁 TTL（retryInterval=1min），确保业务在锁过期前完成
//   - 锁续约在每次业务循环后执行，如果续约失败则退出循环（说明已丢失锁）
//   - 信号量在 tableLoop 结束时 defer 释放，确保不泄漏
//
// 字段说明：
//   - shardingStrategy:  分片策略，提供 Broadcast() 返回所有分片列表
//   - baseKey:           分布式锁 key 前缀（如 "retry_compensator"）
//   - dclient:           分布式锁客户端
//   - biz:               业务函数，接收带分片信息的 context
//   - retryInterval:     获取锁失败时的重试间隔，也是锁的 TTL
//   - defaultTimeout:    Lock/Unlock/Refresh 等操作的超时时间
//   - resourceSemaphore: 资源信号量，控制单节点并发处理的分片数
type ShardingLoopJob struct {
	shardingStrategy  sharding.ShardingStrategy
	baseKey           string // 业务标识
	dclient           dlock.Client
	logger            *elog.Component
	biz               func(ctx context.Context) error
	retryInterval     time.Duration
	defaultTimeout    time.Duration
	resourceSemaphore ResourceSemaphore
}

// ShardingLoopJobOption 是 ShardingLoopJob 的可选配置函数类型。
type ShardingLoopJobOption func(*ShardingLoopJob)

// NewShardingLoopJob 创建一个分片循环任务实例。
//
// 参数：
//   - dclient:           分布式锁客户端
//   - baseKey:           业务标识，作为分布式锁 key 的前缀
//   - biz:               业务函数，每次循环调用一次。ctx 中携带当前分片信息（通过 sharding.DstFromCtx 获取）
//   - shardingStrategy:  分片策略
//   - resourceSemaphore: 资源信号量
//
// 默认配置：retryInterval=1min, defaultTimeout=3s。
func NewShardingLoopJob(
	dclient dlock.Client,
	baseKey string,
	// 你要执行的业务。注意当 ctx 被取消的时候，就会退出全部循环
	biz func(ctx context.Context) error,
	shardingStrategy sharding.ShardingStrategy,
	resourceSemaphore ResourceSemaphore,
) *ShardingLoopJob {
	const defaultTimeout = 3 * time.Second
	return newShardingLoopJobLoop(dclient, baseKey, biz, shardingStrategy, time.Minute, defaultTimeout, resourceSemaphore)
}

// newShardingLoopJobLoop 内部构造函数，允许指定 retryInterval 和 defaultTimeout，便于测试。
func newShardingLoopJobLoop(
	dclient dlock.Client,
	baseKey string,
	biz func(ctx context.Context) error,
	shardingStrategy sharding.ShardingStrategy,
	retryInterval time.Duration,
	defaultTimeout time.Duration,
	resourceSemaphore ResourceSemaphore,
) *ShardingLoopJob {
	return &ShardingLoopJob{
		dclient:           dclient,
		shardingStrategy:  shardingStrategy,
		baseKey:           baseKey,
		logger:            elog.DefaultLogger,
		biz:               biz,
		retryInterval:     retryInterval,
		resourceSemaphore: resourceSemaphore,
		defaultTimeout:    defaultTimeout,
	}
}

// generateKey 生成分片级别的分布式锁 key。
// 格式：{baseKey}:{db}:{table}，确保每个 db+table 组合独立加锁。
func (l *ShardingLoopJob) generateKey(db, tab string) string {
	return fmt.Sprintf("%s:%s:%s", l.baseKey, db, tab)
}

// Run 启动分片循环任务的主循环。
//
// 工作流程（双层无限循环）：
//  1. 外层循环：持续遍历所有分片
//  2. 内层循环：对每个分片依次执行：
//     a. 获取资源信号量（失败则 sleep 后重试）
//     b. 创建分布式锁并尝试加锁（失败则释放信号量后跳过）
//     c. 加锁成功后启动独立 goroutine 执行 tableLoop
//
// 退出条件：ctx 被取消（外部调用 cancel）。
func (l *ShardingLoopJob) Run(ctx context.Context) {
	for {
		for _, dst := range l.shardingStrategy.Broadcast() {
			// 超过允许抢占的上限了
			err := l.resourceSemaphore.Acquire(ctx)
			if err != nil {
				time.Sleep(l.retryInterval)
				continue
			}

			key := l.generateKey(dst.DB, dst.Table)
			// 强锁
			lock, err := l.dclient.NewLock(ctx, key, l.retryInterval)
			if err != nil {
				l.logger.Error("初始化分布式锁失败，重试",
					elog.Any("err", err))
				err = l.resourceSemaphore.Release(ctx)
				if err != nil {
					l.logger.Error("释放表的信号量失败", elog.FieldErr(err))
				}
				continue
			}

			lockCtx, cancel := context.WithTimeout(ctx, l.defaultTimeout)
			// 没有拿到锁，不管是系统错误，还是锁被人持有，都没有关系
			// 暂停一段时间之后继续
			err = lock.Lock(lockCtx)
			cancel()
			if err != nil {
				l.logger.Error("没有抢到分布式锁，系统出现问题", elog.Any("err", err))
				err = l.resourceSemaphore.Release(ctx)
				if err != nil {
					l.logger.Error("释放表的信号量失败", elog.FieldErr(err))
				}
				continue
			}
			// 抢占成功了
			go l.tableLoop(sharding.CtxWithDst(ctx, dst), lock)
		}
	}
}

// tableLoop 是单个分片的处理循环，在独立 goroutine 中运行。
//
// 执行流程：
//  1. defer 释放资源信号量（确保不泄漏）
//  2. 执行 bizLoop：在分布式锁保护下持续运行业务逻辑
//  3. bizLoop 退出后（续约失败或 ctx 取消），尝试释放分布式锁
//  4. 释放锁时使用 Background context，因为原始 ctx 可能已被取消
//  5. 根据退出原因决定：ctx 取消则退出，其他错误则 sleep 后等待重新调度
func (l *ShardingLoopJob) tableLoop(ctx context.Context, lock dlock.Lock) {
	defer func() {
		_ = l.resourceSemaphore.Release(ctx)
	}()
	// 在这里执行业务
	err := l.bizLoop(ctx, lock)
	// 要么是续约失败，要么是 ctx 本身已经过期了
	if err != nil {
		l.logger.Error("执行业务失败，将执行重试", elog.FieldErr(err))
	}
	// 不管是什么原因，都要考虑释放分布式锁了
	// 要稍微摆脱 ctx 的控制，因为此时 ctx 可能被取消了
	unCtx, cancel := context.WithTimeout(context.Background(), l.defaultTimeout)
	//nolint:contextcheck // 这里必须使用 Background Context，因为原始 ctx 可能已被取消，但仍需尝试解锁操作。
	unErr := lock.Unlock(unCtx)
	cancel()
	if unErr != nil {
		l.logger.Error("释放分布式锁失败", elog.Any("err", unErr))
	}
	err = ctx.Err()
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		// 被取消，那么就要跳出循环
		l.logger.Info("任务被取消，退出任务循环")
		return
	default:
		// 不可挽回的错误，后续考虑回去
		l.logger.Error("执行任务失败，将执行重试")
		time.Sleep(l.retryInterval)
	}
}

// bizLoop 在分布式锁保护下持续执行业务逻辑。
//
// 每次循环：
//  1. 创建带 50s 超时的 context 执行业务（确保在锁 TTL 内完成）
//  2. 检查 ctx 是否已取消（外部关闭信号）
//  3. 续约分布式锁（延长锁的持有时间）
//
// 退出条件：
//   - ctx 被取消 → 返回 ctx.Err()
//   - 锁续约失败 → 返回续约错误（上层 tableLoop 会释放锁）
func (l *ShardingLoopJob) bizLoop(ctx context.Context, lock dlock.Lock) error {
	const bizTimeout = 50 * time.Second
	for {
		// 可以确保业务在分布式锁过期之前结束
		bizCtx, cancel := context.WithTimeout(ctx, bizTimeout)
		err := l.biz(bizCtx)
		cancel()
		if err != nil {
			l.logger.Error("业务执行失败", elog.FieldErr(err))
		}
		if ctx.Err() != nil {
			// 要中断这个循环了
			return ctx.Err()
		}
		refCtx, cancel := context.WithTimeout(ctx, l.defaultTimeout)
		err = lock.Refresh(refCtx)
		cancel()
		if err != nil {
			return fmt.Errorf("分布式锁续约失败 %w", err)
		}
	}
}
