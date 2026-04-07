# 分布式任务调度平台 — 优化点深度分析（面试导向）

> 本文档梳理项目中可以进一步优化的点，按 **架构设计 → 并发安全 → 可靠性 → 性能 → 可观测性 → 工程化** 六个维度组织，每个点都附带 **现状 → 问题 → 优化方案 → 面试话术** 四段式分析。

---

## 一、架构设计层面

### 1.1 V2 分布式调度未接入生产 Wire

**现状**：`ioc/wire.go` 中只组装了 V1 调度器和补偿器；V2 的 SchedulerV2 和 4 个 V2 补偿器虽然代码完整，但未通过 Wire 注入到 `SchedulerApp`，仅在集成测试中使用。

**问题**：V2 是项目的核心亮点（分库分表 + ShardingLoopJob），却无法在生产环境跑起来。

**优化方案**：
```
方案一（灰度切换）：Feature Flag 控制 V1/V2 切入
  - 配置项 scheduler.version = "v1" | "v2"
  - Wire 中根据配置选择 Provider Set
  
方案二（双写过渡）：
  1. V1 写入 + V2 影子读（对比结果一致性）
  2. V2 写入 + V1 降级
  3. 全量 V2
```

**面试话术**：*"V2 代码完整但有意不急于接入生产，是因为分库分表切换涉及数据迁移和一致性验证。我的设计是先通过集成测试验证 V2 DAO 和 ShardingLoopJob 的正确性，再通过 Feature Flag 灰度切入，最终通过双写对比确认一致后全量切换。"*

---

### 1.2 缺少 Admin/API 层

**现状**：整个系统只有 gRPC Server（executor/reporter），没有 HTTP API 层。任务创建靠直接写 DB 或 example 代码。

**问题**：无法运维级别管理任务（CRUD、手动触发、暂停/恢复、查看执行历史）。

**优化方案**：
```
增加 RESTful API 层：
  - POST   /api/tasks              — 创建任务
  - GET    /api/tasks/:id          — 查询任务
  - PUT    /api/tasks/:id/pause    — 暂停
  - PUT    /api/tasks/:id/resume   — 恢复
  - POST   /api/tasks/:id/trigger  — 手动触发
  - GET    /api/tasks/:id/executions — 执行历史
  
技术选择：gin/echo + protobuf-json 双序列化
```

**面试话术**：*"当前聚焦在调度引擎层，API 层故意后置。但如果要生产化，我会加 RESTful + WebSocket 层，WebSocket 用于实时推送任务状态变更，比如 DAG 执行进度的实时可视化。"*

---

### 1.3 任务优先级队列缺失

**现状**：调度循环 `scheduleLoop` 里 `FindSchedulableTasks` 按 `next_schedule_time ASC` 排序，所有任务一视同仁。

**问题**：高优任务（如告警处理、数据修复）无法优先调度，可能被大量低优任务阻塞。

**优化方案**：
```go
// Domain 层增加 Priority 字段
type Task struct {
    Priority     int  // 0=LOW, 5=NORMAL, 10=HIGH, 15=CRITICAL
    // ...
}

// SQL 改为优先级 + 时间双维排序
SELECT * FROM tasks 
WHERE status = 'WAITING' AND next_schedule_time <= NOW()
ORDER BY priority DESC, next_schedule_time ASC 
LIMIT ?

// 高级方案：多级队列
// CRITICAL 队列独立扫描，不受限流器约束
// HIGH 队列 2x 令牌权重
// NORMAL/LOW 队列共享剩余令牌
```

**面试话术**：*"优先级设计有两层考虑：一是 SQL 层面用 priority DESC + next_schedule_time ASC 双维排序，保证高优先调度；二是在 LoadChecker 层面做差异化限流——CRITICAL 任务不受令牌桶约束，避免系统过载时关键任务被误限。"*

---

### 1.4 DAG 工作流缺少缓存与优化

**现状**：`PlanService.getPlanData()` 每次执行 DAG 任务时都重新查询 4 个数据源（task + plan + 关系 + 已有执行），再用 ANTLR4 重新解析 DSL → 构建 DAG。

**问题**：DAG 定义是静态的，每次重复解析浪费 CPU；4 次 DB 查询增加延迟。

**优化方案**：
```go
// 方案一：本地缓存 + 版本号失效
type PlanCache struct {
    mu    sync.RWMutex
    cache map[int64]*CachedPlan  // taskID → cached
}

type CachedPlan struct {
    version  int64      // task.version
    planTask *PlanTask
}

// Task 表增加 plan_version 字段，DSL 变更时递增
// getPlanData() 先查缓存，version 一致直接返回

// 方案二：Redis 分布式缓存 + Task 更新时主动失效
```

**面试话术**：*"ANTLR4 解析本身不慢（微秒级），但 4 次 DB 查询是瓶颈。我会加两层缓存：进程内 LRU 缓存 DAG 结构（按 taskID+version 做 key），Redis 缓存跨进程共享。DAG 定义变更时通过 version 号主动失效，不用 TTL 被动过期。"*

---

### 1.5 缺少熔断降级机制

**现状**：调度器直接调用 Prometheus、MySQL、Redis、Kafka，任何一个外部依赖故障都可能拖慢整个调度循环。虽然有降级策略（Prometheus 挂了允许继续调度），但没有系统性的熔断。

**优化方案**：
```go
// 引入 Circuit Breaker（如 sony/gobreaker）
cb := gobreaker.NewCircuitBreaker(gobreaker.Settings{
    Name:        "prometheus-picker",
    MaxRequests: 5,                // 半开状态允许 5 次探测
    Interval:    60 * time.Second, // 统计窗口
    Timeout:     30 * time.Second, // 熔断后 30s 尝试恢复
    ReadyToTrip: func(counts gobreaker.Counts) bool {
        return counts.ConsecutiveFailures > 3
    },
})

// 包装 PrometheusPicker
func (p *prometheusPicker) Pick(ctx context.Context, ...) (string, error) {
    result, err := p.cb.Execute(func() (interface{}, error) {
        return p.doQuery(ctx)
    })
    if err != nil {
        return p.fallbackRoundRobin()  // 降级为轮询
    }
    return result.(string), nil
}
```

**面试话术**：*"我在 LoadChecker 里实现了简单降级（查询失败就放行），但这不够系统化。生产环境我会引入 Circuit Breaker 做三态熔断（Closed → Open → Half-Open），对 Prometheus、MySQL 查询都加熔断器，避免级联故障。关键是 fallback 策略要有梯度——Prometheus 降级用轮询，MySQL 降级用最近一次快照。"*

---

## 二、并发安全层面

### 2.1 V2 SchedulerV2 缺少 WaitGroup 优雅停机

**现状**：
```go
// V1 Scheduler — 有 WaitGroup
func (s *Scheduler) Start(ctx context.Context) error {
    s.wg.Add(1)
    go func() {
        defer s.wg.Done()
        s.scheduleLoop(ctx)
    }()
    return nil
}
func (s *Scheduler) GracefulStop() {
    s.cancel()
    s.wg.Wait()  // ✅ 等待所有 goroutine 完成
}

// V2 SchedulerV2 — 只有 cancel()
func (s *SchedulerV2) GracefulStop() {
    s.cancel()
    // ❌ 没有 Wait，goroutine 可能还在执行
}
```

**问题**：V2 的 `GracefulStop()` 只调用 `cancel()`，不等待 `Start()` 中启动的多个 goroutine 完成。可能导致正在执行的任务被强制中断，数据库状态不一致。

**优化方案**：
```go
func (s *SchedulerV2) Start(ctx context.Context) error {
    s.wg.Add(2)
    go func() {
        defer s.wg.Done()
        s.slj.Run(ctx, s.scheduleOneLoop)
    }()
    go func() {
        defer s.wg.Done()
        s.shardingSlj.Run(ctx, s.shardingScheduleOneLoop)
    }()
    return nil
}

func (s *SchedulerV2) GracefulStop() {
    s.cancel()
    
    // 带超时等待
    done := make(chan struct{})
    go func() { s.wg.Wait(); close(done) }()
    select {
    case <-done:
        s.logger.Info("all loops stopped gracefully")
    case <-time.After(30 * time.Second):
        s.logger.Warn("graceful stop timed out")
    }
}
```

**面试话术**：*"V1 用了 WaitGroup + Wait() 做优雅停机，V2 我漏掉了。这个 bug 的影响是：cancel 发出后 goroutine 可能还在执行 scheduleOneLoop 的某一步（比如正在抢占任务），如果进程直接退出，这个执行记录会卡在 RUNNING 状态，需要 InterruptCompensator 来兜底。修复方案是 WaitGroup + 带超时的 Wait。"*

---

### 2.2 ClientsV2 连接竞态

**现状**（`pkg/grpc/clients_v2.go`）：
```go
func (c *ClientsV2[T]) getOrCreate(addr string) (T, error) {
    c.mu.RLock()
    if client, ok := c.clients[addr]; ok {
        c.mu.RUnlock()
        return client, nil
    }
    c.mu.RUnlock()  // ← 释放读锁

    // ⚠️ 此处多个 goroutine 可能同时到达
    conn, err := grpc.DialContext(...)  // 创建连接
    
    c.mu.Lock()
    if existing, ok := c.clients[addr]; ok {
        c.mu.Unlock()
        conn.Close()  // 关闭重复连接
        return existing, nil
    }
    // ...
}
```

**问题**：Double-Check Locking 本身正确，但多个 goroutine 可能同时创建连接（虽然只保留一个）。高并发下白白浪费连接资源和 TCP 握手时间。

**优化方案**：
```go
import "golang.org/x/sync/singleflight"

type ClientsV2[T any] struct {
    clients map[string]T
    mu      sync.RWMutex
    sf      singleflight.Group  // 保证相同 addr 只创建一次
    creator func(conn *grpc.ClientConn) T
}

func (c *ClientsV2[T]) getOrCreate(addr string) (T, error) {
    c.mu.RLock()
    if client, ok := c.clients[addr]; ok {
        c.mu.RUnlock()
        return client, nil
    }
    c.mu.RUnlock()

    // singleflight 合并并发请求
    result, err, _ := c.sf.Do(addr, func() (interface{}, error) {
        conn, err := grpc.DialContext(...)
        if err != nil {
            return nil, err
        }
        client := c.creator(conn)
        c.mu.Lock()
        c.clients[addr] = client
        c.mu.Unlock()
        return client, nil
    })
    // ...
}
```

**面试话术**：*"原来的实现是 Double-Check Locking，正确性没问题，但效率有问题——10 个 goroutine 同时请求同一个新节点的连接，会建 10 条 TCP 然后丢掉 9 条。用 singleflight 可以把这 10 次请求合并成 1 次，其余 9 个等结果就行。这是典型的缓存击穿优化思路。"*

---

### 2.3 ClientsV2 缺少 Close 方法（连接泄漏）

**现状**：`ClientsV2` 没有 `Close()` 方法，进程停止时 gRPC 连接不会被主动关闭。

**优化方案**：
```go
type connEntry[T any] struct {
    client T
    conn   *grpc.ClientConn
}

func (c *ClientsV2[T]) Close() error {
    c.mu.Lock()
    defer c.mu.Unlock()
    var errs []error
    for addr, entry := range c.entries {
        if err := entry.conn.Close(); err != nil {
            errs = append(errs, fmt.Errorf("close %s: %w", addr, err))
        }
    }
    c.entries = make(map[string]connEntry[T])
    return errors.Join(errs...)
}
```

**面试话术**：*"资源泄漏是 Go 项目的常见问题。除了 Close()，还需要在 SchedulerApp.GracefulStop() 里按依赖逆序关闭资源：先停调度循环 → 等待执行中任务完成 → 关闭 gRPC 连接 → 关闭 DB 连接池。"*

---

### 2.4 ExecutionService.UpdateState 的 TOCTOU 竞态

**现状**（`internal/service/task/execution_service.go`）：
```go
func (s *executionService) UpdateState(ctx context.Context, exec TaskExecution) error {
    // Step 1: 先查当前状态
    old, err := s.repo.FindByID(ctx, exec.ID)
    
    // Step 2: 校验状态转换合法性
    if !old.Status.CanTransitionTo(exec.Status) {
        return ErrInvalidStateTransition
    }
    
    // Step 3: 更新
    return s.repo.Update(ctx, exec)
}
```

**问题**：Step 1 和 Step 3 之间，其他 goroutine/节点可能已经修改了状态。经典的 TOCTOU（Time-of-Check-Time-of-Use）问题。

**优化方案**：
```go
// 方案一：CAS 更新（推荐，与任务抢占保持一致）
func (s *executionService) UpdateState(ctx context.Context, exec TaskExecution) error {
    affected, err := s.repo.UpdateWithCAS(ctx, exec.ID, exec.Status, exec.OldStatus)
    if err != nil {
        return err
    }
    if affected == 0 {
        return ErrConcurrentStateUpdate
    }
    return nil
}

// SQL: UPDATE task_executions 
//      SET status = ?, version = version + 1 
//      WHERE id = ? AND status = ? AND version = ?

// 方案二：状态机 + 数据库事务
// 但 CAS 更轻量，与项目现有风格一致
```

**面试话术**：*"状态更新用了先查后改的模式，存在 TOCTOU 风险。项目里任务抢占已经用了 CAS（WHERE version = ?），状态变更也应该统一用 CAS。好处是无锁、高性能，而且状态机的合法转换检查可以下推到 SQL 的 WHERE 条件里。"*

---

### 2.5 NormalTaskRunner goroutine 泄漏风险

**现状**（`internal/service/runner/normal_task_runner.go`）：
```go
func (r *NormalTaskRunner) handleNormalTask(ctx context.Context, t domain.Task, exec domain.TaskExecution) {
    go func() {  // ← 无限制地启动 goroutine
        // ... 可能长时间运行
        r.invoker.Invoke(ctx, exec)
    }()
}
```

**问题**：
1. 没有 goroutine 数量限制，大量任务同时调度可能耗尽内存
2. 没有跟踪机制（WaitGroup/errgroup），优雅停机时无法等待

**优化方案**：
```go
// 使用 worker pool 限制并发
type NormalTaskRunner struct {
    pool *ants.Pool  // 或自实现的 worker pool
    wg   sync.WaitGroup
}

func NewNormalTaskRunner(maxWorkers int) *NormalTaskRunner {
    pool, _ := ants.NewPool(maxWorkers, ants.WithPreAlloc(true))
    return &NormalTaskRunner{pool: pool}
}

func (r *NormalTaskRunner) handleNormalTask(ctx context.Context, ...) {
    r.wg.Add(1)
    r.pool.Submit(func() {
        defer r.wg.Done()
        r.invoker.Invoke(ctx, exec)
    })
}

func (r *NormalTaskRunner) GracefulStop() {
    r.wg.Wait()
    r.pool.Release()
}
```

**面试话术**：*"Runner 里直接 go func() 有两个问题：一是并发数不可控，可能 OOM；二是优雅停机时感知不到这些 goroutine。我会用 worker pool 限制并发（比如 CPU 核数 × 2），加 WaitGroup 做优雅等待。这个设计也便于后续加 goroutine 级别的 metrics 监控。"*

---

## 三、可靠性层面

### 3.1 Kafka 事件发送无重试

**现状**（`internal/service/task/execution_service.go`）：
```go
func (s *executionService) sendCompletedEvent(ctx context.Context, exec TaskExecution) {
    err := s.producer.ProduceTaskCompleteEvent(ctx, event)
    if err != nil {
        s.logger.Error("send complete event failed", elog.FieldErr(err))
        // ← 只是打日志，不重试
    }
}
```

**问题**：DAG 工作流依赖 complete event 驱动 NextStep。事件丢失 = DAG 卡死。

**优化方案**：
```
方案一（Outbox Pattern，推荐）：
  1. 任务状态变更 + 写 outbox 表放在同一个数据库事务
  2. 独立 goroutine 轮询 outbox 表，发送到 Kafka
  3. 发送成功后标记 outbox 记录为已发送
  → 保证事件至少一次投递，幂等由消费端保证

方案二（本地重试 + DLQ）：
  1. 发送失败时指数退避重试 3 次
  2. 仍失败写入 Dead Letter Queue
  3. DLQ 有独立消费者做延迟重投
```

**面试话术**：*"当前实现的最大可靠性缺口就是事件投递。Kafka 写入失败 → DAG 卡死 → 没有补偿机制。我推荐 Outbox Pattern：把事件写入和状态变更放在同一事务里，再异步投递到 Kafka。这样即使 Kafka 暂时不可用，事件也不会丢。消费端做幂等就行（ExecutionID 去重）。"*

---

### 3.2 ShardingCompensator 的 Offset 分页一致性

**现状**：ShardingCompensator V1 使用 offset 分页遍历父任务：
```go
for {
    parents := repo.FindShardingParents(ctx, offset, limit)
    if len(parents) == 0 { break }
    for _, p := range parents {
        handle(p)  // 可能改变 p 的状态，使其不再被下次查询命中
    }
    offset += limit
}
```

**问题**：`handle()` 可能把 parent 从 RUNNING 改为 SUCCESS/FAILED，导致后续页的 offset 偏移——某些 parent 可能被跳过或重复处理。

**优化方案**：
```
方案一：游标分页（推荐）
  WHERE id > ? AND status = 'RUNNING'
  ORDER BY id ASC LIMIT ?
  
  用上一页最后一条的 ID 作为游标，不受中间删改影响

方案二：快照隔离
  事务开始前拍快照 ID 列表，按 ID 列表逐批处理
  
方案三（V2 已部分解决）：
  ShardingLoopJob 按分片表遍历，每表独立 offset
  但仍有单表内的 offset 偏移问题
```

**面试话术**：*"Offset 分页在数据变动场景下有一致性问题，这是经典的翻页异常。解法是改用游标分页（WHERE id > lastID），这样不管中间有多少行被修改，游标位置始终稳定。V2 的 per-table offset 缓解了跨表问题，但单表内仍需改为游标。"*

---

### 3.3 Plan 错误处理 Bug

**现状**（`internal/service/task/plan.go`）：
```go
func (p *PlanTask) getPlanData(...) error {
    // ...
    eerr := p.buildDAG(dslExpression)
    if eerr != nil {
        return err  // ← Bug：返回的是外层的 err（可能是 nil），不是 eerr
    }
}
```

**问题**：变量名 typo，`eerr` 错误被吞掉，DAG 构建失败不会上报。

**优化方案**：直接修复 `return err` → `return eerr`。并建议启用 `go vet` / `staticcheck` 的未使用变量检查。

**面试话术**：*"这是一个变量名拼写导致的 silent failure，错误被吞了。防御措施是 CI 里开启 staticcheck 的 SA4006（变量赋值后未使用），这类问题在编译阶段就能发现。"*

---

### 3.4 panic("implement me") 残留

**现状**（`internal/repository/dao/sharding_task_execution.go`）：
```go
func (dao *ShardingTaskExecutionDAO) FindExecutionByTaskIDAndPlanExecID(...) {
    panic("implement me")
}
```

**问题**：生产代码里有 panic，一旦被调用就会导致整个进程崩溃。

**优化方案**：要么实现它，要么返回 `ErrNotImplemented` 错误。更深层的改进是在接口定义时用编译期检查确保实现完整性。

---

## 四、性能层面

### 4.1 PlanTask 线性搜索 O(n)

**现状**（`internal/service/task/plan.go`）：
```go
func (p *PlanTask) GetTask(taskID int64) *PlanNode {
    for _, node := range p.Nodes {  // ← O(n) 遍历
        if node.TaskID == taskID {
            return node
        }
    }
    return nil
}
```

**问题**：每次查找 DAG 节点都是线性扫描。DAG 节点数多时性能差，而且 `NextStep` 和 `CheckPre` 频繁调用此方法。

**优化方案**：
```go
type PlanTask struct {
    Nodes   []*PlanNode
    nodeMap map[int64]*PlanNode  // ← 增加索引
}

func (p *PlanTask) buildIndex() {
    p.nodeMap = make(map[int64]*PlanNode, len(p.Nodes))
    for _, n := range p.Nodes {
        p.nodeMap[n.TaskID] = n
    }
}

func (p *PlanTask) GetTask(taskID int64) *PlanNode {
    return p.nodeMap[taskID]  // O(1)
}
```

**面试话术**：*"DAG 节点查找从 O(n) 优化到 O(1)。虽然单个 DAG 节点数通常不多（几十个），但 NextStep 在事件驱动下可能被高频调用，而且 CheckPre 又会递归查前驱节点，累积效应不可忽视。用 map 索引是最简单有效的优化。"*

---

### 4.2 重试策略对象每次创建

**现状**（`internal/service/task/execution_service.go`）：
```go
func (s *executionService) updateRetryState(ctx context.Context, exec TaskExecution) error {
    strategy := retry.NewExponentialBackoff(exec.RetryConfig)  // ← 每次调用都创建
    // ...
}
```

**优化方案**：
```go
// 方案一：缓存到 TaskExecution 领域对象
type TaskExecution struct {
    retryStrategy *retry.Strategy  // lazy init
}

// 方案二：sync.Pool
var strategyPool = sync.Pool{
    New: func() any { return &retry.ExponentialBackoff{} },
}
```

---

### 4.3 Resolver 全量拉取

**现状**（`pkg/grpc/resolver.go`）：
```go
func (r *etcdResolver) resolve() {
    // 每次有 etcd Watch 事件，都全量拉取所有节点
    instances, _ := r.registry.ListInstances(ctx)
    addrs := make([]resolver.Address, 0, len(instances))
    for _, inst := range instances {
        addrs = append(addrs, resolver.Address{Addr: inst.Addr})
    }
    r.cc.UpdateState(resolver.State{Addresses: addrs})
}
```

**问题**：每次节点变更事件（加入/离开），都全量拉取所有节点列表。集群规模大时效率低。

**优化方案**：
```go
// 增量更新：维护本地节点集合，根据事件类型 add/remove
func (r *etcdResolver) handleEvent(event registry.Event) {
    switch event.Type {
    case registry.EventAdd:
        r.addrs[event.Instance.Addr] = struct{}{}
    case registry.EventDelete:
        delete(r.addrs, event.Instance.Addr)
    }
    r.cc.UpdateState(r.buildState())
}
```

**面试话术**：*"当前 Resolver 每次收到 Watch 事件都全量拉取，是因为 etcd Subscribe 的 Event 只有 Type 没有 Instance 信息（这是 registry 接口的设计缺陷）。修复分两步：一是 Event 结构加 Instance 字段，二是 Resolver 维护本地集合做增量更新。集群 100 节点以下全量拉取其实问题不大，但架构上应该是增量的。"*

---

### 4.4 ResourceSemaphore 潜在下溢

**现状**（`pkg/loopjob/resource.go`）：
```go
func (r *MaxCntResourceSemaphore) Release() {
    r.mu.Lock()
    r.curCount--  // ← 没有检查是否 < 0
    r.mu.Unlock()
}
```

**优化方案**：
```go
func (r *MaxCntResourceSemaphore) Release() {
    r.mu.Lock()
    defer r.mu.Unlock()
    if r.curCount <= 0 {
        // log warning: double release detected
        return
    }
    r.curCount--
}
```

---

## 五、可观测性层面

### 5.1 自定义指标太少

**现状**：只有 3 个自定义 Prometheus 指标：
- `scheduler_loop_duration_seconds` — 调度循环耗时
- `load_checker_sleep_duration_seconds` — 负载检查退避时长
- `gorm_query_duration_seconds` — 数据库查询耗时

**缺失的关键指标**：
```
业务层：
  - task_schedule_total{status="success|failed|skipped"}  — 任务调度计数
  - task_execution_duration_seconds{task_type="..."}       — 按任务类型的执行耗时直方图
  - task_queue_depth                                        — 待调度任务积压量
  - task_retry_total{task_id="..."}                        — 重试次数分布
  - dag_step_duration_seconds{plan_id="..."}               — DAG 各步骤耗时

基础设施层：
  - grpc_client_connection_count                            — gRPC 连接池大小
  - compensator_scan_total{type="retry|reschedule|..."}    — 补偿器扫描次数
  - sharding_loop_duration_seconds{table="..."}            — 分片循环耗时
```

**面试话术**：*"可观测性的黄金三角是 Metrics + Logging + Tracing。当前只有 Metrics 且只有 3 个指标，远不够生产级别。我会补充任务维度的 RED 指标（Rate、Error、Duration），加上 OpenTelemetry 做分布式链路追踪，串联调度器 → gRPC → 执行器 → Reporter 的完整链路。"*

---

### 5.2 缺少告警规则

**现状**：有 Prometheus 和 Grafana，但没有告警规则。

**优化方案**：
```yaml
# prometheus/alerting_rules.yml
groups:
  - name: scheduler_alerts
    rules:
      - alert: ScheduleLoopSlow
        expr: histogram_quantile(0.99, scheduler_loop_duration_seconds) > 5
        for: 5m
        labels: { severity: warning }
        annotations:
          summary: "调度循环 P99 延迟超过 5s"
          
      - alert: TaskQueueBacklog
        expr: task_queue_depth > 1000
        for: 10m
        labels: { severity: critical }
        annotations:
          summary: "待调度任务积压超过 1000"
          
      - alert: CompensatorHighRetryRate
        expr: rate(task_retry_total[5m]) > 10
        for: 5m
        labels: { severity: warning }
        annotations:
          summary: "任务重试率异常升高"
```

---

### 5.3 缺少分布式链路追踪

**现状**：没有 tracing 集成。

**优化方案**：
```
引入 OpenTelemetry：
  - gRPC interceptor 自动注入 trace context
  - GORM plugin 记录 SQL span
  - Kafka producer/consumer 传播 trace header
  - 调度器 scheduleLoop 作为 root span
  
部署 Jaeger/Tempo 存储 trace 数据
```

**面试话术**：*"分布式系统最头疼的是排查跨节点问题。比如一个 DAG 任务卡住了，需要从调度器追踪到具体哪个执行节点、哪个 gRPC 调用超时。OpenTelemetry 可以串联整条链路，而且 gRPC 和 GORM 都有现成的 instrumentation，接入成本很低。"*

---

## 六、工程化层面

### 6.1 测试覆盖不足

**现状**：
- 有 DAO 层集成测试（使用真实 MySQL）
- 有 E2E 测试（example/）
- **缺少**：单元测试（service 层、domain 层的纯逻辑测试）

**优化方案**：
```
优先补充的测试：
  1. 状态机测试：CanTransitionTo() 的所有合法/非法转换路径
  2. 重试策略测试：指数退避的边界情况（溢出、maxInterval）
  3. DAG 构建测试：各种 DSL 表达式 → PlanTask 的正确性
  4. CompositeChecker 测试：AND/OR 组合的各种场景
  5. ShardingStrategy 测试：哈希分布均匀性
  
测试框架：testify + gomock
```

**面试话术**：*"当前测试集中在 DAO 集成测试和 E2E，缺少 service 层的单元测试。单元测试的 ROI 最高——跑得快、能覆盖边界情况、易于维护。我会优先补状态机和重试策略的测试，因为这两个模块的 bug 影响面最大。"*

---

### 6.2 配置值可读性差

**现状**（`config/config.yaml`）：
```yaml
loadChecker:
  limiter:
    interval: 10000000000    # ← 10s，但看起来像天文数字
  database:
    interval: 60000000000    # ← 60s
    threshold: 200000000     # ← 200ms
```

**问题**：Go 的 `time.Duration` 默认单位是纳秒，YAML 里直接写纳秒值极不可读。

**优化方案**：
```go
// 自定义 Duration 类型，支持 "10s"、"200ms" 格式
type Duration struct {
    time.Duration
}

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
    duration, err := time.ParseDuration(value.Value)
    if err != nil {
        return err
    }
    d.Duration = duration
    return nil
}

// config.yaml 变为：
// loadChecker:
//   limiter:
//     interval: "10s"
//   database:
//     interval: "60s"
//     threshold: "200ms"
```

---

### 6.3 V1/V2 代码重复

**现状**：4 个补偿器各有 V1 和 V2 版本，代码标注 `//nolint:dupl`。核心逻辑相同，只是循环框架不同（for-loop vs ShardingLoopJob）。

**优化方案**：
```go
// 策略模式：提取公共逻辑为接口
type CompensationLogic interface {
    FindCandidates(ctx context.Context, offset, limit int) ([]domain.TaskExecution, error)
    Handle(ctx context.Context, exec domain.TaskExecution) error
}

// V1: 用 for-loop 驱动 CompensationLogic
// V2: 用 ShardingLoopJob 驱动 CompensationLogic
// 公共逻辑只写一份
```

**面试话术**：*"V1 和 V2 补偿器的区别只在循环框架——V1 是简单 for-loop + sleep，V2 是 ShardingLoopJob + 分布式锁。核心的扫描逻辑和处理逻辑完全一样，但被复制了一遍。我会用策略模式把循环驱动和业务逻辑解耦，公共部分只写一份。"*

---

### 6.4 CompositeChecker OR 逻辑 Bug

**现状**（`pkg/loadchecker/composite.go`）：
```go
func (c *CompositeChecker) checkOR(ctx context.Context) (bool, time.Duration) {
    allFailed := true
    minDuration := time.Hour
    for _, checker := range c.checkers {
        pass, duration := checker.Check(ctx)
        if pass {
            return true, 0  // 短路返回 ✅
        }
        allFailed = false     // ← Bug：失败时设为 false
        if duration < minDuration {
            minDuration = duration
        }
    }
    if allFailed && minDuration == time.Hour {
        // ...
    }
    return false, minDuration
}
```

**问题**：`allFailed` 在 checker 失败（`pass = false`）时被设为 `false`，这意味着只要有任何一个 checker 被评估，`allFailed` 就是 `false`。逻辑与变量名语义矛盾——应该是 `allFailed = true` 保持不变（因为确实失败了），或者变量名改为 `anyChecked`。

**面试话术**：*"这是个语义 bug：变量叫 allFailed 但逻辑写反了。实际上 OR 的含义是'任一通过就通过'，短路返回已经处理了。剩余的 allFailed 分支本意是处理'所有 checker 都没返回有效退避时间'的边界情况，但变量赋值写反导致这个分支永远不会执行。"*

---

### 6.5 Docker Compose 健康检查不完整

**现状**：
```yaml
etcd:
  depends_on:
    - mysql-setup  # ← service_started，不是 service_healthy

redis:
  # ← 没有 healthcheck
```

**优化方案**：
```yaml
etcd:
  healthcheck:
    test: ["CMD", "etcdctl", "endpoint", "health"]
    interval: 10s
    timeout: 5s
    retries: 3

redis:
  healthcheck:
    test: ["CMD", "redis-cli", "ping"]
    interval: 10s
    timeout: 5s
    retries: 3

scheduler:
  depends_on:
    mysql:
      condition: service_healthy
    redis:
      condition: service_healthy
    etcd:
      condition: service_healthy
    kafka:
      condition: service_healthy
```

---

### 6.6 缺少 CI/CD Pipeline

**优化方案**：
```yaml
# .github/workflows/ci.yml
name: CI
on: [push, pull_request]
jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: golangci/golangci-lint-action@v3
  
  unit-test:
    runs-on: ubuntu-latest
    steps:
      - run: go test -race -coverprofile=coverage.out ./...
      - uses: codecov/codecov-action@v3
  
  integration-test:
    runs-on: ubuntu-latest
    services:
      mysql: { image: mysql:8.0 }
      redis: { image: redis:7 }
    steps:
      - run: go test -tags=integration ./...
  
  build:
    runs-on: ubuntu-latest
    steps:
      - run: docker build -t scheduler .
```

---

## 七、面试总结话术

> **"如果让我重新设计这个系统，我会重点改进三个方面：**
> 
> 1. **可靠性**：引入 Outbox Pattern 保证事件至少一次投递，用 CAS 统一状态变更的并发控制，加 Circuit Breaker 防止级联故障。
> 
> 2. **可观测性**：补充 RED 指标 + OpenTelemetry 链路追踪 + 告警规则，让系统'可诊断'。
> 
> 3. **工程化**：CI/CD pipeline + 单元测试覆盖 + 配置可读性改造 + V1/V2 代码去重。
> 
> **这些不是事后诸葛亮——项目 docs 里每个 Step 都记录了'实现局限与改进方向'，说明我在设计时就知道取舍了什么。面试项目追求的是核心设计的深度，不是生产级的完备性。"**

---

## 附：优化点速查表

| # | 维度 | 优化点 | 优先级 | 面试加分度 |
|---|------|--------|--------|-----------|
| 1.1 | 架构 | V2 未接入生产 Wire | P1 | ⭐⭐⭐⭐⭐ |
| 1.2 | 架构 | 缺少 Admin API 层 | P2 | ⭐⭐⭐ |
| 1.3 | 架构 | 任务优先级队列 | P2 | ⭐⭐⭐⭐ |
| 1.4 | 架构 | DAG 缓存缺失 | P2 | ⭐⭐⭐ |
| 1.5 | 架构 | 缺少熔断降级 | P1 | ⭐⭐⭐⭐⭐ |
| 2.1 | 并发 | V2 缺少 WaitGroup 停机 | P0 | ⭐⭐⭐⭐⭐ |
| 2.2 | 并发 | ClientsV2 连接竞态 | P1 | ⭐⭐⭐⭐ |
| 2.3 | 并发 | ClientsV2 无 Close 方法 | P1 | ⭐⭐⭐ |
| 2.4 | 并发 | UpdateState TOCTOU | P1 | ⭐⭐⭐⭐⭐ |
| 2.5 | 并发 | goroutine 泄漏风险 | P1 | ⭐⭐⭐⭐ |
| 3.1 | 可靠 | Kafka 事件无重试 | P0 | ⭐⭐⭐⭐⭐ |
| 3.2 | 可靠 | Offset 分页一致性 | P1 | ⭐⭐⭐⭐ |
| 3.3 | 可靠 | Plan 错误处理 Bug | P0 | ⭐⭐⭐ |
| 3.4 | 可靠 | panic 残留 | P0 | ⭐⭐ |
| 4.1 | 性能 | PlanTask 线性搜索 | P2 | ⭐⭐⭐ |
| 4.2 | 性能 | 重试策略重复创建 | P3 | ⭐⭐ |
| 4.3 | 性能 | Resolver 全量拉取 | P2 | ⭐⭐⭐⭐ |
| 4.4 | 性能 | 信号量下溢 | P2 | ⭐⭐ |
| 5.1 | 观测 | 自定义指标太少 | P1 | ⭐⭐⭐⭐ |
| 5.2 | 观测 | 缺少告警规则 | P1 | ⭐⭐⭐ |
| 5.3 | 观测 | 缺少链路追踪 | P1 | ⭐⭐⭐⭐⭐ |
| 6.1 | 工程 | 测试覆盖不足 | P1 | ⭐⭐⭐⭐ |
| 6.2 | 工程 | 配置纳秒不可读 | P3 | ⭐⭐ |
| 6.3 | 工程 | V1/V2 代码重复 | P2 | ⭐⭐⭐ |
| 6.4 | 工程 | CompositeChecker OR Bug | P1 | ⭐⭐⭐ |
| 6.5 | 工程 | Docker 健康检查不全 | P2 | ⭐⭐⭐ |
| 6.6 | 工程 | 缺少 CI/CD | P1 | ⭐⭐⭐⭐ |
