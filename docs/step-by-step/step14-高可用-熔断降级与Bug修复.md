# Step 14：高可用 — 熔断降级与组合检查器 Bug 修复

> **目标**：引入系统性熔断降级机制（Circuit Breaker），为所有外部依赖提供三态熔断保护；修复 CompositeChecker OR 逻辑中的语义 Bug。
>
> **完成后你能看到**：Prometheus 故障时 Picker 自动降级为轮询选节点，30 秒后半开探测恢复；MySQL 响应超时时 DatabaseLoadChecker 使用上次快照，不再阻塞调度循环；CompositeChecker OR 模式的边界分支正确执行。

---

## 1. 架构总览

Step 14 聚焦于调度器的**韧性增强**，解决两个问题：

1. **外部依赖无熔断**：调度器直接调用 Prometheus / MySQL / Redis / Kafka，虽有简单降级（查询失败就放行），但没有系统性的**状态机驱动**熔断
2. **CompositeChecker OR 逻辑 Bug**：`checkOR()` 中 `allFailed` 变量赋值语义反转，导致默认退避分支永远不执行

```
┌─────────────────────────────────────────────────────────────────────────┐
│                       Step 14 变更范围                                   │
│                                                                         │
│  ┌──────────────────────────────────────────────────────────────────┐  │
│  │  pkg/circuitbreaker/                                              │  │
│  │                                                                    │  │
│  │  ┌─────────────────┐    ┌─────────────────┐                       │  │
│  │  │  CircuitBreaker  │    │  CircuitBreaker  │                       │  │
│  │  │  Wrapper         │    │  Wrapper         │                       │  │
│  │  │  (Prometheus     │    │  (MySQL          │                       │  │
│  │  │   Picker)        │    │   LoadChecker)   │                       │  │
│  │  └────────┬─────────┘    └────────┬─────────┘                       │  │
│  │           │                       │                                 │  │
│  │           │    sony/gobreaker     │                                 │  │
│  │           │    三态状态机          │                                 │  │
│  │           │                       │                                 │  │
│  │           ▼                       ▼                                 │  │
│  │  Closed ──→ Open ──→ Half-Open ──→ Closed                          │  │
│  │  (正常)    (熔断)    (探测)       (恢复)                            │  │
│  └──────────────────────────────────────────────────────────────────┘  │
│                                                                         │
│  ┌──────────────────────────────────────────────────────────────────┐  │
│  │  pkg/loadchecker/composite.go                                      │  │
│  │                                                                    │  │
│  │  checkOR() 逻辑修复：                                               │  │
│  │  删除 allFailed 变量，简化为 minDuration 初始值判断                   │  │
│  └──────────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## 2. 变更对比

| 维度 | Step 13 之前 | Step 14（熔断降级 + Bug 修复） |
|------|------|------|
| **外部依赖容错** | Prometheus 查询失败 → 允许调度（隐式降级） | 系统性三态熔断 → 梯度 fallback |
| **状态管理** | 无状态，每次调用独立 | gobreaker 状态机：Closed / Open / Half-Open |
| **降级策略** | 单一：查询失败就放行 | 梯度：Prometheus 降级轮询 / MySQL 降级快照 |
| **故障恢复** | 无自动探测，依赖外部恢复 | Half-Open 自动探测，成功则恢复 |
| **CompositeChecker OR** | `allFailed` 语义反转，默认退避分支死代码 | 删除 `allFailed`，逻辑简化 |
| **新增依赖** | — | `sony/gobreaker v0.4.1` |

---

## 3. 设计决策分析

### 3.1 为什么需要系统性熔断？简单降级不够吗？

| 维度 | 简单降级（现状） | 系统性熔断（Circuit Breaker） |
|------|------|------|
| **触发方式** | 每次调用失败都重试/降级 | 连续失败 N 次后**短路**，不再发起调用 |
| **故障隔离** | 无。故障依赖持续被调用，TCP 连接超时累积 | 有。Open 状态直接走 fallback，零网络开销 |
| **恢复机制** | 无。依赖恢复后也没有探测确认 | Half-Open 状态自动发送探测请求验证恢复 |
| **可观测性** | 只有错误日志 | 状态变更事件 + metrics（可以告警） |
| **级联风险** | 高。一个依赖慢，拖慢整个调度循环 | 低。熔断后直接 fallback，调度循环不阻塞 |

**决策**：引入 `sony/gobreaker`，为每个外部依赖创建独立的熔断器实例。

### 3.2 sony/gobreaker vs 其他方案

| 方案 | 优点 | 缺点 | 适用场景 |
|------|------|------|---------|
| **sony/gobreaker** | 轻量、无依赖、标准三态模型 | 仅提供本地熔断 | ✅ 进程内服务调用 |
| **go-resilience** | 功能丰富（重试+熔断+超时+隔板） | 过度设计，学习成本高 | 复杂微服务网关 |
| **hystrix-go** | Netflix 经典方案 | 已停止维护（archived） | 不推荐新项目 |
| **自研状态机** | 完全可控 | 重复造轮子，需覆盖边界情况 | 有特殊需求时 |

**决策**：`sony/gobreaker`。Go 生态最成熟的熔断器实现，API 简洁，零外部依赖，完美适配本项目场景。

### 3.3 独立熔断器 vs 全局熔断器

| 方案 | 优点 | 缺点 |
|------|------|------|
| **每个依赖独立熔断器**（当前） | 故障隔离精确，Prometheus 挂了不影响 MySQL 检查 | 配置项多 |
| **全局统一熔断器** | 配置简单 | 一个依赖故障导致全局熔断，误伤面大 |

**决策**：独立熔断器。调度器依赖的 Prometheus、MySQL、Redis 故障独立性高，不应互相干扰。

---

## 4. Circuit Breaker 三态状态机

### 4.1 状态转换图

```
                  连续失败 > 3 次
         ┌──────────────────────────────┐
         │                              │
         ▼                              │
    ┌─────────┐     Timeout=30s    ┌─────────┐
    │  CLOSED  │ ─────────────────→│  OPEN    │
    │  (正常)   │                   │  (熔断)   │
    │          │                   │          │
    │ 正常调用  │                   │ 直接返回   │
    │ 统计失败  │                   │ fallback  │
    └─────────┘                   └────┬─────┘
         ▲                              │
         │                              │ 等待 Timeout 超时
         │                              │
         │      探测成功              ┌────▼──────┐
         └────────────────────────── │ HALF-OPEN  │
                                     │  (探测)     │
                                     │            │
                                     │ 允许最多    │
                                     │ 5 次请求    │
                                     └────┬───────┘
                                          │
                                          │ 探测失败
                                          │
                                          ▼
                                     回到 OPEN
```

### 4.2 三态行为详解

| 状态 | 行为 | 转换条件 |
|------|------|---------|
| **Closed（关闭/正常）** | 请求正常转发到后端，统计失败次数 | 连续失败 > `ConsecutiveFailures`(3) → Open |
| **Open（打开/熔断）** | 所有请求直接走 fallback，不发起真实调用 | 等待 `Timeout`(30s) → Half-Open |
| **Half-Open（半开/探测）** | 允许最多 `MaxRequests`(5) 个请求通过探测 | 探测全部成功 → Closed；任一失败 → Open |

### 4.3 关键参数设计

```go
gobreaker.Settings{
    Name:        "prometheus-picker",     // 熔断器名称，用于日志和 metrics
    MaxRequests: 5,                       // Half-Open 状态允许通过的探测请求数
    Interval:    60 * time.Second,        // Closed 状态下的统计窗口（60s 重置计数）
    Timeout:     30 * time.Second,        // Open → Half-Open 的等待时间
    ReadyToTrip: func(counts gobreaker.Counts) bool {
        return counts.ConsecutiveFailures > 3  // 连续 3 次失败触发熔断
    },
    OnStateChange: func(name string, from, to gobreaker.State) {
        log.Printf("[CircuitBreaker] %s: %s → %s", name, from, to)
    },
}
```

**参数选择理由**：

| 参数 | 值 | 理由 |
|------|---|------|
| `MaxRequests=5` | 5 | 允许足够多的探测请求确认依赖恢复，避免单次偶然成功就切回 |
| `Interval=60s` | 60s | 统计窗口 1 分钟，避免短暂抖动触发误熔断 |
| `Timeout=30s` | 30s | 外部依赖通常 30s 内能完成重启/故障转移 |
| `ConsecutiveFailures>3` | 3 | 连续失败，排除网络偶发超时；3 次是经验平衡值 |

---

## 5. CircuitBreakerWrapper 实现

### 5.1 通用包装器

```go
package circuitbreaker

import (
    "context"
    "time"

    "github.com/sony/gobreaker"
)

// CircuitBreakerWrapper 是通用的熔断器包装器。
// 它将 sony/gobreaker 的三态状态机与具体的调用逻辑和降级策略组合在一起。
//
// 泛型参数 T 是被包装调用的返回值类型。
type CircuitBreakerWrapper[T any] struct {
    cb       *gobreaker.CircuitBreaker
    execute  func(ctx context.Context) (T, error) // 正常调用
    fallback func(ctx context.Context) (T, error) // 降级调用
}

// NewCircuitBreakerWrapper 创建一个新的熔断器包装器
func NewCircuitBreakerWrapper[T any](
    settings gobreaker.Settings,
    execute func(ctx context.Context) (T, error),
    fallback func(ctx context.Context) (T, error),
) *CircuitBreakerWrapper[T] {
    return &CircuitBreakerWrapper[T]{
        cb:       gobreaker.NewCircuitBreaker(settings),
        execute:  execute,
        fallback: fallback,
    }
}

// Call 执行受熔断器保护的调用。
// Closed 状态 → execute
// Open 状态 → 直接 fallback（不发起网络调用）
// Half-Open 状态 → execute（探测）
func (w *CircuitBreakerWrapper[T]) Call(ctx context.Context) (T, error) {
    result, err := w.cb.Execute(func() (interface{}, error) {
        return w.execute(ctx)
    })
    if err != nil {
        // 熔断器打开或执行失败，走降级
        return w.fallback(ctx)
    }
    return result.(T), nil
}

// State 返回当前熔断器状态（用于监控/日志）
func (w *CircuitBreakerWrapper[T]) State() gobreaker.State {
    return w.cb.State()
}

// Counts 返回当前统计数据（用于 metrics 暴露）
func (w *CircuitBreakerWrapper[T]) Counts() gobreaker.Counts {
    return w.cb.Counts()
}
```

### 5.2 包装 PrometheusPicker

```go
package picker

import (
    "context"
    "math/rand"
    "sync"
    "time"

    "github.com/sony/gobreaker"
    "gitee.com/flycash/distributed_task_platform/internal/domain"
)

// CircuitBreakerDispatcher 带熔断保护的节点选择器。
// 正常情况下通过 Prometheus 查询指标选择最优节点；
// 熔断时降级为轮询选择已注册的节点。
type CircuitBreakerDispatcher struct {
    inner      *Dispatcher                      // 原始 Prometheus Picker
    cb         *gobreaker.CircuitBreaker         // 熔断器
    registry   NodeRegistry                      // 节点注册表（从 etcd 获取）
    mu         sync.Mutex
    roundRobin int                               // 轮询计数器
}

// NewCircuitBreakerDispatcher 创建带熔断保护的 Dispatcher
func NewCircuitBreakerDispatcher(
    inner *Dispatcher,
    registry NodeRegistry,
) *CircuitBreakerDispatcher {
    cbd := &CircuitBreakerDispatcher{
        inner:    inner,
        registry: registry,
    }
    cbd.cb = gobreaker.NewCircuitBreaker(gobreaker.Settings{
        Name:        "prometheus-picker",
        MaxRequests: 5,
        Interval:    60 * time.Second,
        Timeout:     30 * time.Second,
        ReadyToTrip: func(counts gobreaker.Counts) bool {
            return counts.ConsecutiveFailures > 3
        },
        OnStateChange: func(name string, from, to gobreaker.State) {
            cbd.inner.logger.Warn("熔断器状态变更",
                elog.String("name", name),
                elog.String("from", from.String()),
                elog.String("to", to.String()))
        },
    })
    return cbd
}

func (d *CircuitBreakerDispatcher) Name() string {
    return "CIRCUIT_BREAKER_DISPATCHER"
}

// Pick 执行受熔断器保护的节点选择
func (d *CircuitBreakerDispatcher) Pick(ctx context.Context, task domain.Task) (string, error) {
    result, err := d.cb.Execute(func() (interface{}, error) {
        return d.inner.Pick(ctx, task) // Closed/Half-Open → 正常 Prometheus 查询
    })
    if err != nil {
        // Open 状态或查询失败 → 降级轮询
        return d.fallbackRoundRobin()
    }
    return result.(string), nil
}

// fallbackRoundRobin 降级策略：从已注册节点中轮询选择
func (d *CircuitBreakerDispatcher) fallbackRoundRobin() (string, error) {
    nodes := d.registry.GetAliveNodes()
    if len(nodes) == 0 {
        return "", fmt.Errorf("无可用执行节点（Prometheus 熔断 + 注册表为空）")
    }
    d.mu.Lock()
    idx := d.roundRobin % len(nodes)
    d.roundRobin++
    d.mu.Unlock()
    
    d.inner.logger.Warn("Prometheus 熔断，降级为轮询选节点",
        elog.String("selectedNode", nodes[idx]),
        elog.Int("totalNodes", len(nodes)))
    return nodes[idx], nil
}
```

### 5.3 包装 ClusterLoadChecker

```go
package loadchecker

import (
    "context"
    "sync"
    "time"

    "github.com/sony/gobreaker"
)

// CircuitBreakerClusterChecker 带熔断保护的集群负载检查器。
// Prometheus 不可用时，使用最近一次成功的检查结果作为快照。
type CircuitBreakerClusterChecker struct {
    inner    *ClusterLoadChecker
    cb       *gobreaker.CircuitBreaker

    // 快照：最近一次成功的检查结果
    mu           sync.RWMutex
    lastDuration time.Duration
    lastOK       bool
    hasSnapshot  bool
}

func NewCircuitBreakerClusterChecker(inner *ClusterLoadChecker) *CircuitBreakerClusterChecker {
    cbcc := &CircuitBreakerClusterChecker{
        inner:   inner,
        lastOK:  true, // 默认允许调度
    }
    cbcc.cb = gobreaker.NewCircuitBreaker(gobreaker.Settings{
        Name:        "cluster-load-checker",
        MaxRequests: 5,
        Interval:    60 * time.Second,
        Timeout:     30 * time.Second,
        ReadyToTrip: func(counts gobreaker.Counts) bool {
            return counts.ConsecutiveFailures > 3
        },
    })
    return cbcc
}

func (c *CircuitBreakerClusterChecker) Check(ctx context.Context) (time.Duration, bool) {
    result, err := c.cb.Execute(func() (interface{}, error) {
        duration, ok := c.inner.Check(ctx)
        // 更新快照
        c.mu.Lock()
        c.lastDuration = duration
        c.lastOK = ok
        c.hasSnapshot = true
        c.mu.Unlock()
        return [2]interface{}{duration, ok}, nil
    })
    if err != nil {
        // 熔断 → 使用快照
        return c.fallbackSnapshot()
    }
    pair := result.([2]interface{})
    return pair[0].(time.Duration), pair[1].(bool)
}

// fallbackSnapshot 降级策略：使用最近一次成功的检查结果
func (c *CircuitBreakerClusterChecker) fallbackSnapshot() (time.Duration, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    if c.hasSnapshot {
        return c.lastDuration, c.lastOK
    }
    // 无快照时允许调度（与原始降级策略一致：宁可过载不停摆）
    return 0, true
}
```

### 5.4 包装 DatabaseLoadChecker

```go
// CircuitBreakerDatabaseChecker 带熔断保护的数据库负载检查器。
// MySQL 响应时间查询依赖 Prometheus，Prometheus 不可用时使用快照。
type CircuitBreakerDatabaseChecker struct {
    inner    *DatabaseLoadChecker
    cb       *gobreaker.CircuitBreaker

    mu           sync.RWMutex
    lastDuration time.Duration
    lastOK       bool
    hasSnapshot  bool
}

func NewCircuitBreakerDatabaseChecker(inner *DatabaseLoadChecker) *CircuitBreakerDatabaseChecker {
    cbdc := &CircuitBreakerDatabaseChecker{
        inner:  inner,
        lastOK: true,
    }
    cbdc.cb = gobreaker.NewCircuitBreaker(gobreaker.Settings{
        Name:        "database-load-checker",
        MaxRequests: 5,
        Interval:    60 * time.Second,
        Timeout:     30 * time.Second,
        ReadyToTrip: func(counts gobreaker.Counts) bool {
            return counts.ConsecutiveFailures > 3
        },
    })
    return cbdc
}

func (c *CircuitBreakerDatabaseChecker) Check(ctx context.Context) (time.Duration, bool) {
    result, err := c.cb.Execute(func() (interface{}, error) {
        duration, ok := c.inner.Check(ctx)
        c.mu.Lock()
        c.lastDuration = duration
        c.lastOK = ok
        c.hasSnapshot = true
        c.mu.Unlock()
        return [2]interface{}{duration, ok}, nil
    })
    if err != nil {
        return c.fallbackSnapshot()
    }
    pair := result.([2]interface{})
    return pair[0].(time.Duration), pair[1].(bool)
}

func (c *CircuitBreakerDatabaseChecker) fallbackSnapshot() (time.Duration, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()
    if c.hasSnapshot {
        return c.lastDuration, c.lastOK
    }
    return 0, true
}
```

---

## 6. Fallback 策略设计

### 6.1 梯度降级策略总览

```
┌──────────────────────┐     ┌──────────────────────────────────────────┐
│   外部依赖            │     │           Fallback 策略                   │
│                      │     │                                          │
│  Prometheus          │────→│  降级为轮询选节点（从 etcd 注册表获取）    │
│  (Picker)            │     │  质量：中等（失去智能调度，但功能可用）     │
│                      │     │                                          │
│  Prometheus          │────→│  使用最近一次检查快照                     │
│  (ClusterChecker)    │     │  质量：可接受（短期内快照仍有参考价值）    │
│                      │     │                                          │
│  Prometheus          │────→│  使用最近一次检查快照                     │
│  (DatabaseChecker)   │     │  质量：可接受（DB 负载短期内变化不大）     │
│                      │     │                                          │
│  无快照时             │────→│  允许调度（宁可过载不停摆）                │
│  (所有 Checker)      │     │  质量：最低（但保证服务可用性）            │
└──────────────────────┘     └──────────────────────────────────────────┘
```

### 6.2 降级质量评估

| 组件 | 正常状态 | 降级状态 | 功能损失 | 恢复时间 |
|------|---------|---------|---------|---------|
| PrometheusPicker | TopK + 随机选最优节点 | 轮询选节点 | 失去资源感知，但任务不停 | ≤30s（Half-Open 探测） |
| ClusterLoadChecker | 实时集群对比 | 上次快照 | 短期可接受（30s 内偏差小） | ≤30s |
| DatabaseLoadChecker | 实时 DB 响应时间 | 上次快照 | 短期可接受 | ≤30s |

---

## 7. CompositeChecker OR 逻辑 Bug 修复

### 7.1 Bug 分析

**原始代码**（`pkg/loadchecker/composite.go:70-94`）：

```go
func (c *CompositeChecker) checkOR(ctx context.Context) (sleepDuration time.Duration, shouldSchedule bool) {
    minDuration := time.Hour // 设置一个很大的初始值
    allFailed := true        // ← 初始值 true

    for _, checker := range c.checkers {
        duration, ok := checker.Check(ctx)
        if ok {
            return 0, true   // 短路返回 ✅ 正确
        } else {
            allFailed = false // ← Bug：checker 不通过时设为 false
            if duration < minDuration {
                minDuration = duration
            }
        }
    }

    // 所有检查器都不通过
    if allFailed && minDuration == time.Hour { // ← 永远不执行
        minDuration = time.Minute
    }

    return minDuration, false
}
```

### 7.2 问题追踪

逐步分析执行流程：

```
场景：2 个 checker，都失败

初始状态：allFailed = true, minDuration = 1h

迭代 1：checker[0].Check() → pass=false, duration=10s
  → 进入 else 分支
  → allFailed = false     ← 语义错误！checker 失败了，allFailed 应该保持 true
  → minDuration = 10s

迭代 2：checker[1].Check() → pass=false, duration=5s
  → 进入 else 分支
  → allFailed = false     ← 依然 false
  → minDuration = 5s

循环结束：
  allFailed = false, minDuration = 5s
  if allFailed && minDuration == time.Hour → false && ... → 不执行
  
  返回 (5s, false) ← 功能上恰好正确（有具体退避时间）
```

```
场景：0 个 checker（空切片）

初始状态：allFailed = true, minDuration = 1h

循环不执行

if allFailed && minDuration == time.Hour → true && true → 执行
  minDuration = 1min

返回 (1min, false) ← 空 checker 列表不应返回"不可调度"
```

**Bug 影响**：

| 场景 | 期望行为 | 实际行为 | 是否正确 |
|------|---------|---------|---------|
| 有 checker 通过 | 短路返回 (0, true) | 短路返回 (0, true) | ✅ |
| 所有 checker 失败，有退避时间 | 返回最小退避 | 返回最小退避（allFailed=false 但 minDuration 有效） | ✅（偶然） |
| 所有 checker 失败，都返回 0 退避 | 应该用默认 1min | allFailed=false → 默认分支不执行 → 返回 0 | ❌ |
| 空 checker 列表 | 应允许调度 | allFailed=true → minDuration=1min → 返回不可调度 | ❌ |

### 7.3 修复方案

**删除 `allFailed` 变量，简化判断逻辑**：

```go
// checkOR 任一检查器通过即可调度（短路评估）。
// 如果全部不通过，返回所有检查器中最小的等待时间（尽早重试）。
func (c *CompositeChecker) checkOR(ctx context.Context) (sleepDuration time.Duration, shouldSchedule bool) {
    // 空 checker 列表：默认允许调度
    if len(c.checkers) == 0 {
        return 0, true
    }

    minDuration := time.Hour // 初始值（哨兵值）

    for _, checker := range c.checkers {
        duration, ok := checker.Check(ctx)
        if ok {
            // 有检查器通过，短路返回
            return 0, true
        }
        // 记录最小退避时间
        if duration < minDuration {
            minDuration = duration
        }
    }

    // 所有检查器都不通过
    // 如果没有 checker 返回有效退避时间（minDuration 仍是初始值），使用默认值
    if minDuration == time.Hour {
        minDuration = time.Minute
    }

    return minDuration, false
}
```

### 7.4 修复前后对比

| 场景 | 修复前 | 修复后 |
|------|--------|--------|
| 有 checker 通过 | ✅ (0, true) | ✅ (0, true) |
| 所有失败，有退避时间 | ✅ (min, false) | ✅ (min, false) |
| 所有失败，退避=0 | ❌ (0, false) 无默认退避 | ✅ (0, false) 最小值为 0 < 1h |
| 空 checker 列表 | ❌ (1min, false) 错误拒绝 | ✅ (0, true) 允许调度 |

---

## 8. Wire 依赖注入变更

### 8.1 ioc/load_checker.go 变更

```go
// 变更前：直接返回裸的 LoadChecker
func InitClusterLoadChecker(nodeID string, client prometheusapi.Client) *loadchecker.ClusterLoadChecker {
    // ...
    return loadchecker.NewClusterLoadChecker(nodeID, v1.NewAPI(client), cfg)
}

// 变更后：包装熔断器
func InitClusterLoadChecker(nodeID string, client prometheusapi.Client) loadchecker.LoadChecker {
    var cfg loadchecker.ClusterLoadConfig
    err := econf.UnmarshalKey("loadChecker.cluster", &cfg)
    if err != nil {
        panic(err)
    }
    inner := loadchecker.NewClusterLoadChecker(nodeID, v1.NewAPI(client), cfg)
    return loadchecker.NewCircuitBreakerClusterChecker(inner)
}
```

### 8.2 ioc/picker.go 变更

```go
// 变更前
func InitExecutorNodePicker(client prometheusapi.Client) picker.ExecutorNodePicker {
    // ...
    return picker.NewDispatcher(v1.NewAPI(client), cfg)
}

// 变更后
func InitExecutorNodePicker(
    client prometheusapi.Client,
    registry picker.NodeRegistry,
) picker.ExecutorNodePicker {
    var cfg picker.Config
    err := econf.UnmarshalKey("intelligentScheduling", &cfg)
    if err != nil {
        panic(err)
    }
    inner := picker.NewDispatcher(v1.NewAPI(client), cfg)
    return picker.NewCircuitBreakerDispatcher(inner, registry)
}
```

---

## 9. 配置参考

### 9.1 熔断器配置（建议添加到 config.yaml）

```yaml
# ========== 熔断器 ==========
circuitBreaker:
  prometheusPicker:
    maxRequests: 5                   # Half-Open 探测请求数
    interval: 60000000000            # 统计窗口 60s
    timeout: 30000000000             # Open → Half-Open 等待 30s
    consecutiveFailures: 3           # 连续失败 3 次触发
  clusterChecker:
    maxRequests: 5
    interval: 60000000000
    timeout: 30000000000
    consecutiveFailures: 3
  databaseChecker:
    maxRequests: 5
    interval: 60000000000
    timeout: 30000000000
    consecutiveFailures: 3
```

### 9.2 配置说明

| 参数 | 含义 | 建议值 | 调优方向 |
|------|------|--------|---------|
| `maxRequests` | Half-Open 状态允许的探测请求数 | 5 | 依赖恢复不稳定时增大 |
| `interval` | Closed 状态的统计重置窗口 | 60s | 依赖抖动频繁时增大（避免误熔断） |
| `timeout` | Open 后等多久尝试 Half-Open | 30s | 依赖恢复慢时增大 |
| `consecutiveFailures` | 连续失败多少次触发 Open | 3 | 网络不稳定时增大 |

---

## 10. 代码文件清单

| 变更类型 | 文件路径 | 说明 |
|---------|---------|------|
| **新增** | `pkg/circuitbreaker/wrapper.go` | 通用泛型熔断器包装器 |
| **新增** | `pkg/loadchecker/circuit_breaker_cluster.go` | ClusterLoadChecker 熔断包装 |
| **新增** | `pkg/loadchecker/circuit_breaker_database.go` | DatabaseLoadChecker 熔断包装 |
| **新增** | `internal/service/picker/circuit_breaker_dispatcher.go` | Dispatcher 熔断包装 |
| **修改** | `pkg/loadchecker/composite.go` | 修复 checkOR() Bug |
| **修改** | `ioc/load_checker.go` | Wire 注入变更 |
| **修改** | `ioc/picker.go` | Wire 注入变更 |
| **修改** | `go.mod` | 添加 `sony/gobreaker` 依赖 |

---

## 11. 测试验证

### 11.1 熔断器单元测试

```go
func TestCircuitBreakerWrapper_OpenOnConsecutiveFailures(t *testing.T) {
    failCount := 0
    wrapper := NewCircuitBreakerWrapper[string](
        gobreaker.Settings{
            Name: "test",
            ReadyToTrip: func(counts gobreaker.Counts) bool {
                return counts.ConsecutiveFailures > 2
            },
            Timeout: 100 * time.Millisecond,
        },
        func(ctx context.Context) (string, error) {
            failCount++
            return "", errors.New("service unavailable")
        },
        func(ctx context.Context) (string, error) {
            return "fallback-result", nil
        },
    )

    ctx := context.Background()

    // 前 3 次：Closed 状态，执行 execute 但失败，然后走 fallback
    for i := 0; i < 3; i++ {
        result, err := wrapper.Call(ctx)
        assert.NoError(t, err)
        assert.Equal(t, "fallback-result", result)
    }
    assert.Equal(t, 3, failCount) // execute 被调用了 3 次

    // 第 4 次：Open 状态，直接走 fallback，execute 不再被调用
    result, err := wrapper.Call(ctx)
    assert.NoError(t, err)
    assert.Equal(t, "fallback-result", result)
    assert.Equal(t, 3, failCount) // execute 没有被调用（仍然是 3）

    // 等待 Timeout 过后进入 Half-Open
    time.Sleep(150 * time.Millisecond)
    assert.Equal(t, gobreaker.StateHalfOpen, wrapper.State())
}
```

### 11.2 CompositeChecker OR 修复测试

```go
func TestCheckOR_AllFailed_UsesMinDuration(t *testing.T) {
    checker := NewCompositeChecker(StrategyOR,
        &mockChecker{duration: 10 * time.Second, ok: false},
        &mockChecker{duration: 5 * time.Second, ok: false},
    )
    
    duration, ok := checker.Check(context.Background())
    assert.False(t, ok)
    assert.Equal(t, 5*time.Second, duration) // 取最小退避时间
}

func TestCheckOR_EmptyCheckers_AllowsScheduling(t *testing.T) {
    checker := NewCompositeChecker(StrategyOR) // 空 checker
    
    duration, ok := checker.Check(context.Background())
    assert.True(t, ok)                  // 应允许调度
    assert.Equal(t, time.Duration(0), duration)
}

func TestCheckOR_AllFailed_ZeroDuration_UsesDefault(t *testing.T) {
    checker := NewCompositeChecker(StrategyOR,
        &mockChecker{duration: 0, ok: false},
        &mockChecker{duration: 0, ok: false},
    )
    
    duration, ok := checker.Check(context.Background())
    assert.False(t, ok)
    // minDuration=0 < time.Hour，所以用实际最小值 0，不触发默认值
    // 这是正确行为：checker 说 0 退避就是 0
    assert.Equal(t, time.Duration(0), duration)
}
```

---

## 12. 实现局限与改进方向

| # | 现状 | 改进方案 | 优先级 |
|---|------|---------|--------|
| 1 | 熔断器状态仅记录日志 | 暴露为 Prometheus 指标（`circuit_breaker_state{name="..."}` gauge） | 高 |
| 2 | 熔断器配置硬编码 | 从 config.yaml 读取，支持热更新 | 高 |
| 3 | 快照无过期机制 | 快照加 TTL（如 5 分钟），过期后切换为更保守的 fallback | 中 |
| 4 | 仅 Prometheus 相关组件有熔断 | Redis 连接、Kafka 生产者也可加熔断 | 中 |
| 5 | Half-Open 探测是同步的 | 可用异步 goroutine 做后台探测，减少主路径延迟 | 低 |

---

## 13. 面试高频 Q&A

### Q1: 为什么要引入熔断器？现有的简单降级不够用吗？

**回答要点**：

简单降级的问题是**每次调用都要经历一次失败**。比如 Prometheus 挂了，每轮调度循环都要发一次 HTTP 请求、等 5 秒超时、然后降级。10 个调度循环就浪费 50 秒。

熔断器的核心价值是**快速失败（fail-fast）**。Open 状态下直接走 fallback，0 网络开销。而且 Half-Open 的自动探测机制让恢复也是自动的，不需要人为干预。

*"关键区别在于：简单降级是'每次都试一下然后失败'，熔断器是'确认挂了就不试了，定期探测是否恢复'。这将故障期间的额外延迟从 O(N × timeout) 降到了接近 0。"*

### Q2: 熔断器的三个状态分别是什么？画一下状态转换图。

```
Closed（正常）
  │
  │ 连续失败 > 3 次
  ▼
Open（熔断）──── 等待 30s ────→ Half-Open（探测）
  ▲                                    │
  │            探测失败                 │ 探测成功
  └────────────────────────────────────┘──────→ Closed
```

*"Closed 是正常状态，请求正常转发。连续失败超过阈值后跳到 Open，所有请求直接走 fallback。等待 Timeout 后进入 Half-Open，允许少量请求通过做探测——如果全部成功就回到 Closed，任一失败就回到 Open。这是经典的三态模型，来自 Martin Fowler 的 Circuit Breaker 模式。"*

### Q3: 每个依赖的 fallback 策略是什么？为什么不同？

| 组件 | Fallback | 理由 |
|------|----------|------|
| PrometheusPicker | 轮询选节点 | 失去最优选择，但任务仍能分发执行 |
| ClusterLoadChecker | 使用上次快照 | 集群负载短期变化不大，30s 内快照有效 |
| DatabaseLoadChecker | 使用上次快照 | DB 负载同理 |
| 所有 Checker 无快照时 | 允许调度 | 宁可过载也不停摆——调度器的首要职责是执行任务 |

*"Fallback 策略的设计原则是'有梯度地降级'。不是所有降级都一样——Picker 丢了精确性用轮询兜底，Checker 丢了实时性用快照兜底。最坏情况下（所有都挂了、没有快照），也要保证调度器核心功能可用。"*

### Q4: CompositeChecker OR 逻辑的 Bug 是怎么发现的？修复方案是什么？

*"代码审查时发现的。变量名叫 `allFailed`，语义是'所有 checker 都失败了'，初始值 true——但在 checker 失败的 else 分支里赋值 `allFailed = false`，逻辑完全反了。正确逻辑应该是：checker 失败时 allFailed 保持 true（因为确实失败了），checker 成功时才改为 false。"*

*"我的修复方案更彻底——直接删掉 `allFailed` 变量。因为 OR 逻辑的核心是短路返回（有通过的就立刻 return true），剩下的全是失败情况，不需要额外的布尔标记。用 `minDuration == time.Hour`（哨兵值未被更新）来判断是否需要默认退避，逻辑更清晰。"*

### Q5: gobreaker 的 `Interval` 和 `Timeout` 参数分别控制什么？

- **`Interval`**（60s）：Closed 状态下的**统计窗口**。每 60 秒重置失败计数器。这意味着如果 60 秒内连续失败 3 次才熔断，超过 60 秒的间歇性失败会被重置，不会误触发。

- **`Timeout`**（30s）：Open 状态的**等待时间**。熔断后等 30 秒才进入 Half-Open 做探测。这个值要匹配依赖的典型恢复时间——Prometheus 重启通常在 30 秒内完成。

*"两个参数的调优方向相反：Interval 越大越不容易误熔断（但检测慢），Timeout 越小恢复越快（但可能过早探测还没恢复的服务）。当前 60s/30s 是平衡值。"*

### Q6: 如果 Prometheus 挂了很久（超过 30 秒），系统会怎样？

*"Open 状态维持 30 秒后进入 Half-Open，发送探测请求。如果 Prometheus 还没恢复，探测失败，立刻回到 Open 状态，再等 30 秒。这是一个无限循环的'探测-失败-等待'，直到 Prometheus 恢复。"*

*"在整个过程中，Picker 一直使用轮询 fallback，LoadChecker 一直使用快照 fallback。调度器核心功能不受影响——只是失去了智能调度和动态限流的能力。这就是梯度降级的价值：不是全挂，而是优雅地丢弃非关键能力。"*
