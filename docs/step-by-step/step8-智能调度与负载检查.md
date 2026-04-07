# Step 8：智能调度 + 负载检查

> **目标**：调度器根据执行节点的实际负载（CPU、内存）**智能选择最优节点**，同时在自身负载过高时**自动降频**，避免雪崩。核心能力：PrometheusPicker 基于 PromQL 查询的 TopN + 随机防惊群选节点，三级 LoadChecker（令牌桶 + DB 响应时间 + 集群平均对比）组合控制调度节奏。
>
> **完成后你能看到**：启动 Prometheus + 3 个执行器节点（CPU 空闲率分别为 95%、85%、30%）→ 创建一批 CPU_PRIORITY 任务 → 观察日志：Picker 查询 `topk(3, avg_over_time(executor_cpu_idle_percent[30s])...)` → TopN=[node-01, node-02]，选择 node-01 → 任务更多地被分配到低负载节点 → 人为制造 DB 慢查询 → DatabaseLoadChecker 检测到响应时间 150ms > 阈值 100ms → 调度器退避 10s → 恢复后自动恢复调度频率。

---

## 1. 架构总览

Step 8 在 Step 7 的调度循环中插入两个新能力：**调度前**通过 Picker 选择最优执行节点，**调度后**通过 LoadChecker 检查系统负载决定是否降频。两者与调度器核心逻辑解耦，通过接口注入。

```
┌───────────────────────────────────────────────────────────────────────────┐
│                           scheduleLoop (每轮循环)                          │
│                                                                           │
│  ┌─────────────────────────────────────────────────────────────────────┐  │
│  │ 1. metrics.StartRecordExecutionTime()  ← 开始计时                    │  │
│  └─────────────────────────────────────────────────────────────────────┘  │
│                              ↓                                            │
│  ┌─────────────────────────────────────────────────────────────────────┐  │
│  │ 2. taskSvc.SchedulableTasks(ctx)       ← 拉取可调度任务              │  │
│  └─────────────────────────────────────────────────────────────────────┘  │
│                              ↓                                            │
│  ┌─────────────────────────────────────────────────────────────────────┐  │
│  │ 3. for task := range tasks {                                        │  │
│  │        ctx = newContext(task)  ──────────────────────────┐           │  │
│  │        runner.Run(ctx, task)                             │           │  │
│  │    }                                                     │           │  │
│  └──────────────────────────────────────────────────────────┼──────────┘  │
│                              ↓                              │             │
│  ┌──────────────────────────────────────────────────────────┼──────────┐  │
│  │ 4. loadChecker.Check(ctx)              ← 负载检查        │           │  │
│  │    if !ok { sleep(duration) }          ← 降频            │           │  │
│  └──────────────────────────────────────────────────────────┼──────────┘  │
│                                                             │             │
│  ┌──────────────────────────────────────────────────────────▼──────────┐  │
│  │ newContext(task) 内部逻辑：                                          │  │
│  │                                                                     │  │
│  │  executorNodePicker.Pick(ctx, task)                                  │  │
│  │    ├── CPU_PRIORITY  → pickByMetric("executor_cpu_idle_percent")     │  │
│  │    ├── MEMORY_PRIORITY → pickByMetric("executor_memory_available_bytes")│ │
│  │    └── default       → fallback to CPU_PRIORITY                      │  │
│  │                                                                     │  │
│  │  PromQL: topk(3, avg_over_time(metric[30s])                         │  │
│  │              AND ON(instance) up{job="executors"} == 1)              │  │
│  │                                                                     │  │
│  │  结果: [node-01(95%), node-02(85%)] → rand → node-01               │  │
│  │  → balancer.WithSpecificNodeID(ctx, "node-01")                      │  │
│  │  → gRPC Picker 精确路由到 node-01                                   │  │
│  │                                                                     │  │
│  │  失败？→ 降级为原始 ctx（轮询策略）                                   │  │
│  └─────────────────────────────────────────────────────────────────────┘  │
└───────────────────────────────────────────────────────────────────────────┘

┌───────────────────────────────────────────────────────────────────────────┐
│                     LoadChecker 组合检查器                                  │
│                                                                           │
│  CompositeChecker (strategy=AND)                                          │
│  ├── LimiterChecker (令牌桶)                                              │
│  │   └── limiter.Allow()? → 有令牌→通过 / 无令牌→Reserve().Delay()退避    │
│  ├── DatabaseLoadChecker (DB 响应时间)                                     │
│  │   └── PromQL: avg(rate(gorm_query_duration_seconds_sum[5m]) /          │
│  │                    rate(gorm_query_duration_seconds_count[5m]))         │
│  │   └── avgTime > 100ms? → 退避 10s                                     │
│  └── ClusterLoadChecker (集群平均对比)                                     │
│      └── PromQL: avg(rate(scheduler_loop_duration_seconds_sum[5m]) /      │
│                       rate(scheduler_loop_duration_seconds_count[5m]))    │
│      └── currentDuration / avgDuration > 1.2? → 退避 current×2.0        │
│                                                                           │
│  AND 策略：全部通过 → 继续调度；任一不通过 → 取最大退避时间               │
└───────────────────────────────────────────────────────────────────────────┘
```

---

## 2. Step 7 → Step 8 变更对比

| 维度 | Step 7 | Step 8 新增/变更 |
|------|--------|-----------------|
| 节点选择 | 轮询（Round Robin） | Prometheus 驱动的智能选择（TopN + 随机） |
| 调度策略 | 无策略概念 | CPU_PRIORITY / MEMORY_PRIORITY / 自动降级 |
| 负载感知 | 无 | 三级负载检查器组合（AND 策略） |
| 限流 | 无 | 令牌桶（rate.Limiter）控制调度频率 |
| DB 保护 | 无 | 通过 GORM 插件上报的 gorm_query_duration_seconds 监控 DB 负载 |
| 集群均衡 | 无 | 对比当前节点与集群平均调度耗时，动态降频 |
| Prometheus | Docker Compose 中存在但未使用 | 完整集成：HTTP API 客户端 + PromQL 查询 + Remote Write 接收 |
| Prometheus 指标 | 无 | scheduler_loop_duration_seconds + load_checker_sleep_duration_seconds + gorm_query_duration_seconds |
| gRPC 负载均衡 | 轮询 + 排除节点 | 新增指定节点路由（WithSpecificNodeID） |
| Domain 模型 | 无 SchedulingStrategy 字段 | Task 新增 SchedulingStrategy（CPU_PRIORITY / MEMORY_PRIORITY） |
| 降级策略 | 无 | Prometheus 查询失败 → 回退到轮询；DB/集群检查失败 → 允许继续调度 |

---

## 3. 设计决策分析

### 3.1 TopN + 随机 vs 加权轮询

| | TopN + 随机（本项目） | 加权轮询 |
|---|---|---|
| **原理** | 查出前 N 个最优节点，随机选一个 | 按负载计算权重，概率分配 |
| **防惊群** | ✅ 随机打散 | ❌ 权重最高的节点可能被集中命中 |
| **实现复杂度** | 低（一条 PromQL + rand） | 中（需维护权重表、原子计数器） |
| **一致性** | 弱（每次查询可能结果不同） | 强（权重稳定时分配确定） |
| **选择原因** | 调度场景不需要严格一致性，防惊群更重要 | — |

**决策**：选 TopN + 随机。任务调度的核心诉求是"不要把大量任务集中到同一个节点"，TopN 保证只选低负载节点，随机保证不集中到其中某一个。实现只需一条 `topk()` PromQL 查询。

### 3.2 拉模型（Pull）vs 推模型（Push）采集负载

| | 拉模型（本项目） | 推模型 |
|---|---|---|
| **原理** | 调度器主动查 Prometheus | 执行节点主动上报到调度器 |
| **时效性** | 秒级（取决于 scrape_interval） | 毫秒级 |
| **可靠性** | Prometheus 挂 → 降级为轮询 | 节点网络抖动 → 上报丢失 |
| **耦合度** | 低（执行节点不知道调度器存在） | 高（需双向通信） |
| **历史数据** | ✅ Prometheus 天然支持 | 需自行维护 |
| **选择原因** | 秒级时效足够，解耦更重要，且 Prometheus 已部署 | — |

**决策**：选拉模型。调度决策的频率是秒级（scheduleInterval=10s），Prometheus 的 15s scrape_interval 完全满足。更关键的是，执行节点不需要知道调度器的存在——它们只需要暴露 `/metrics` 端点。

### 3.3 AND 组合 vs OR 组合

| | AND（本项目） | OR |
|---|---|---|
| **语义** | 所有检查器都通过才调度 | 任一检查器通过就调度 |
| **退避时间** | 取所有不通过检查器中的**最大值** | 取所有不通过检查器中的**最小值** |
| **安全性** | 高（任一维度过载都会触发保护） | 低（可能在部分过载下继续调度） |
| **适用场景** | 生产环境 | 测试/开发环境 |
| **选择原因** | 调度器负载检查是"安全阀"，宁可多退避也不能雪崩 | — |

**决策**：生产环境使用 AND 策略。`CompositeChecker(StrategyAND, limiter, database)` 确保令牌桶和 DB 负载同时达标才继续调度。`ClusterLoadChecker` 作为独立参数注入调度器，在调度循环外层独立检查。

### 3.4 固定退避 vs 指数退避

DatabaseLoadChecker 使用**固定退避**（10s），ClusterLoadChecker 使用**动态退避**（`currentDuration × slowdownMultiplier`）：

- **DatabaseLoadChecker 固定退避**：DB 过载是全局性的，所有节点看到的状态一致，固定退避简单可预测。
- **ClusterLoadChecker 动态退避**：节点间性能差异大，慢节点退避时间 = 当前耗时 × 2，越慢退避越久，自然实现"快节点多干、慢节点少干"。

---

## 4. 智能节点选择器（Picker）

### 4.1 接口定义

```go
// internal/service/picker/types.go

type ExecutorNodePicker interface {
    Name() string
    Pick(ctx context.Context, task domain.Task) (nodeID string, err error)
}
```

`Pick` 方法接收一个 `domain.Task`，根据任务的 `SchedulingStrategy` 字段选择最优执行节点，返回 nodeID。

### 4.2 Domain 模型扩展

```go
// internal/domain/task.go

type SchedulingStrategy string

const (
    SchedulingStrategyCPUPriority    SchedulingStrategy = "CPU_PRIORITY"
    SchedulingStrategyMemoryPriority SchedulingStrategy = "MEMORY_PRIORITY"
)

func (t SchedulingStrategy) IsCPUPriority() bool    { return t == SchedulingStrategyCPUPriority }
func (t SchedulingStrategy) IsMemoryPriority() bool { return t == SchedulingStrategyMemoryPriority }
```

Task 结构体中新增 `SchedulingStrategy SchedulingStrategy` 字段，由用户在创建任务时指定调度偏好。

### 4.3 Dispatcher — 策略分发门面

```go
// internal/service/picker/dispatcher.go

type Dispatcher struct {
    basePicker *prometheusPicker  // 底层 Prometheus 查询引擎
    config     Config
}

func (d *Dispatcher) Pick(ctx context.Context, task domain.Task) (string, error) {
    var metricName string
    switch {
    case task.SchedulingStrategy.IsCPUPriority():
        metricName = "executor_cpu_idle_percent"       // CPU 空闲率越高越好
    case task.SchedulingStrategy.IsMemoryPriority():
        metricName = "executor_memory_available_bytes" // 可用内存越多越好
    default:
        metricName = "executor_cpu_idle_percent"       // 安全回退
    }
    return d.basePicker.pickByMetric(ctx, metricName)
}
```

**设计模式**：策略 + 门面。`Dispatcher` 是对外的唯一入口，内部按 `SchedulingStrategy` 分派到不同的 PromQL 指标查询。新增策略只需在 switch 中加一个 case + 定义一个指标名。

### 4.4 prometheusPicker — 核心查询引擎

```go
// internal/service/picker/prometheus_picker.go

func (p *prometheusPicker) pickByMetric(ctx context.Context, metricName string) (string, error) {
    // 1. 构造 PromQL
    query := fmt.Sprintf(
        `topk(%d, avg_over_time(%s[%s]) AND ON(instance) up{job=%q} == 1)`,
        p.config.TopNCandidates,  // 3
        metricName,               // executor_cpu_idle_percent
        p.config.TimeWindow,      // 30s
        p.config.JobName,         // executors
    )
    // 实际生成: topk(3, avg_over_time(executor_cpu_idle_percent[30s])
    //                    AND ON(instance) up{job="executors"} == 1)

    // 2. 执行查询（带超时）
    queryCtx, cancel := context.WithTimeout(ctx, p.config.QueryTimeout) // 5s
    defer cancel()
    result, _, err := p.promClient.Query(queryCtx, query, time.Now())

    // 3. 解析结果
    nodeIDs := p.parseQueryResult(result)  // 从 sample.Metric["node_id"] 提取

    // 4. TopN 中随机选一个（防惊群）
    selectedNodeID := nodeIDs[rand.Intn(len(nodeIDs))]
    return selectedNodeID, nil
}
```

**PromQL 拆解**：

```
topk(3,                                          ← 取最优的前 3 个
  avg_over_time(executor_cpu_idle_percent[30s])   ← 30s 窗口平均值（消除抖动）
  AND ON(instance)                                ← 通过 instance 标签关联
  up{job="executors"} == 1                        ← 只选存活节点
)
```

这条 PromQL 的语义是：**在所有存活的执行器节点中，取 CPU 空闲率 30 秒均值最高的前 3 个**。

### 4.5 配置

```go
// internal/service/picker/types.go

type Config struct {
    JobName        string        `yaml:"jobName"`        // 默认 "executors"
    TopNCandidates int           `yaml:"topNCandidates"` // 默认 3
    TimeWindow     time.Duration `yaml:"timeWindow"`     // 默认 1 分钟
    QueryTimeout   time.Duration `yaml:"queryTimeout"`   // 默认 5 秒
}
```

对应 `config.yaml`：

```yaml
intelligentScheduling:
  jobName: "executors"
  topNCandidates: 3
  timeWindow: 30000000000      # 30s
  queryTimeout: 5000000000     # 5s
```

### 4.6 与 gRPC 负载均衡器的集成

Picker 选出 nodeID 后，通过 context 注入：

```go
// internal/service/scheduler/scheduler.go

func (s *Scheduler) newContext(task domain.Task) context.Context {
    if nodeID, err := s.executorNodePicker.Pick(s.ctx, task); err == nil && nodeID != "" {
        // 智能调度成功 → 注入指定节点 ID
        return balancer.WithSpecificNodeID(s.ctx, nodeID)
    }
    // 智能调度失败 → 降级为轮询
    return s.ctx
}
```

gRPC Balancer 层的 `routingPicker.Pick()` 会优先检查 `SpecificNodeIDContextKey`：

```go
// pkg/grpc/balancer/routing_picker.go

func (p *routingPicker) Pick(info balancer.PickInfo) (balancer.PickResult, error) {
    // 优先级 1：检查是否有指定节点 ID（由 PrometheusPicker 注入）
    specificNodeID, hasSpecific := GetSpecificNodeID(info.Ctx)
    if hasSpecific {
        for i := range p.nodeIDs {
            if p.nodeIDs[i] == specificNodeID {
                return balancer.PickResult{SubConn: p.subConns[i]}, nil
            }
        }
        return balancer.PickResult{}, status.Errorf(codes.Unavailable, "指定的节点不可用: %s", specificNodeID)
    }

    // 优先级 2：检查排除节点 ID（由分片调度失败重试注入）
    // ...

    // 优先级 3：普通轮询
    return p.pickRoundRobin(), nil
}
```

**完整调用链路**：

```
Scheduler.scheduleLoop()
  → newContext(task)
    → Dispatcher.Pick(ctx, task)
      → prometheusPicker.pickByMetric("executor_cpu_idle_percent")
        → Prometheus Query API → 返回 TopN 节点 IDs
      → rand.Intn(len) → "node-01"
    → balancer.WithSpecificNodeID(ctx, "node-01")
  → runner.Run(ctx, task)
    → invoker.Invoke(ctx, ...)
      → gRPC Dial → routingPicker.Pick(info)
        → GetSpecificNodeID(info.Ctx) → "node-01" → 精确路由
```

---

## 5. 三级负载检查器（LoadChecker）

### 5.1 接口定义

```go
// pkg/loadchecker/types.go

type LoadChecker interface {
    Check(ctx context.Context) (sleepDuration time.Duration, shouldSchedule bool)
}
```

- `shouldSchedule = true`：允许继续调度
- `shouldSchedule = false`：需要退避 `sleepDuration` 后再调度

Context 传递执行时间：

```go
const ExecutionTimeContextKey contextKey = "execution_time"

func WithExecutionTime(ctx context.Context, duration time.Duration) context.Context
func GetExecutionTime(ctx context.Context) time.Duration
```

### 5.2 LimiterChecker — 令牌桶限流

**原理**：基于 `golang.org/x/time/rate.Limiter`，控制调度频率的绝对上限。

```go
// pkg/loadchecker/limiter.go

type LimiterChecker struct {
    limiter      *rate.Limiter     // 令牌桶
    waitDuration time.Duration     // 最小等待时间（兜底）
}

func (c *LimiterChecker) Check(_ context.Context) (time.Duration, bool) {
    // 1. 尝试获取令牌
    if c.limiter.Allow() {
        return 0, true  // 有令牌，允许调度
    }

    // 2. 无令牌 → 计算精确等待时间
    reservation := c.limiter.Reserve()
    sleepDuration := reservation.Delay()
    reservation.Cancel()  // 不实际消耗令牌（由调用方 sleep 后重试）

    // 3. 兜底保护：防止等待时间过短导致忙等
    if c.waitDuration > 0 && sleepDuration < c.waitDuration {
        sleepDuration = c.waitDuration
    }
    return sleepDuration, false
}
```

**配置**：

```yaml
loadChecker:
  limiter:
    rateLimit: 10.0          # 每秒 10 次调度
    burstSize: 20            # 突发容量 20
    waitDuration: 5000000000 # 最小等待 5s
```

**为什么用 `Allow()` 而不是 `Reserve().OK()`**：`Reserve().OK()` 在 `rate != Inf` 时永远返回 true（它只告诉你"预定成功但可能需要等待"），而 `Allow()` 是真正的"当前是否有可用令牌"检查。

### 5.3 DatabaseLoadChecker — DB 响应时间

**原理**：通过 Prometheus 查询 GORM 插件上报的数据库平均响应时间，超过阈值则降频。

```go
// pkg/loadchecker/database.go

type DatabaseLoadChecker struct {
    promClient      v1.API
    threshold       time.Duration  // 100ms
    timeWindow      time.Duration  // 5m
    backoffDuration time.Duration  // 10s
}

func (c *DatabaseLoadChecker) Check(ctx context.Context) (time.Duration, bool) {
    avgTime, err := c.queryDBAvgTime(ctx)
    if err != nil {
        return 0, true  // 降级策略：查询失败允许继续调度
    }
    if avgTime > c.threshold {
        return c.backoffDuration, false  // DB 过载，固定退避
    }
    return 0, true
}
```

**PromQL**：

```
avg(
  rate(gorm_query_duration_seconds_sum[5m])
  /
  rate(gorm_query_duration_seconds_count[5m])
)
```

语义：先计算每个实例的平均单次查询耗时（sum 增速 / count 增速），再对所有实例取平均。

**GORM 插件数据来源**：`pkg/prometheus/gorm_plugin.go` 中的 `GormMetricsPlugin` 在每个 CRUD 操作的 before/after 回调中自动记录 `gorm_query_duration_seconds` 指标（Histogram，标签：operation, table）。

### 5.4 ClusterLoadChecker — 集群平均对比

**原理**：将当前节点的调度循环耗时与集群所有节点的平均耗时比较，超过阈值则该节点降频。

```go
// pkg/loadchecker/cluster.go

type ClusterLoadChecker struct {
    nodeID             string
    promClient         v1.API
    thresholdRatio     float64        // 1.2（超过平均值 20%）
    timeWindow         time.Duration  // 5m
    slowdownMultiplier float64        // 2.0
    minBackoffDuration time.Duration  // 5s
}

func (c *ClusterLoadChecker) Check(ctx context.Context) (time.Duration, bool) {
    // 1. 从 context 获取当前循环执行耗时
    currentDuration := GetExecutionTime(ctx)
    if currentDuration == 0 { return 0, true }

    // 2. 查询集群平均循环耗时
    avgDuration, err := c.queryClusterAverageTime(ctx)
    if err != nil { return 0, true }  // 降级
    if avgDuration == 0 { return 0, true }

    // 3. 计算性能比例
    ratio := float64(currentDuration) / float64(avgDuration)

    // 4. 超过阈值 → 动态退避
    if ratio > c.thresholdRatio {  // ratio > 1.2
        backoff := time.Duration(float64(currentDuration) * c.slowdownMultiplier)
        if backoff < c.minBackoffDuration { backoff = c.minBackoffDuration }
        return backoff, false
    }
    return 0, true
}
```

**PromQL**：

```
avg(
  rate(scheduler_loop_duration_seconds_sum[5m])
  /
  rate(scheduler_loop_duration_seconds_count[5m])
)
```

**关键设计**：退避时间不是固定的，而是 `currentDuration × 2.0`。越慢的节点退避越久，自然实现"快节点多干、慢节点少干"。

### 5.5 CompositeChecker — 组合检查器

```go
// pkg/loadchecker/composite.go

type CompositeChecker struct {
    checkers []LoadChecker
    strategy CompositeStrategy  // AND / OR
}
```

**AND 策略**（生产环境使用）：

```go
func (c *CompositeChecker) checkAND(ctx context.Context) (time.Duration, bool) {
    var maxDuration time.Duration
    for _, checker := range c.checkers {
        duration, ok := checker.Check(ctx)
        if !ok {
            if duration > maxDuration { maxDuration = duration }
        }
    }
    return maxDuration, maxDuration == 0
}
```

遍历所有检查器，收集不通过的检查器中最大的退避时间。`maxDuration == 0` 说明全部通过。

**OR 策略**（短路评估）：

```go
func (c *CompositeChecker) checkOR(ctx context.Context) (time.Duration, bool) {
    minDuration := time.Hour
    for _, checker := range c.checkers {
        duration, ok := checker.Check(ctx)
        if ok { return 0, true }  // 任一通过，立即返回
        if duration < minDuration { minDuration = duration }
    }
    return minDuration, false  // 全部不通过，返回最小等待时间（尽早重试）
}
```

### 5.6 在调度循环中的集成位置

```go
// internal/service/scheduler/scheduler.go — scheduleLoop()

func (s *Scheduler) scheduleLoop() {
    for {
        // ① 计时开始
        stopFunc := s.metrics.StartRecordExecutionTime()

        // ② 拉取任务 + 执行
        tasks := s.taskSvc.SchedulableTasks(...)
        for i := range tasks {
            s.runner.Run(s.newContext(tasks[i]), tasks[i])
        }

        // ③ 计时结束 → 得到 duration
        // ④ 注入 duration 到 context → 传给 loadChecker
        duration, ok := s.loadChecker.Check(
            loadchecker.WithExecutionTime(s.ctx, stopFunc()),
        )
        if !ok {
            s.metrics.RecordLoadCheckerSleep("composite", duration)
            time.Sleep(duration)  // 退避
        }
    }
}
```

**注意**：`loadChecker` 字段接收的是 `CompositeChecker`（AND 组合了 Limiter + Database），`ClusterLoadChecker` 作为独立字段注入到 `Scheduler` 构造函数中（实际在当前实现中直接作为 `loadChecker` 传入）。

---

## 6. Prometheus 可观测性指标

### 6.1 SchedulerMetrics — 调度器指标

```go
// pkg/prometheus/scheduler_metrics.go

type SchedulerMetrics struct {
    nodeID        string
    loopDuration  *prometheus.HistogramVec  // scheduler_loop_duration_seconds
    sleepDuration *prometheus.HistogramVec  // load_checker_sleep_duration_seconds
}
```

| 指标名 | 类型 | 标签 | 用途 |
|--------|------|------|------|
| `scheduler_loop_duration_seconds` | Histogram | `node_id` | 调度循环耗时，被 ClusterLoadChecker 查询 |
| `load_checker_sleep_duration_seconds` | Histogram | `node_id`, `checker_type` | 负载检查器触发的退避时间 |

**关键方法**：

```go
// 开始记录 → 返回停止函数
func (m *SchedulerMetrics) StartRecordExecutionTime() func() time.Duration {
    start := time.Now()
    return func() time.Duration {
        duration := time.Since(start)
        m.loopDuration.WithLabelValues(m.nodeID).Observe(duration.Seconds())
        return duration  // 返回给 loadChecker 使用
    }
}

// 记录退避时间（仅 > 0 时记录，避免零值污染直方图）
func (m *SchedulerMetrics) RecordLoadCheckerSleep(checkerType string, duration time.Duration) {
    if duration > 0 {
        m.sleepDuration.WithLabelValues(m.nodeID, checkerType).Observe(duration.Seconds())
    }
}
```

### 6.2 GormMetricsPlugin — GORM 数据库指标

```go
// pkg/prometheus/gorm_plugin.go

type GormMetricsPlugin struct {
    queryDuration *prometheus.HistogramVec  // gorm_query_duration_seconds
}
```

| 指标名 | 类型 | 标签 | 用途 |
|--------|------|------|------|
| `gorm_query_duration_seconds` | Histogram | `operation`, `table` | 数据库操作耗时，被 DatabaseLoadChecker 查询 |

**实现方式**：GORM 回调插件，在每个 CRUD 操作（query/create/update/delete/row/raw）的 before/after 回调中记录耗时。

```go
// before 回调：记录开始时间
func (p *GormMetricsPlugin) before(operation string) func(*gorm.DB) {
    return func(db *gorm.DB) {
        db.Set(callbackStartTimeKey, time.Now())
        db.Set(callbackOperationKey, operation)
    }
}

// after 回调：计算耗时并上报
func (p *GormMetricsPlugin) after(db *gorm.DB) {
    startTime := db.Get(callbackStartTimeKey)
    duration := time.Since(startTime)
    table := db.Statement.Table
    p.queryDuration.WithLabelValues(operation, table).Observe(duration.Seconds())
}
```

### 6.3 执行器节点指标（由执行器上报）

| 指标名 | 类型 | 标签 | 用途 |
|--------|------|------|------|
| `executor_cpu_idle_percent` | Gauge | `job`, `instance`, `node_id` | CPU 空闲率，被 PrometheusPicker 查询 |
| `executor_memory_available_bytes` | Gauge | `job`, `instance`, `node_id` | 可用内存字节数，被 PrometheusPicker 查询 |
| `up` | Gauge | `job`, `instance`, `node_id` | 节点存活状态，用于 PromQL 过滤 |

这些指标由执行器节点通过 Prometheus Remote Write API 推送到 Prometheus。

### 6.4 指标数据流向

```
┌─────────────┐   Remote Write    ┌─────────────┐  PromQL Query  ┌──────────────┐
│ Executor-01 │ ──────────────→  │  Prometheus  │ ←───────────── │ Scheduler    │
│ Executor-02 │   (cpu/mem/up)   │             │  topk(3,...)   │ Picker       │
│ Executor-03 │                  │             │                │ LoadChecker  │
└─────────────┘                  └──────┬──────┘                └──────────────┘
                                        │                              │
┌─────────────┐   GORM Plugin          │                              │
│   MySQL     │ ←─── query/create ──── │ ◄── gorm_query_duration      │
│             │   duration recorded    │     被 DatabaseLoadChecker   │
└─────────────┘                        │     查询                      │
                                        │                              │
                                        │   scheduler_loop_duration    │
                                        │     被 ClusterLoadChecker    │
                                        │     查询                      │
                                        └──────────────────────────────┘
```

---

## 7. IOC 装配

### 7.1 Prometheus 客户端

```go
// ioc/prometheus.go

func InitPrometheusClient() prometheusapi.Client {
    var cfg struct{ URL string `yaml:"url"` }
    econf.UnmarshalKey("prometheus", &cfg)
    client, _ := prometheusapi.NewClient(prometheusapi.Config{Address: cfg.URL})
    return client
}
```

### 7.2 Picker 初始化

```go
// ioc/picker.go

func InitExecutorNodePicker(promClient prometheusapi.Client) picker.ExecutorNodePicker {
    var config picker.Config
    econf.UnmarshalKey("intelligentScheduling", &config)
    v1API := v1.NewAPI(promClient)
    return picker.NewDispatcher(v1API, config)
}
```

### 7.3 LoadChecker 初始化

```go
// ioc/load_checker.go

// 组合检查器 = Limiter + Database（AND 策略）
func InitLoadCompositeChecker(client prometheusapi.Client) *loadchecker.CompositeChecker {
    return loadchecker.NewCompositeChecker(
        loadchecker.StrategyAND,
        InitLimiterLoadChecker(),
        InitDatabaseLoadChecker(client),
    )
}

// 集群负载检查器（独立注入到 Scheduler）
func InitClusterLoadChecker(nodeID string, client prometheusapi.Client) *loadchecker.ClusterLoadChecker {
    var cfg loadchecker.ClusterLoadConfig
    econf.UnmarshalKey("loadChecker.cluster", &cfg)
    return loadchecker.NewClusterLoadChecker(nodeID, v1.NewAPI(client), cfg)
}
```

### 7.4 Scheduler 装配

```go
// ioc/scheduler.go

func InitScheduler(
    nodeID string,
    runner runner.Runner,
    taskSvc task.Service,
    execSvc task.ExecutionService,
    acquirer acquirer.TaskAcquirer,
    grpcClients *grpc.ClientsV2[executorv1.ExecutorServiceClient],
    lc *loadchecker.ClusterLoadChecker,  // ClusterLoadChecker 独立注入
    nodePicker picker.ExecutorNodePicker,  // Picker 独立注入
) *scheduler.Scheduler {
    return scheduler.NewScheduler(
        nodeID, runner, taskSvc, execSvc, acquirer, grpcClients, cfg,
        lc,  // 作为 loadChecker 参数
        prometheus.NewSchedulerMetrics(nodeID),
        nodePicker,
    )
}
```

### 7.5 Wire 依赖图

```go
// cmd/scheduler/ioc/wire.go

BaseSet = wire.NewSet(
    ioc.InitDB,
    ioc.InitDistributedLock,
    ioc.InitEtcdClient,
    ioc.InitMQ,
    ioc.InitRunner,
    ioc.InitInvoker,
    ioc.InitRegistry,
    ioc.InitPrometheusClient,  // ← Step 8 新增
)

schedulerSet = wire.NewSet(
    ioc.InitNodeID,
    ioc.InitClusterLoadChecker,     // ← Step 8 新增
    ioc.InitScheduler,
    ioc.InitMySQLTaskAcquirer,
    ioc.InitExecutorServiceGRPCClients,
    ioc.InitExecutorNodePicker,     // ← Step 8 新增
)
```

生成的 `wire_gen.go` 中的装配顺序：

```go
func InitSchedulerApp() *ioc.SchedulerApp {
    // ...
    client := ioc.InitPrometheusClient()
    clusterLoadChecker := ioc.InitClusterLoadChecker(string2, client)
    executorNodePicker := ioc.InitExecutorNodePicker(client)
    scheduler := ioc.InitScheduler(string2, runner, ..., clusterLoadChecker, executorNodePicker)
    // ...
}
```

---

## 8. 配置完整参考

```yaml
# config/config.yaml

prometheus:
  url: "http://localhost:9090"

# 智能调度配置
intelligentScheduling:
  jobName: "executors"           # Prometheus 中执行节点的 job 名称
  topNCandidates: 3              # 从前 N 个候选节点中随机选择
  timeWindow: 30000000000        # 30s — 指标查询时间窗口
  queryTimeout: 5000000000       # 5s  — Prometheus 查询超时

# 负载检查器配置
loadChecker:
  limiter:
    rateLimit: 10.0              # 每秒允许 10 次调度
    burstSize: 20                # 突发容量 20
    waitDuration: 5000000000     # 5s  — 限流时最小等待
  database:
    threshold: 100000000         # 100ms — DB 响应时间阈值
    timeWindow: 300000000000     # 5m  — 查询时间窗口
    backoffDuration: 10000000000 # 10s — 负载过高时退避
  cluster:
    thresholdRatio: 1.2          # 超过平均值 20% 触发降频
    timeWindow: 300000000000     # 5m  — 查询时间窗口
    slowdownMultiplier: 2.0      # 降频乘数，退避时间 = 当前耗时 × 2
    minBackoffDuration: 5000000000 # 5s — 最小退避时间
```

Docker 环境的差异（`config/docker-config.yaml`）：

```yaml
prometheus:
  url: "http://prometheus:9090"  # Docker 内部网络地址（vs localhost:9090）
```

---

## 9. Docker Compose 集成

### 9.1 Prometheus 服务

```yaml
# docker-compose.yml

prometheus:
  image: prom/prometheus:latest
  container_name: task-prometheus
  user: root
  volumes:
    - ./scripts/prometheus/prometheus.yml:/etc/prometheus/prometheus.yml
    - prometheus-data:/prometheus
  ports:
    - "9090:9090"
  command:
    - "--web.enable-remote-write-receiver"  # 开启 Remote Write API
    - "--config.file=/etc/prometheus/prometheus.yml"
  networks:
    - task-network
```

关键配置：`--web.enable-remote-write-receiver` 开启 `/api/v1/write` 端点，允许执行器节点通过 Remote Write 推送指标。

### 9.2 Prometheus 采集配置

```yaml
# scripts/prometheus/prometheus.yml

global:
  scrape_interval: 15s
  evaluation_interval: 15s

scrape_configs:
  - job_name: 'prometheus'
    static_configs:
      - targets: ['localhost:9090']
```

当前配置仅采集 Prometheus 自身指标。执行器节点通过 Remote Write（而非 scrape）推送指标，因此无需在 `scrape_configs` 中配置执行器目标。

### 9.3 Grafana 服务

```yaml
grafana:
  image: grafana/grafana-enterprise:latest
  container_name: task-grafana
  ports:
    - "3000:3000"
  environment:
    - GF_SECURITY_ADMIN_PASSWORD=123
  depends_on:
    - prometheus
```

---

## 10. 集成测试

### 10.1 Picker 集成测试

`internal/test/integration/picker/picker_test.go`（216 行）：

**测试架构**：

```
MockExecutorNode ──(Remote Write)──→ Prometheus ←──(PromQL)── Dispatcher
       ×6 个                           (Docker)
```

**测试流程**：
1. 创建 6 个 MockExecutorNode（3 个 CPU 节点 + 3 个 Memory 节点）
2. 每个节点通过 Remote Write API 每 5s 推送一次指标
3. 等待 Prometheus 抓取到 `up` 指标（45s 超时）
4. 设置不同的指标值（如 CPU: 95%, 85%, 30%）
5. 创建 Dispatcher（TopN=2）
6. 执行 20 次 Pick，验证：
   - 只选中 Top2 节点（cpu-node-01, cpu-node-02）
   - 不选中最差节点（cpu-node-03）
   - 两个 Top2 节点都被选中过（验证随机性）

**MockExecutorNode 实现**（272 行）：

```go
// Remote Write 推送逻辑
func (n *MockExecutorNode) pushMetrics() error {
    var timeSeries []prompb.TimeSeries

    // 1. up 指标（存活状态）
    timeSeries = append(timeSeries, prompb.TimeSeries{
        Labels: []prompb.Label{
            {Name: "__name__", Value: "up"},
            {Name: "job", Value: "executors"},
            {Name: "instance", Value: n.NodeID},
            {Name: "node_id", Value: n.NodeID},
        },
        Samples: []prompb.Sample{{Value: 1, Timestamp: now}},
    })

    // 2. 业务指标（CPU 或 Memory）
    // ...

    // 3. protobuf 序列化 + snappy 压缩 + HTTP POST
    data, _ := proto.Marshal(&prompb.WriteRequest{Timeseries: timeSeries})
    compressed := snappy.Encode(nil, data)
    req, _ := http.NewRequestWithContext(ctx, "POST", "http://localhost:9090/api/v1/write", bytes.NewReader(compressed))
    req.Header.Set("Content-Type", "application/x-protobuf")
    req.Header.Set("Content-Encoding", "snappy")
    req.Header.Set("X-Prometheus-Remote-Write-Version", "0.1.0")
}
```

### 10.2 测试 IOC

`internal/test/integration/ioc/scheduler.go` 中使用 `mockPicker`（始终返回空字符串，降级为轮询）替代真实的 PrometheusPicker，确保非 Picker 相关的集成测试不依赖 Prometheus。

`internal/test/integration/ioc/load_checker.go` 提供带默认值的测试初始化函数，在配置文件不存在时回退到硬编码默认值。

---

## 11. 降级策略汇总

| 场景 | 降级行为 | 原因 |
|------|---------|------|
| Prometheus 不可用 | Picker 返回 error → 使用原始 ctx → 轮询 | 宁可轮询也不能停止调度 |
| `topk` 查询返回空 | Picker 返回 "未找到可用执行节点" error → 轮询 | 可能是指标还未上报 |
| DB 负载检查查询失败 | `(0, true)` — 允许继续调度 | 宁可过载也不停摆 |
| 集群负载检查查询失败 | `(0, true)` — 允许继续调度 | 同上 |
| 当前循环无执行时间数据 | `(0, true)` — 允许继续调度 | 首次循环可能没有历史数据 |
| 指定节点 gRPC 不可用 | gRPC Balancer 返回 Unavailable error → 任务执行失败 → 进入重试补偿 | 不自动降级到其他节点，保证路由语义一致 |

---

## 12. 实现局限与改进方向

### 12.1 单指标决策

当前 Picker 每次只查询一个指标（CPU 或 Memory），不支持多维度综合评分。改进方向：引入加权评分模型（如 `score = cpu×0.5 + mem×0.3 + taskCount×0.2`），需要在 Prometheus 中做更复杂的 PromQL 计算或在应用层组合多次查询结果。

### 12.2 无自适应参数调节

TopNCandidates、thresholdRatio 等参数都是静态配置。改进方向：基于历史负载数据自动调节参数（如 TopN 随集群节点数变化，thresholdRatio 随整体负载趋势调整）。

### 12.3 Prometheus 单点

当前只有一个 Prometheus 实例。改进方向：Prometheus HA（Thanos/Cortex）或使用 VictoriaMetrics 替代。

### 12.4 Remote Write vs Scrape

执行器使用 Remote Write 推送指标，而非标准的 Pull 模式。改进方向：让执行器暴露 `/metrics` 端点，在 `prometheus.yml` 中配置 `scrape_configs`，这样 Prometheus 有更好的服务发现和采集控制。

### 12.5 CompositeChecker 与 ClusterLoadChecker 拆分

当前 `CompositeChecker`（AND: Limiter + Database）和 `ClusterLoadChecker` 分别注入。前者作为 `loadChecker` 接口，后者直接传入 `Scheduler`。改进方向：统一为一个三级 `CompositeChecker`。

---

## 13. 面试高频 Q&A

### Q1：为什么不用加权轮询而是 TopN + 随机？

**答**：核心原因是防惊群。加权轮询中权重最高的节点会被集中命中——假设 3 个节点权重 50:30:20，加权轮询会把 50% 的任务分给节点 1，但在短时间内大量任务到达时，节点 1 会被瞬间打满。TopN + 随机的语义是"从最优的 N 个节点中随机选一个"，保证只选低负载节点（TopN 筛选），同时在低负载节点间均匀分散（随机打散）。实现上也更简单，一条 `topk()` PromQL 就搞定。

### Q2：Prometheus 查询延迟对调度性能有多大影响？

**答**：查询超时设为 5s，scrape_interval 是 15s，TimeWindow 是 30s。在正常情况下，Prometheus 即时查询的延迟在 10-50ms 级别，可以忽略。如果 Prometheus 不可用或超时，Picker 会返回 error，调度器降级为轮询——不会阻塞调度。关键是 Picker 在 `newContext(task)` 中被调用，这是在拿到任务列表之后、执行之前，即使 Picker 慢了几十毫秒，也不会影响调度循环的整体吞吐。

### Q3：三级负载检查器的触发顺序和优先级？

**答**：`CompositeChecker(AND, limiter, database)` 按顺序检查，但 AND 策略会**遍历所有检查器**（不短路），取最大退避时间。具体执行链：
1. LimiterChecker：纯本地计算（令牌桶），纳秒级
2. DatabaseLoadChecker：一次 Prometheus 查询，毫秒级
3. 如果都通过（maxDuration == 0），返回 `(0, true)`
4. 如果任一不通过，返回 `(maxDuration, false)`

ClusterLoadChecker 在当前实现中作为独立参数传入 Scheduler 构造函数。如果需要统一为三级 AND，只需改 `InitLoadCompositeChecker` 的参数即可。

### Q4：如果 Prometheus 挂了，整个调度系统会怎样？

**答**：不会停止调度，全部降级为"无智能"模式：
- **Picker**：`Pick()` 返回 error → `newContext()` 回退到原始 ctx → gRPC Balancer 使用轮询
- **DatabaseLoadChecker**：查询失败 → 返回 `(0, true)` → 允许继续调度
- **ClusterLoadChecker**：查询失败 → 返回 `(0, true)` → 允许继续调度
- **LimiterChecker**：纯本地令牌桶，不依赖 Prometheus，始终工作

设计原则是"宁可过载也不停摆"——Prometheus 是锦上添花，不是必需品。

### Q5：gRPC 指定节点路由失败时为什么不自动降级到其他节点？

**答**：这是有意为之。`routingPicker` 在找不到指定节点时返回 `Unavailable` error 而不是降级到轮询，原因：
1. **语义一致性**：Picker 选了 node-01 是因为 node-01 负载最低，如果 node-01 不可用，说明集群状态已经变化，下一轮调度时 Picker 会重新查询选出新的最优节点
2. **避免雪崩**：如果自动降级到其他节点，可能把任务发到一个已经过载的节点
3. **补偿兜底**：执行失败的任务会进入 RetryCompensator 重试，此时 Picker 会基于最新指标重新选择

### Q6：GORM 插件的 before/after 回调对数据库性能有影响吗？

**答**：影响极小。before 回调只做 `time.Now()` + `db.Set()`（两次 map 写入），after 回调做 `time.Since()` + `Observe()`（一次 Histogram 写入）。这些都是内存操作，开销在微秒级。Histogram 的 `Observe()` 使用了 Prometheus 客户端库的原子操作，没有锁竞争。真正的性能热点是 SQL 查询本身（毫秒级），回调开销可以忽略。

---

## 14. 关键源码索引

| 组件 | 文件路径 | 行数 | 说明 |
|------|---------|------|------|
| ExecutorNodePicker 接口 | `internal/service/picker/types.go` | 59 | 接口 + Config + 默认值 |
| prometheusPicker | `internal/service/picker/prometheus_picker.go` | 103 | PromQL 查询 + TopN + 随机 |
| Dispatcher | `internal/service/picker/dispatcher.go` | 56 | 策略分发门面 |
| LoadChecker 接口 | `pkg/loadchecker/types.go` | 36 | 接口 + Context 传递 |
| LimiterChecker | `pkg/loadchecker/limiter.go` | 68 | 令牌桶限流 |
| DatabaseLoadChecker | `pkg/loadchecker/database.go` | 98 | DB 响应时间检查 |
| ClusterLoadChecker | `pkg/loadchecker/cluster.go` | 141 | 集群平均对比 |
| CompositeChecker | `pkg/loadchecker/composite.go` | 95 | AND/OR 组合策略 |
| SchedulerMetrics | `pkg/prometheus/scheduler_metrics.go` | 88 | 调度器 Prometheus 指标 |
| GormMetricsPlugin | `pkg/prometheus/gorm_plugin.go` | 176 | GORM 数据库指标插件 |
| routingPicker | `pkg/grpc/balancer/routing_picker.go` | 95 | gRPC 路由式负载均衡 |
| Balancer Context | `pkg/grpc/balancer/types.go` | 44 | Specific/Exclude NodeID |
| Scheduler V1 | `internal/service/scheduler/scheduler.go` | 259 | 完整集成 Picker + LoadChecker |
| Scheduler V2 | `internal/service/scheduler/schedulerv2.go` | 236 | V2 同样集成 Picker + LoadChecker |
| IOC Prometheus | `ioc/prometheus.go` | 32 | Prometheus 客户端初始化 |
| IOC Picker | `ioc/picker.go` | 24 | Picker 初始化 |
| IOC LoadChecker | `ioc/load_checker.go` | 49 | 三级检查器初始化 |
| IOC Scheduler | `ioc/scheduler.go` | 61 | 调度器装配 |
| IOC DB (GORM Plugin) | `ioc/db.go` | 71 | 注册 GORM Prometheus 插件 |
| Wire 生产 | `cmd/scheduler/ioc/wire.go` | 124 | 完整依赖注入定义 |
| Wire 生成 | `cmd/scheduler/ioc/wire_gen.go` | 78 | 自动生成的装配代码 |
| Prometheus 配置 | `scripts/prometheus/prometheus.yml` | 10 | 采集配置 |
| Docker Compose | `docker-compose.yml` | 176 | Prometheus + Grafana 服务 |
| Picker 集成测试 | `internal/test/integration/picker/picker_test.go` | 216 | TopN 验证 + 随机性验证 |
| Mock 执行器节点 | `internal/test/integration/picker/mock_executor_node.go` | 272 | Remote Write 模拟推送 |
| 测试 IOC Scheduler | `internal/test/integration/ioc/scheduler.go` | 81 | mockPicker 降级测试 |
| 测试 IOC LoadChecker | `internal/test/integration/ioc/load_checker.go` | 81 | 带默认值的测试初始化 |
