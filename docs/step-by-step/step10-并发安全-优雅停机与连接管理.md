# Step 10：并发安全 — 优雅停机与连接管理

> **优化主线**：SchedulerV2 优雅停机缺陷 → ClientsV2 singleflight 优化 → ClientsV2 连接泄漏修复  
> **关键词**：WaitGroup、singleflight、连接池、资源泄漏

---

## 一、架构概览

```
                    ┌─────────────────────────────────────────┐
                    │            Application Lifecycle         │
                    │                                         │
                    │  Start()                 GracefulStop() │
                    │    │                          │         │
                    │    ▼                          ▼         │
                    │  ┌──────────┐          ┌───────────┐    │
                    │  │ goroutine│──wg.Add──│ wg.Wait() │    │
                    │  │ goroutine│──wg.Add──│ + timeout │    │
                    │  └──────────┘          └───────────┘    │
                    └─────────────────────────────────────────┘

                    ┌─────────────────────────────────────────┐
                    │         Connection Management            │
                    │                                         │
                    │  Get("svc-A") ─┐                        │
                    │  Get("svc-A") ─┼─ singleflight ─► 1 conn│
                    │  Get("svc-A") ─┘                        │
                    │                                         │
                    │  Close() ─► connMap.Range ─► conn.Close │
                    └─────────────────────────────────────────┘
```

---

## 二、优化点详解

### 2.1 SchedulerV2 优雅停机缺陷

#### 现状分析

**V1（正确实现）** — `internal/service/scheduler/scheduler.go`：

```go
// V1 Start() — 正确使用 WaitGroup
func (s *Scheduler) Start(ctx context.Context) {
    s.wg.Add(2)
    go func() {
        defer s.wg.Done()
        s.scheduleLoop(ctx)
    }()
    go func() {
        defer s.wg.Done()
        s.compensateLoop(ctx)
    }()
}

// V1 GracefulStop() — 等待所有 goroutine 退出
func (s *Scheduler) GracefulStop() {
    s.cancel()
    s.wg.Wait()  // ✅ 阻塞直到 goroutine 全部退出
}
```

**V2（存在缺陷）** — `internal/service/scheduler/schedulerv2.go`：

```go
// V2 Start() — 没有 WaitGroup 追踪
func (s *SchedulerV2) Start(ctx context.Context) {
    go s.scheduleLoop(ctx)   // ❌ 无 wg.Add
    go s.compensateLoop(ctx) // ❌ 无 wg.Add
}

// V2 GracefulStop() — 只取消 context，不等待退出
func (s *SchedulerV2) GracefulStop() {
    s.cancel()  // ❌ 没有 wg.Wait()
}
```

#### 问题

| 维度 | 描述 |
|------|------|
| **数据丢失** | `cancel()` 后 goroutine 可能正在执行 DB 写入（如 `UpdateNextScheduleTime`），进程提前退出导致事务中断 |
| **资源泄漏** | goroutine 内可能持有 DB 连接、Redis 连接等，未正常释放 |
| **不可观测** | 无法确认 goroutine 是否真正退出，日志中没有 "scheduler stopped" 之类的确认信息 |

#### 优化设计

```go
type SchedulerV2 struct {
    // ... 现有字段
    wg     sync.WaitGroup
    cancel context.CancelFunc
}

func (s *SchedulerV2) Start(ctx context.Context) {
    s.wg.Add(2)
    go func() {
        defer s.wg.Done()
        s.scheduleLoop(ctx)
    }()
    go func() {
        defer s.wg.Done()
        s.compensateLoop(ctx)
    }()
    s.logger.Info("SchedulerV2 started", zap.Int("goroutines", 2))
}

func (s *SchedulerV2) GracefulStop() {
    s.logger.Info("SchedulerV2 shutting down...")
    s.cancel()

    // 带超时的等待
    done := make(chan struct{})
    go func() {
        s.wg.Wait()
        close(done)
    }()

    select {
    case <-done:
        s.logger.Info("SchedulerV2 stopped gracefully")
    case <-time.After(30 * time.Second):
        s.logger.Error("SchedulerV2 shutdown timeout after 30s, forcing exit")
    }
}
```

**关键设计决策**：

1. **为什么是 30s 超时？** — 调度循环一次迭代最长时间约为 `DB 查询(~100ms) + gRPC 调用(~1s) + Kafka 发送(~500ms)`，留 30s 足够多个迭代完成收尾
2. **为什么不用 `context.WithTimeout`？** — `cancel()` 已经通知了 goroutine 退出，这里的超时是 **等待退出的超时**，是两个独立的控制面

---

### 2.2 ClientsV2 singleflight 优化

#### 现状分析

`pkg/grpc/clients_v2.go` 当前实现：

```go
func (c *ClientsV2) Get(ctx context.Context, serviceName string) (
    executorv1.ExecutorClient, error) {
    
    val, loaded := c.m.LoadOrStore(serviceName, &executorv1.UnimplementedExecutorClient{})
    if loaded {
        return val, nil  // 缓存命中
    }
    // 缓存未命中 — 创建新连接
    cc, err := grpc.NewClient(/* ... */)
    if err != nil {
        panic(err)  // ❌ 问题1: panic 不应该出现
    }
    newClient := executorv1.NewExecutorClient(cc)
    c.m.Store(serviceName, newClient)
    return newClient, nil
}
```

#### 问题

```
时间线：N 个 goroutine 并发请求同一 serviceName

goroutine-1 ─── LoadOrStore("svc-A") ─── miss ─── NewClient() ─── conn-1
goroutine-2 ─── LoadOrStore("svc-A") ─── miss ─── NewClient() ─── conn-2  ← 浪费
goroutine-3 ─── LoadOrStore("svc-A") ─── miss ─── NewClient() ─── conn-3  ← 浪费
...
goroutine-N ─── LoadOrStore("svc-A") ─── miss ─── NewClient() ─── conn-N  ← 浪费

结果：创建了 N 个 TCP 连接，只有最后 Store 的那个被保留，
      前 N-1 个连接泄漏（没有 Close）
```

`LoadOrStore` 的语义是：如果 key 不存在就存入，但 **多个 goroutine 可以同时判定 "不存在"**，因为存入的是一个 placeholder（`UnimplementedExecutorClient`），后续的 goroutine 在 placeholder 被替换前会看到 `loaded=true` 但拿到的是无用的 placeholder。

#### 优化设计

```go
import "golang.org/x/sync/singleflight"

type ClientsV2 struct {
    m       syncx.Map[string, executorv1.ExecutorClient]
    connMap syncx.Map[string, *grpc.ClientConn]  // 追踪连接
    sf      singleflight.Group
    // ...
}

func (c *ClientsV2) Get(ctx context.Context, serviceName string) (
    executorv1.ExecutorClient, error) {
    
    // 快路径：缓存命中
    if val, ok := c.m.Load(serviceName); ok {
        return val, nil
    }

    // 慢路径：singleflight 合并并发请求
    result, err, _ := c.sf.Do(serviceName, func() (interface{}, error) {
        // double check
        if val, ok := c.m.Load(serviceName); ok {
            return val, nil
        }

        cc, err := grpc.NewClient(
            fmt.Sprintf("etcd:///%s", serviceName),
            grpc.WithResolvers(c.resolver),
            grpc.WithDefaultServiceConfig(c.serviceConfig),
            grpc.WithTransportCredentials(insecure.NewCredentials()),
        )
        if err != nil {
            return nil, fmt.Errorf("create grpc client for %s: %w", serviceName, err)
        }

        client := executorv1.NewExecutorClient(cc)
        c.m.Store(serviceName, client)
        c.connMap.Store(serviceName, cc)  // 追踪连接引用
        return client, nil
    })

    if err != nil {
        return nil, err
    }
    return result.(executorv1.ExecutorClient), nil
}
```

**singleflight 原理**：

```
goroutine-1 ─── sf.Do("svc-A") ─── 执行 NewClient() ─── conn-1 ──┐
goroutine-2 ─── sf.Do("svc-A") ─── 等待...                        ├─ 共享结果
goroutine-3 ─── sf.Do("svc-A") ─── 等待...                        │
goroutine-N ─── sf.Do("svc-A") ─── 等待...  ──────────────────────┘

结果：只创建 1 个连接，N 个 goroutine 共享同一结果
```

---

### 2.3 ClientsV2 连接泄漏修复

#### 现状分析

当前 `ClientsV2` 没有 `Close()` 方法，`grpc.ClientConn` 的引用在 `NewClient` 后就丢失了：

```go
cc, err := grpc.NewClient(/* ... */)
// cc 的引用没有保存！
newClient := executorv1.NewExecutorClient(cc)
c.m.Store(serviceName, newClient)
// 从 ExecutorClient 接口无法反向获取 *grpc.ClientConn
```

#### 优化设计

```go
// Close 关闭所有 gRPC 连接
func (c *ClientsV2) Close() error {
    var firstErr error
    c.connMap.Range(func(serviceName string, cc *grpc.ClientConn) bool {
        if err := cc.Close(); err != nil {
            c.logger.Error("close grpc conn failed",
                zap.String("service", serviceName),
                zap.Error(err))
            if firstErr == nil {
                firstErr = err
            }
        }
        return true
    })
    return firstErr
}
```

在 `SchedulerApp` 的 shutdown 流程中调用：

```go
func (app *SchedulerApp) GracefulStop() {
    app.scheduler.GracefulStop()    // 1. 先停调度循环
    app.clients.Close()              // 2. 再关闭连接
    app.server.GracefulStop()        // 3. 最后停 gRPC server
}
```

**关闭顺序**很重要：先停止产生新请求的调度器，再关闭连接，最后停 server。

---

## 三、V1 → V2 对比

| 维度 | V1（现状） | V2 现状 | 优化后 |
|------|-----------|---------|--------|
| 优雅停机 | ✅ `wg.Wait()` | ❌ 只有 `cancel()` | ✅ `wg.Wait()` + 30s 超时 |
| 连接创建 | N/A（V1 不用 ClientsV2） | ❌ 并发创建 N 个连接 | ✅ singleflight 合并为 1 个 |
| 连接释放 | N/A | ❌ 无 Close 方法 | ✅ connMap + Close() |
| 错误处理 | — | ❌ `panic(err)` | ✅ 返回 error |

---

## 四、面试话术

### Q: 你项目中的并发安全问题是怎么处理的？

> "以调度器优雅停机为例。V1 用了标准的 `sync.WaitGroup` 模式——`Start()` 时 `wg.Add`，每个 goroutine `defer wg.Done`，`GracefulStop()` 先 `cancel()` 再 `wg.Wait()`。但 V2 重构时遗漏了 WaitGroup，只有 `cancel()` 没有 Wait，这意味着进程可能在 goroutine 还在执行 DB 操作时就退出了。修复方案是补上 WaitGroup，并且加了 30s 超时兜底，避免因 goroutine 卡住导致无限阻塞。"

### Q: gRPC 连接管理有什么考虑？

> "原始实现用 `sync.Map` 的 `LoadOrStore` 做缓存，但有个并发窗口问题——多个 goroutine 同时 miss 时会各自创建连接，只有最后一个被保留，前面的全部泄漏。我用 `singleflight` 解决了这个问题，同一 key 的并发请求只执行一次创建逻辑，其余等待共享结果。同时加了 `connMap` 追踪所有 `*grpc.ClientConn` 引用，提供 `Close()` 方法在 shutdown 时统一回收。"

### Q: singleflight 和 sync.Once 的区别？

> "sync.Once 是全局只执行一次，适合初始化场景。singleflight 是对 **相同 key 的并发请求** 去重——第一个请求执行，后续请求等待复用结果。但下一轮如果 key 没被缓存，又可以重新执行。更适合缓存填充这类场景。另外 singleflight 还有个 `DoChan` 方法支持超时控制。"
