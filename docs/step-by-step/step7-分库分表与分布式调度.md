# Step 7：分库分表 + 分布式调度（V2）

> **目标**：将 Step 1~6 的单库单表架构升级为**分库分表 + 多节点分布式调度**。核心改造：2 库 × 2 任务表 × 4 执行表 的分片数据布局，Snowflake ID 内嵌路由信息实现 O(1) 分片定位，ShardingLoopJob 框架驱动每个分片独立加锁、独立调度，多个调度器节点自动瓜分分片负载。
>
> **完成后你能看到**：启动 Redis + MySQL（2 个分库自动建表）→ 启动调度器节点 A → A 抢占所有分片的分布式锁开始调度 → 启动调度器节点 B → B 抢到部分分片的锁，A/B 各自只扫描自己持锁的分片 → 杀掉 A → A 的锁过期 → B 自动接管所有分片 → 写入一个分片任务 → 子任务被拆分到多个分表 → ShardingCompensatorV2 按分片扫描聚合 → 父任务标记 SUCCESS。

---

## 1. 架构总览

Step 7 从两个维度升级系统：**数据层**从单库单表变为多库多表，**调度层**从单节点 for 循环变为多节点分布式锁竞争。两层改造通过 `ShardingStrategy` 和 `ShardingLoopJob` 这两个核心组件串联。

```
┌───────────────────────────────────────────────────────────────────────┐
│                      Scheduler Node A                                 │
│                                                                       │
│  ShardingLoopJob.Run()                                                │
│  ┌─────────────────────────────────────────────────────────────┐      │
│  │  for _, dst := range strategy.Broadcast() {                 │      │
│  │      // dst = {DB: "task_0", Table: "task_0"}              │      │
│  │      //        {DB: "task_0", Table: "task_1"}              │      │
│  │      //        {DB: "task_1", Table: "task_0"}              │      │
│  │      //        {DB: "task_1", Table: "task_1"}              │      │
│  │                                                             │      │
│  │      semaphore.Acquire()  ← 并发度控制                      │      │
│  │      lock = dclient.NewLock("scheduleKey:task_0:task_0")   │      │
│  │      lock.Lock()          ← 抢不到就阻塞/等下一轮           │      │
│  │      go tableLoop(ctx, dst, lock) ─┐                       │      │
│  │  }                                  │                       │      │
│  └─────────────────────────────────────┼───────────────────────┘      │
│                                        │                              │
│  ┌─────────────────────────────────────▼───────────────────────┐      │
│  │  tableLoop(dst, lock)                                       │      │
│  │    defer semaphore.Release()                                │      │
│  │    bizLoop:                                                  │      │
│  │      ctx = CtxWithDst(ctx, dst)  ← 注入分片路由              │      │
│  │      scheduleOneLoop(ctx)        ← 只扫当前分片的 task 表   │      │
│  │      lock.Refresh()              ← 续约分布式锁              │      │
│  └─────────────────────────────────────────────────────────────┘      │
│                                                                       │
│  ┌─────────────────────────────────────────────────────────────┐      │
│  │  scheduleOneLoop(ctx)                                       │      │
│  │    dao.FindSchedulableTasks(ctx) ← 从 ctx 取 Dst → 定位表  │      │
│  │    dao.Acquire(ctx, task)        ← CAS 抢占                 │      │
│  │    runner.Run(ctx, task)         ← 执行                     │      │
│  └─────────────────────────────────────────────────────────────┘      │
└───────────────────────────────────────────────────────────────────────┘

┌───────────────────────────────────────────────────────────────────────┐
│                      Scheduler Node B                                 │
│                                                                       │
│  同样的 ShardingLoopJob.Run()                                         │
│  → 对 4 个分片尝试 lock.Lock()                                        │
│  → Node A 已持锁的分片：B 阻塞等待                                     │
│  → Node A 释放/崩溃 → 锁过期 → B 自动接管                              │
└───────────────────────────────────────────────────────────────────────┘

┌───────────────────────────────────────────────────────────────────────┐
│                         数据层                                        │
│                                                                       │
│  ┌──────────────┐    ┌──────────────┐                                │
│  │   task_0      │    │   task_1      │                                │
│  │              │    │              │                                │
│  │  task_0      │    │  task_0      │    ← 2 库 × 2 任务表 = 4 分片  │
│  │  task_1      │    │  task_1      │                                │
│  │              │    │              │                                │
│  │  task_exec_0 │    │  task_exec_0 │                                │
│  │  task_exec_1 │    │  task_exec_1 │    ← 2 库 × 4 执行表 = 8 分片  │
│  │  task_exec_2 │    │  task_exec_2 │                                │
│  │  task_exec_3 │    │  task_exec_3 │                                │
│  └──────────────┘    └──────────────┘                                │
│                                                                       │
│  路由算法：                                                            │
│    dbIdx   = bizID % 2        → 选库                                  │
│    tableIdx = (bizID / 2) % N → 选表（task N=2, execution N=4）       │
│                                                                       │
│  Snowflake ID 内嵌 bizID（12 bit）→ 从 ID 直接反推分片位置            │
└───────────────────────────────────────────────────────────────────────┘
```

---

## 2. Step 6 → Step 7 变化对比

| 维度 | Step 6 (分片任务) | Step 7 (分库分表 + 分布式调度) |
|------|-------------------|-------------------------------|
| 数据库拓扑 | 单库 `task`，单张 `task` 表 + 单张 `task_execution` 表 | 2 库 `task_0`/`task_1`，每库 2 张 task 表 + 4 张 execution 表 |
| ID 生成 | 数据库自增 | Snowflake：`bizID(12bit) \| sequence(12bit) \| timestamp(41bit)` |
| 数据路由 | 无（所有数据在一张表） | `ShardingStrategy` 两级哈希：`bizID % dbCount → 选库`，`(bizID / dbCount) % tableCount → 选表` |
| 调度器架构 | V1：单节点 `for{}` 循环全表扫描 | V2：`ShardingLoopJob` 框架，每个分片独立加锁、独立调度 |
| 多节点支持 | 不支持（全表扫描冲突） | 自动瓜分：每个分片一把分布式锁，多节点竞争 |
| 补偿器 | V1：单节点 `for{}` 循环全表扫描 | V2：全部 4 个补偿器迁移到 `ShardingLoopJob` 框架 |
| 并发控制 | 无 | `ResourceSemaphore` 限制单节点最大同时处理分片数 |
| 分布式锁 | 无 | Redis `dlock.Client`（SET NX + Lua 脚本续约） |
| DAO 层 | `GORMTaskDAO` 单表操作 | `ShardingTaskDAO` / `ShardingTaskExecutionDAO` 多库多表路由 |
| 故障恢复 | 调度器挂了就停了 | 锁过期后其他节点自动接管 |
| 新增基础设施 | 无 | Redis（分布式锁）、多数据库连接池 |

---

## 3. 核心设计决策

### 3.1 为什么选两级哈希，不用一致性哈希？

| 维度 | 两级哈希 | 一致性哈希 |
|------|---------|-----------|
| 实现复杂度 | 两行取模运算 | 需要虚拟节点、环形结构、TreeMap |
| 分片数量 | 固定（2×2 / 2×4），编译期确定 | 动态增减 |
| 扩容成本 | 需要全量数据迁移（或翻倍扩容只迁一半） | 只迁移相邻节点的数据 |
| 适用场景 | 分片数可预估、不频繁变化 | 节点动态增减（如缓存集群） |

**选择理由**：任务调度平台的分片数在部署时确定，几乎不会动态变化。两级哈希的确定性和低复杂度远优于一致性哈希的灵活性——这里不需要那种灵活性。

### 3.2 为什么 Snowflake ID 嵌入 bizID 而非 workerID？

标准 Snowflake 的 10 bit workerID 标识"哪台机器生成的"。本系统改为 12 bit bizID（业务分片 ID），理由：

- **O(1) 路由**：拿到一个 ID，直接 `ExtractShardingID(id)` 就知道它在哪个库哪张表，不需要额外查路由表
- **跨分片查询**：给定 taskID 查执行记录，从 ID 提取 shardingID 即可定位，无需扫描所有分片
- **去中心化**：不依赖机器注册，任何节点都能生成 ID，bizID 由业务逻辑决定
- **代价**：同一毫秒同一 bizID 下最多 4096 个序列号（12 bit sequence），对于任务调度场景绰绰有余

### 3.3 为什么每个分片一把锁，而非全局一把锁？

| 方案 | 优点 | 缺点 |
|------|------|------|
| 全局一把锁 | 简单，不需要分片感知 | 同一时刻只有一个节点在干活，其他节点空等 |
| 每个分片一把锁 | N 个分片 = N 路并行，多节点自动瓜分 | 锁管理复杂度 O(N) |

**选择理由**：分库分表的意义就是并行化，如果用全局锁就退化回了单节点。4 个任务分片 = 4 路并行 = 最多 4 个节点同时工作。

### 3.4 为什么 V2 组件没有接入生产 Wire？

阅读 `cmd/scheduler/ioc/wire.go` 可以发现，**生产环境仍然使用 V1 组件**（`GORMTaskDAO`、`Scheduler`、V1 补偿器）。V2 组件（`ShardingTaskDAO`、`SchedulerV2`、V2 补偿器）只在 `internal/test/integration/ioc/wire.go` 中组装。

这是**刻意的分阶段策略**：
1. V2 代码先写好、测试通过
2. 生产环境继续跑 V1，保持稳定
3. 切换时只需修改 Wire 绑定 + 数据迁移脚本
4. 回滚同理——改回 V1 Wire 即可

> 面试角度：这体现了对生产稳定性的尊重。分库分表不是写完就上线，而是「代码先行 → 集成测试 → 灰度切换」。

---

## 4. ShardingStrategy：分片路由策略

**文件**：`pkg/sharding/strategy.go`（138 行）

### 4.1 数据结构

```go
type ShardingStrategy struct {
    dbBase       string // "task"
    tableBase    string // "task" 或 "task_execution"
    dbSharding   int    // 库分片数，如 2
    tableSharding int   // 表分片数，如 2（task）或 4（execution）
}

type Dst struct {
    Table       string // 完整表名，如 "task_0"
    DB          string // 完整库名，如 "task_0"
    TableSuffix string // 表后缀，如 "_0"
    DBSuffix    string // 库后缀，如 "_0"
}
```

### 4.2 路由算法：Shard()

```go
func (s *ShardingStrategy) Shard(bizID int) Dst {
    dbHash := bizID % s.dbSharding       // 第一级：选库
    tabHash := (bizID / s.dbSharding) % s.tableSharding  // 第二级：选表
    return Dst{
        DB:          fmt.Sprintf("%s_%d", s.dbBase, dbHash),
        Table:       fmt.Sprintf("%s_%d", s.tableBase, tabHash),
        DBSuffix:    fmt.Sprintf("_%d", dbHash),
        TableSuffix: fmt.Sprintf("_%d", tabHash),
    }
}
```

**路由示例**（dbSharding=2, tableSharding=2）：

| bizID | dbHash (bizID%2) | tabHash ((bizID/2)%2) | DB | Table |
|-------|------------------|-----------------------|----|-------|
| 0 | 0 | 0 | task_0 | task_0 |
| 1 | 1 | 0 | task_1 | task_0 |
| 2 | 0 | 1 | task_0 | task_1 |
| 3 | 1 | 1 | task_1 | task_1 |
| 4 | 0 | 0 | task_0 | task_0 |

可以看到 `bizID 0~3` 正好覆盖所有 4 个分片，第 4 个开始循环。

### 4.3 广播：Broadcast()

```go
func (s *ShardingStrategy) Broadcast() []Dst {
    var result []Dst
    for i := 0; i < s.dbSharding; i++ {
        for j := 0; j < s.tableSharding; j++ {
            result = append(result, Dst{...})
        }
    }
    return result  // 笛卡尔积：db × table
}
```

返回所有分片的 `Dst` 列表。用于：
- `ShardingLoopJob.Run()` 遍历所有分片
- `ShardingTaskDAO.Renew()` 广播更新所有表
- 补偿器全量扫描

### 4.4 Context 传递机制

```go
type dstKey struct{}

func CtxWithDst(ctx context.Context, dst Dst) context.Context {
    return context.WithValue(ctx, dstKey{}, dst)
}

func DstFromCtx(ctx context.Context) Dst {
    return ctx.Value(dstKey{}).(Dst)
}
```

**设计精妙之处**：路由信息通过 `context.Context` 传递，DAO 层从 ctx 取出 `Dst` 决定操作哪个库哪张表。这样上层（调度器/补偿器）只需要在进入分片循环时注入一次 `Dst`，之后整条调用链（Service → Repository → DAO）都能自动路由到正确的分片，**零侵入**。

---

## 5. Snowflake ID 生成器

**文件**：`pkg/id_generator/snowflake.go`（111 行）

### 5.1 Bit 布局

```
标准 Snowflake:
| 1bit 符号 | 41bit 时间戳 | 10bit workerID | 12bit 序列号 |

本系统 Snowflake:
| 1bit 符号 | 12bit bizID | 12bit 序列号 | 41bit 时间戳 |
             ↑ 分片路由     ↑ 毫秒内序列     ↑ 从 epoch 起的毫秒数
```

**为什么把 bizID 放高位？**
- `ExtractShardingID(id)` 只需一次右移：`id >> (sequenceBits + timestampBits)` = `id >> 53`
- 高位变化频率最低（同一 bizID 的所有 ID 共享前 12 bit），有利于数据库 B+ 树索引的局部性

### 5.2 核心方法

```go
// 生成 ID：传入 shardingID（通常是 bizID）
func (g *Generator) GenerateID(shardingID int) (int64, error) {
    now := time.Now().UnixMilli() - g.epoch  // epoch: 2024-01-01
    // ... 序列号处理（同毫秒递增，满了等下一毫秒）...
    id := int64(shardingID)<<(sequenceBits+timestampBits) |
          int64(g.sequence)<<timestampBits |
          now
    return id, nil
}

// 从 ID 反推 shardingID
func ExtractShardingID(id int64) int {
    return int(id >> (sequenceBits + timestampBits))  // >> 53
}
```

### 5.3 在 DAO 中的使用

```go
// ShardingTaskDAO.Create
func (s *ShardingTaskDAO) Create(ctx context.Context, task Task) (int64, error) {
    id, _ := s.idGen.GenerateID(task.BizID)  // 用 BizID 作为 shardingID
    task.Id = id
    dst := s.shardingStrategy.Shard(task.BizID)  // 路由到分片
    // ...
}

// ShardingTaskDAO.GetByID — 不需要 BizID，从 ID 自己提取
func (s *ShardingTaskDAO) GetByID(ctx context.Context, id int64) (Task, error) {
    shardingID := idPkg.ExtractShardingID(id)  // O(1) 定位分片
    dst := s.shardingStrategy.Shard(shardingID)
    // ...
}
```

### 5.4 Execution 表的路由：taskID % 1024

Execution 的 shardingID 不是自己的 bizID，而是 `taskID % 1024`：

```go
// ShardingTaskExecutionDAO.Create
id, _ := s.idGen.GenerateID(int(taskExecution.TaskID % shardingNumber))  // shardingNumber = 1024
dst := s.shardingStrategy.Shard(int(taskExecution.TaskID))
```

**为什么？** Execution 必须和它的 Task 路由到「相关」的分片。通过 `taskID` 做路由，保证同一 Task 的所有 Execution 都在相同的路由路径上（虽然 task 表和 execution 表的分片数不同，但路由因子一致）。

---

## 6. ShardingLoopJob 框架

**文件**：`pkg/loopjob/shardingloopjob.go`（237 行）

这是 V2 架构的**核心骨架**——V2 调度器和所有 V2 补偿器都不直接写循环，而是把业务逻辑注入到这个框架中。

### 6.1 结构定义

```go
type ShardingLoopJob struct {
    dclient  dlock.Client          // Redis 分布式锁客户端
    baseKey  string                // 锁 key 前缀，如 "scheduleKey"
    biz      func(ctx context.Context) error  // 业务逻辑
    strategy sharding.ShardingStrategy
    sem      ResourceSemaphore     // 并发度控制
}
```

### 6.2 Run()：外层循环——遍历所有分片

```go
func (s *ShardingLoopJob) Run(ctx context.Context) {
    for {
        if ctx.Err() != nil { return }
        for _, dst := range s.strategy.Broadcast() {
            if ctx.Err() != nil { return }

            err := s.sem.Acquire()  // 并发度控制
            if errors.Is(err, ErrExceedLimit) {
                continue  // 超过限制，跳过这个分片
            }

            key := fmt.Sprintf("%s:%s:%s", s.baseKey, dst.DB, dst.Table)
            lock, _ := s.dclient.NewLock(ctx, key, retryInterval)  // retryInterval = 1 min
            err = lock.Lock(ctx)  // 抢锁，抢不到阻塞
            if err != nil {
                s.sem.Release()
                continue
            }

            go s.tableLoop(ctx, dst, lock)  // 抢到了，开协程处理
        }
    }
}
```

### 6.3 tableLoop()：中层——单分片处理循环

```go
func (s *ShardingLoopJob) tableLoop(ctx context.Context, dst sharding.Dst, lock dlock.Lock) {
    defer s.sem.Release()  // 协程退出时释放信号量

    s.bizLoop(ctx, dst, lock)

    // bizLoop 退出说明 ctx 被取消或业务失败
    lock.Unlock(context.Background())  // 用 Background，确保即使 ctx 取消也能解锁
}
```

> **为什么用 `context.Background()` 解锁？** 因为 `lock.Unlock()` 走 Redis 网络调用。如果用已取消的 ctx，解锁会失败，锁就得等 TTL 过期才释放。用 Background 确保优雅退出时立即释放锁，其他节点能更快接管。

### 6.4 bizLoop()：内层——业务逻辑 + 锁续约

```go
func (s *ShardingLoopJob) bizLoop(ctx context.Context, dst sharding.Dst, lock dlock.Lock) {
    for {
        bizCtx, cancel := context.WithTimeout(ctx, 50*time.Second)
        bizCtx = sharding.CtxWithDst(bizCtx, dst)  // 注入路由信息

        err := s.biz(bizCtx)  // 执行业务逻辑
        cancel()

        if ctx.Err() != nil { return }  // 外层取消

        err = lock.Refresh(ctx)  // 续约
        if err != nil { return } // 续约失败 → 退出，外层会重新抢锁
    }
}
```

### 6.5 关键时序约束

```
bizTimeout (50s) < lock TTL (retryInterval = 60s)
```

这个约束保证：**业务逻辑执行完毕时，锁还没过期**，可以成功续约。如果 bizTimeout ≥ lock TTL，业务还没执行完锁就过期了，其他节点会抢到同一分片，导致重复调度。

```
时间线：
0s          50s              60s
|──── biz ────|── Refresh ──|── 锁过期 ──
                    ↑
              必须在这之前续约成功
```

### 6.6 整体流程图

```
Run() 外层无限循环
  │
  ├→ Broadcast() 获取所有分片 [task_0:task_0, task_0:task_1, task_1:task_0, task_1:task_1]
  │
  ├→ 对每个分片:
  │     │
  │     ├→ semaphore.Acquire()  ← 超限则 skip
  │     │
  │     ├→ dclient.NewLock(key)
  │     │
  │     ├→ lock.Lock()          ← 抢不到则阻塞（直到 ctx 取消或锁释放）
  │     │
  │     └→ go tableLoop()
  │           │
  │           ├→ bizLoop()
  │           │     │
  │           │     ├→ CtxWithDst(ctx, dst)   ← 注入路由
  │           │     ├→ biz(ctx)               ← 50s 超时执行业务
  │           │     ├→ lock.Refresh()         ← 续约
  │           │     └→ 循环直到 ctx 取消或续约失败
  │           │
  │           └→ lock.Unlock(Background)      ← 退出时释放锁
  │
  └→ 回到外层，重新遍历所有分片
```

---

## 7. ResourceSemaphore：并发度控制

**文件**：`pkg/loopjob/resource.go`（90 行）

### 7.1 接口定义

```go
type ResourceSemaphore interface {
    Acquire() error
    Release()
}
```

### 7.2 MaxCntResourceSemaphore 实现

```go
type MaxCntResourceSemaphore struct {
    mu       sync.Mutex
    curCount int
    maxCount int
}

func (r *MaxCntResourceSemaphore) Acquire() error {
    r.mu.Lock()
    defer r.mu.Unlock()
    if r.curCount >= r.maxCount {
        return ErrExceedLimit
    }
    r.curCount++
    return nil
}

func (r *MaxCntResourceSemaphore) Release() {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.curCount--
}

// 运行时动态调整最大并发数
func (r *MaxCntResourceSemaphore) UpdateMaxCount(maxCount int) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.maxCount = maxCount
}
```

**为什么需要 `UpdateMaxCount()`？** 运行时可以动态调节单节点处理的分片数上限。例如：
- 节点负载高 → 降低 maxCount → 主动释放部分分片给其他节点
- 新增节点 → 降低现有节点的 maxCount → 为新节点腾出分片
- 夜间低峰 → 提高 maxCount → 单节点接管更多分片

---

## 8. V2 调度器：SchedulerV2

**文件**：`internal/service/scheduler/schedulerv2.go`（236 行）

### 8.1 结构定义

```go
type SchedulerV2 struct {
    taskSvc  service.TaskService
    runners  map[string]runner.Runner
    limiter  Limiter
    dclient  dlock.Client              // 新增：分布式锁客户端
    sem      loopjob.ResourceSemaphore // 新增：并发度控制
    taskStr  sharding.ShardingStrategy // 新增：分片路由策略
}
```

### 8.2 Start()：启动调度

```go
func (s *SchedulerV2) Start(ctx context.Context) {
    go loopjob.NewShardingLoopJob(
        s.dclient,
        "scheduleKey",        // 锁 key 前缀
        s.scheduleOneLoop,    // 注入业务逻辑
        s.taskStr,
        s.sem,
    ).Run(ctx)               // 注意：这里调用了 .Run(ctx) 启动框架
}
```

### 8.3 scheduleOneLoop()：V2 的核心调度逻辑

```go
func (s *SchedulerV2) scheduleOneLoop(ctx context.Context) error {
    // ctx 中已经包含了 Dst 路由信息（由 ShardingLoopJob 注入）
    tasks, err := s.taskSvc.FindSchedulableTasks(ctx)  // ← DAO 从 ctx 取 Dst
    if err != nil || len(tasks) == 0 { return err }

    for _, task := range tasks {
        s.limiter.Limit(ctx, task)  // 限流

        go func(t domain.Task) {
            s.taskSvc.Acquire(ctx, t)  // CAS 抢占
            r := s.runners[t.Type]
            r.Run(ctx, t)              // 执行
        }(task)
    }
    return nil
}
```

### 8.4 V1 vs V2 调度器对比

| 维度 | V1 Scheduler | V2 SchedulerV2 |
|------|-------------|----------------|
| 主循环 | 自己写 `for{}` + `time.Sleep` | ShardingLoopJob 框架驱动 |
| 分片感知 | 无，全表扫描 | 每次只扫当前分片的表 |
| 多节点协调 | 不支持 | 分布式锁自动瓜分 |
| 并发控制 | 无 | ResourceSemaphore |
| 锁续约 | 无 | ShardingLoopJob 自动 Refresh |
| 优雅退出 | `sync.WaitGroup` + `cancel()` | 框架管理生命周期 |
| 业务逻辑 | 直接在循环体中 | 通过 `biz func(ctx) error` 注入 |
| 接口 | `GracefulStop()` | 实现 `server.Server` 接口 |

---

## 9. V2 补偿器（全部 4 个）

四个 V2 补偿器共享相同的架构模式：**用 ShardingLoopJob 框架包裹业务逻辑**。区别仅在于锁 key 和具体的扫描/处理逻辑。

### 9.1 通用模式

```go
// 以 InterruptCompensatorV2 为例
type InterruptCompensatorV2 struct {
    taskSvc  service.TaskService
    execSvc  service.ExecutionService
    dclient  dlock.Client
    sem      loopjob.ResourceSemaphore
    execStr  sharding.ShardingStrategy  // 执行表的分片策略
}

func (c *InterruptCompensatorV2) Start(ctx context.Context) {
    go loopjob.NewShardingLoopJob(
        c.dclient,
        "interruptKey",          // 每个补偿器独立的锁前缀
        c.compensateOneLoop,     // 注入补偿逻辑
        c.execStr,               // 执行表的分片策略（2库×4表）
        c.sem,
    ).Run(ctx)
}
```

### 9.2 四个补偿器详情

| 补偿器 | 锁 Key 前缀 | 扫描目标 | 处理逻辑 |
|--------|------------|---------|---------|
| **InterruptCompensatorV2** | `interruptKey` | 超时的执行记录（status=RUNNING, updated_at 过期） | 标记 FAILED → releaseTask → UpdateNextTime |
| **RetryCompensatorV2** | `retryKey` | 可重试的执行记录（status=FAILED, retry_count < max） | 创建新执行记录 → 重新调度 |
| **RescheduleCompensatorV2** | `rescheduleKey` | 可补调度的执行记录（上次调度失败的） | 重新创建执行记录 → 触发调度 |
| **ShardingCompensatorV2** | `shardingKey` | 分片父任务（ShardingParentID=0, status=RUNNING） | 查子任务 → anyFailed/allSuccess → 更新父状态 → releaseTask |

### 9.3 ShardingCompensatorV2 的特殊之处

**文件**：`internal/compensator/shardingv2.go`（197 行）

```go
type ShardingCompensatorV2 struct {
    // ...
    offsets syncx.Map[string, int64]  // 每个分片独立维护扫描偏移量
}

func (c *ShardingCompensatorV2) compensateOneLoop(ctx context.Context) error {
    dst := sharding.DstFromCtx(ctx)
    offsetKey := fmt.Sprintf("%s:%s", dst.DB, dst.Table)

    offset, _ := c.offsets.Load(offsetKey)  // 取当前分片的偏移量
    parents, _ := c.execSvc.FindShardingParents(ctx, offset, c.limit)
    // ... 聚合子任务状态 ...
    c.offsets.Store(offsetKey, newOffset)  // 更新偏移量
    return nil
}
```

与 V1 ShardingCompensator 不同，V2 版本用 `syncx.Map` 为**每个分片**独立维护偏移量。V1 只有一个全局 offset，V2 有 N 个（每分片一个），互不干扰。

### 9.4 V1 vs V2 补偿器对比

| 维度 | V1 补偿器 | V2 补偿器 |
|------|----------|----------|
| 主循环 | `for { select { case <-ctx.Done(): return; default: ... } }` | ShardingLoopJob 框架 |
| 防空转 | `MinDuration(interval)` 计时器 | 框架内建 50s 超时 + 续约节奏 |
| 分片感知 | 无，全表扫描 | 从 ctx 取 Dst，只扫当前分片 |
| 多节点 | 不支持 | 分布式锁竞争 |
| 偏移量 | 单个 offset 变量 | syncx.Map 按分片独立维护 |

---

## 10. V2 DAO 层

### 10.1 ShardingTaskDAO

**文件**：`internal/repository/dao/sharding_task.go`（310 行）

核心区别：持有 `dbs map[string]*egorm.Component`（多个数据库连接），所有操作先定位分片再操作。

#### 关键方法解析

**Create — 用 bizID 生成 ID 并路由**：
```go
func (s *ShardingTaskDAO) Create(ctx context.Context, task Task) (int64, error) {
    id, _ := s.idGen.GenerateID(task.BizID)
    task.Id = id
    dst := s.shardingStrategy.Shard(task.BizID)
    return id, s.dbs[dst.DB].WithContext(ctx).Table(dst.Table).Create(&task).Error
}
```

**GetByID — 从 ID 反推路由，不需要额外参数**：
```go
func (s *ShardingTaskDAO) GetByID(ctx context.Context, id int64) (Task, error) {
    shardingID := idPkg.ExtractShardingID(id)
    dst := s.shardingStrategy.Shard(shardingID)
    var task Task
    err := s.dbs[dst.DB].WithContext(ctx).Table(dst.Table).Where("id = ?", id).First(&task).Error
    return task, err
}
```

**FindSchedulableTasks — 从 Context 取路由**：
```go
func (s *ShardingTaskDAO) FindSchedulableTasks(ctx context.Context, ...) ([]Task, error) {
    dst := sharding.DstFromCtx(ctx)  // 由 ShardingLoopJob 注入
    var tasks []Task
    err := s.dbs[dst.DB].WithContext(ctx).Table(dst.Table).
        Where("status = ? AND next_exec_time <= ?", ...).Find(&tasks).Error
    return tasks, err
}
```

**Renew — 广播所有分片，errgroup 并行更新**：
```go
func (s *ShardingTaskDAO) Renew(ctx context.Context, id int64) error {
    dsts := s.shardingStrategy.Broadcast()
    // 按库分组
    dbDsts := make(map[string][]sharding.Dst)
    for _, dst := range dsts {
        dbDsts[dst.DB] = append(dbDsts[dst.DB], dst)
    }
    // errgroup 并行：每个库一个 goroutine，拼接多表 UPDATE 的 raw SQL
    eg, egCtx := errgroup.WithContext(ctx)
    for db, tables := range dbDsts {
        eg.Go(func() error {
            var sqls []string
            for _, t := range tables {
                sqls = append(sqls, fmt.Sprintf(
                    "UPDATE %s SET updated_at = %d WHERE id = %d AND status = 'RUNNING'",
                    t.Table, time.Now().UnixMilli(), id,
                ))
            }
            return s.dbs[db].WithContext(egCtx).Exec(strings.Join(sqls, "; ")).Error
        })
    }
    return eg.Wait()
}
```

> **为什么 Renew 要广播？** 因为调用者不知道 task 在哪个分片（Renew 场景下可能没有 bizID 信息），所以对所有分片都执行一次 UPDATE。由于 WHERE 条件带了 `id`，只有目标分片会实际匹配到行，其他分片是空更新，代价很低。

### 10.2 ShardingTaskExecutionDAO

**文件**：`internal/repository/dao/sharding_task_execution.go`（524 行）

#### BatchCreate — 分组 SQL 批量插入

这是整个 DAO 层最复杂的方法，核心挑战：**N 个子执行记录可能散落在不同分片，需要按分片分组后批量插入**。

```go
func (s *ShardingTaskExecutionDAO) BatchCreate(ctx context.Context, execs []TaskExecution) error {
    // 1. 为每个 execution 生成 ID
    for i := range execs {
        id, _ := s.idGen.GenerateID(int(execs[i].TaskID % shardingNumber))
        execs[i].Id = id
    }

    // 2. 按库分组
    execMap := s.getTaskExecutionMap(execs)  // map[dbName][]TaskExecution

    // 3. 每个库独立生成 SQL
    eg, egCtx := errgroup.WithContext(ctx)
    for dbName, dbExecs := range execMap {
        eg.Go(func() error {
            // 用 GORM DryRun 生成 INSERT SQL（不执行）
            sqls := s.genSQLs(egCtx, dbName, dbExecs)
            combinedSQL := strings.Join(sqls, "; ")
            // 一次性执行合并后的 SQL
            return s.dbs[dbName].WithContext(egCtx).Exec(combinedSQL).Error
        })
    }
    return eg.Wait()
}
```

**GORM DryRun 技巧**：
```go
func (s *ShardingTaskExecutionDAO) genSQLs(ctx context.Context, dbName string, execs []TaskExecution) []string {
    var sqls []string
    for _, exec := range execs {
        dst := s.shardingStrategy.Shard(int(exec.TaskID))
        stmt := s.dbs[dbName].Session(&gorm.Session{DryRun: true}).
            Table(dst.Table).Create(&exec).Statement
        sqls = append(sqls, s.dbs[dbName].Dialector.Explain(stmt.SQL.String(), stmt.Vars...))
    }
    return sqls
}
```

用 `DryRun: true` 让 GORM 生成 SQL 但不执行，然后手动拼接多条 INSERT，一次网络往返插入一个库的所有记录。

#### ID 冲突重试

BatchCreate 内部有 ID 冲突重试机制：如果 Snowflake 在极端并发下生成了重复 ID，捕获唯一键冲突后重新生成 ID 并重试。

---

## 11. 分布式锁

**文件**：`ioc/dlock.go`（18 行）

```go
func InitDistributedLock(rdb *redis.Client) dlock.Client {
    return dlockredis.NewClient(rdb)
}
```

使用 `meoying/dlock-go` 库的 Redis 实现，底层是 `SET key value NX EX ttl` + Lua 脚本保证原子性。

### 锁 Key 格式

```
{baseKey}:{dbName}:{tableName}

示例：
scheduleKey:task_0:task_0      ← 调度器处理 task_0 库的 task_0 表
interruptKey:task_0:task_exec_0 ← 中断补偿器处理 task_0 库的 task_exec_0 表
retryKey:task_1:task_exec_3     ← 重试补偿器处理 task_1 库的 task_exec_3 表
```

每个 `{组件}:{库}:{表}` 组合一把锁，同一时刻只有一个节点处理该分片。

### 锁的生命周期

```
Node A                              Redis                           Node B
   │                                   │                                │
   ├─ SET scheduleKey:t0:t0 NX EX 60 ─→│                                │
   │                              OK ←─┤                                │
   │                                   │←─ SET scheduleKey:t0:t0 NX ────┤
   │                                   ├── FAIL (key exists) ──────────→│
   │                                   │                         阻塞等待│
   │  ... 执行 biz (50s) ...           │                                │
   ├─ EXPIRE scheduleKey:t0:t0 60 ────→│  续约成功                       │
   │  ... 执行 biz (50s) ...           │                                │
   ├─ DEL scheduleKey:t0:t0 ──────────→│  主动释放                       │
   │                                   │←─ SET scheduleKey:t0:t0 NX ────┤
   │                                   ├── OK ────────────────────────→│
   │                                   │                        接管分片│
```

---

## 12. 数据库 Schema

**文件**：`scripts/mysql/init.sql`（403 行）

### 12.1 数据库拓扑

```sql
-- V1 单库（保留）
CREATE DATABASE IF NOT EXISTS task;

-- V2 分库
CREATE DATABASE IF NOT EXISTS task_0;
CREATE DATABASE IF NOT EXISTS task_1;
```

### 12.2 分表结构

每个 V2 库包含 6 张表：

```sql
-- task_0 库
USE task_0;

CREATE TABLE task_0 (         -- 任务表分片 0
    id         BIGINT PRIMARY KEY,  -- Snowflake ID（非自增）
    biz_id     INT NOT NULL,
    name       VARCHAR(256),
    type       VARCHAR(32),
    cron       VARCHAR(256),
    status     TINYINT DEFAULT 0,
    next_exec_time BIGINT DEFAULT 0,
    sharding_rule  TEXT,       -- JSON 格式的分片规则
    -- ...
    INDEX idx_status_next_exec_time (status, next_exec_time)
);

CREATE TABLE task_1 (...);    -- 任务表分片 1

CREATE TABLE task_execution_0 (  -- 执行表分片 0
    id                 BIGINT PRIMARY KEY,
    task_id            BIGINT NOT NULL,
    status             TINYINT DEFAULT 0,
    sharding_parent_id BIGINT DEFAULT NULL,
    -- ...
    INDEX idx_sharding_parent_id (sharding_parent_id)
);

CREATE TABLE task_execution_1 (...);  -- 执行表分片 1
CREATE TABLE task_execution_2 (...);  -- 执行表分片 2
CREATE TABLE task_execution_3 (...);  -- 执行表分片 3

-- task_1 库（完全相同的 6 张表）
```

### 12.3 总数据分布

```
task_0                    task_1
├── task_0          (bizID%2=0, (bizID/2)%2=0)
├── task_1          (bizID%2=0, (bizID/2)%2=1)
├── task_execution_0 (taskID%2=0, (taskID/2)%4=0)
├── task_execution_1 (taskID%2=0, (taskID/2)%4=1)
├── task_execution_2 (taskID%2=0, (taskID/2)%4=2)
├── task_execution_3 (taskID%2=0, (taskID/2)%4=3)
│                         ├── task_0          (bizID%2=1, ...)
│                         ├── task_1
│                         ├── task_execution_0
│                         ├── task_execution_1
│                         ├── task_execution_2
│                         └── task_execution_3

V1: 1 库 × 2 表 = 2 张表
V2: 2 库 × 6 表 = 12 张表
```

### 12.4 关键索引

- `idx_status_next_exec_time (status, next_exec_time)` — 调度器扫描可调度任务
- `idx_sharding_parent_id (sharding_parent_id)` — ShardingCompensator 查父/子任务

---

## 13. IOC 装配与部署

### 13.1 生产环境 Wire（V1）

**文件**：`cmd/scheduler/ioc/wire.go`

```go
var ProviderSet = wire.NewSet(
    ioc.InitDB,                    // 单库连接
    dao.NewGORMTaskDAO,            // V1 DAO
    dao.NewGORMTaskExecutionDAO,   // V1 DAO
    ioc.InitScheduler,             // V1 调度器
    ioc.InitInterruptCompensator,  // V1 补偿器
    ioc.InitRetryCompensator,
    ioc.InitRescheduleCompensator,
    ioc.InitShardingCompensator,
    // ... 其他 V1 组件
)
```

### 13.2 测试环境 Wire（V2）

**文件**：`internal/test/integration/ioc/wire.go`

```go
var shardingSet = wire.NewSet(
    InitDBs,                          // 多库连接 map[string]*egorm.Component
    InitIDGenerator,                  // Snowflake ID 生成器
    InitShardingTaskDAO,              // V2 Task DAO
    InitShardingTaskExecutionDAO,     // V2 Execution DAO
)
```

**文件**：`internal/test/integration/ioc/sharding.go`

```go
func InitShardingTaskDAO(dbs map[string]*egorm.Component, idGen *idPkg.Generator) dao.TaskDAO {
    return dao.NewShardingTaskDAO(
        dbs, idGen,
        sharding.NewShardingStrategy("task", "task", 2, 2),  // 2库×2表
    )
}

func InitShardingTaskExecutionDAO(dbs map[string]*egorm.Component, idGen *idPkg.Generator) dao.TaskExecutionDAO {
    return dao.NewShardingTaskExecutionDAO(
        dbs, idGen,
        sharding.NewShardingStrategy("task", "task_execution", 2, 4),  // 2库×4表
    )
}

func InitDBs() map[string]*egorm.Component {
    return map[string]*egorm.Component{
        "task_0": connectDB("task_0"),
        "task_1": connectDB("task_1"),
    }
}
```

### 13.3 Docker Compose 新增

```yaml
# docker-compose.yml
redis:
  image: redislabs/rebloom:latest
  ports:
    - "6379:6379"

# config/docker-config.yaml
redis:
  addr: "redis:6379"
```

Redis 用于分布式锁，是 V2 架构的唯一新增基础设施依赖。

---

## 14. 集成测试

### 14.1 DAO 层测试

**ShardingTaskDAO 测试**（`sharding_task_test.go`，381 行）：
- Create → 验证 ID 是 Snowflake 格式
- GetByID → 验证从 ID 反推路由正确
- FindSchedulableTasks → 验证 context 路由
- Acquire → 验证 CAS 抢占

**ShardingTaskExecutionDAO 测试**（`sharding_task_execution_test.go`，571 行）：
- BatchCreate → 验证跨分片批量插入
- FindShardingParents → 验证按分片扫描
- FindShardingChildren → 验证从 parentID 反推路由

### 14.2 E2E 集成测试

**分片中断测试**（`sharding_interrupt/sharding_task_test.go`，593 行）：
1. 启动 3 个执行器节点
2. 插入 Range 分片任务（totalNums=3, step=10000）
3. 调度器抢占 → 拆分 3 个子任务
4. 子任务分别在 3 个节点执行
5. 模拟中断 → InterruptCompensator 检测
6. 重新调度 → 最终所有子任务完成
7. ShardingCompensator 聚合 → 父任务 SUCCESS

---

## 15. 实现局限性与改进方向

### 15.1 当前局限

| 问题 | 说明 |
|------|------|
| **V2 未接入生产** | V2 组件只在测试 Wire 中组装，生产仍跑 V1。缺少 V1→V2 数据迁移脚本和灰度切换方案 |
| **无在线扩容方案** | 从 2×2 扩到 4×4 需要全量数据迁移，没有实现平滑 rehash |
| **无跨分片查询** | 如果需要按非路由字段查询（如 task name），必须广播所有分片再合并 |
| **Snowflake 时钟回拨** | 当前实现依赖本机时钟单调递增，NTP 回拨可能导致 ID 冲突 |
| **锁粒度固定** | 每个表一把锁，如果某个分片数据量远大于其他分片（热点倾斜），无法进一步细分 |

### 15.2 改进方向

1. **灰度切换方案**：双写模式（V1 写入同时 V2 影子写入）→ 对比验证 → 切读 → 切写 → 关闭 V1
2. **在线扩容**：翻倍策略（2→4 库），新分片只有新数据写入，旧数据异步迁移
3. **热点检测**：监控每个分片的扫描量和处理耗时，自动调整 ResourceSemaphore
4. **时钟回拨防护**：记录上次生成的时间戳，回拨时等待或报错

---

## 16. 面试高频问题

### Q1：为什么不用一致性哈希？

> 一致性哈希解决的是**节点动态增减时最小化数据迁移**的问题，典型场景是缓存集群。任务调度平台的分片数在部署时确定，几乎不会频繁变化，两级哈希的确定性和 O(1) 计算复杂度更适合这个场景。如果未来需要动态扩容，应该走「翻倍扩容 + 异步迁移」路径，而不是引入一致性哈希的复杂性。

### Q2：分布式锁过期了但业务还没执行完怎么办？

> 不会发生。关键约束：`bizTimeout(50s) < lockTTL(60s)`。每轮 biz 执行完后立即 Refresh 续约。如果 biz 执行时间异常长（超过 50s context 超时），biz 会被 cancel，然后进入 Refresh 续约。如果 Refresh 也失败（锁已被其他节点抢走），当前 goroutine 退出 bizLoop，外层重新竞争该分片。最坏情况是同一分片被两个节点短暂处理，但 CAS `Acquire` 保证不会重复执行同一任务。

### Q3：Snowflake 时钟回拨怎么处理？

> 当前实现是简单等待（同毫秒内序列号用完就自旋等下一毫秒）。更健壮的做法：记录 `lastTimestamp`，如果 `now < lastTimestamp` 说明时钟回拨，可以选择（1）等待直到追上 lastTimestamp，（2）直接报错拒绝生成，（3）使用备用序列空间。生产环境建议方案 1 + 短回拨容忍阈值（如 5ms 以内等待，超过则报错）。

### Q4：如何做到不停机从 V1 切换到 V2？

> 分三阶段：
> 1. **双写**：V1 正常写入 + V2 影子写入（通过 binlog 或应用层双写），验证数据一致性
> 2. **切读**：读流量切到 V2，写流量仍走 V1+V2 双写，验证读取正确性
> 3. **切写**：写流量切到 V2，关闭 V1 双写。保留 V1 数据一段时间用于回滚
>
> 每个阶段设置回滚开关，出问题立即退回上一阶段。

### Q5：如果某个分片成为热点怎么办？

> 短期：通过 `ResourceSemaphore.UpdateMaxCount()` 动态调整，让处理热点分片的节点减少其他分片的负载。长期：引入虚拟分片——一个物理分片拆分为多个虚拟分片，每个虚拟分片独立加锁，实现更细粒度的负载均衡。但这增加了锁管理复杂度，需要权衡。

### Q6：ShardingLoopJob 的 Background context 解锁会不会导致问题？

> 不会。`lock.Unlock(context.Background())` 的目的是确保解锁操作不受上层 ctx 取消的影响。如果用已取消的 ctx 解锁，Redis 调用会因 ctx.Done() 直接返回，锁无法释放，其他节点要等 TTL 过期。用 Background 牺牲的只是「调用者取消时解锁操作也取消」的能力——但我们恰恰不想要这个能力，因为锁必须被释放。

---

## 17. 总结

Step 7 通过三个核心组件实现了从单库单表到分库分表的架构升级：

1. **ShardingStrategy** — 两级哈希路由 + Context 传递，让上层无感知地操作正确的分片
2. **Snowflake ID Generator** — ID 内嵌路由信息，实现 O(1) 分片定位，消除路由表依赖
3. **ShardingLoopJob** — 统一的分布式调度框架，封装「遍历分片 → 抢锁 → 执行业务 → 续约」的完整生命周期

在此基础上，V2 调度器和 4 个 V2 补偿器只需要关注自己的业务逻辑（`biz func(ctx) error`），框架负责分片遍历、分布式锁、并发控制和优雅退出。

**架构演进脉络**：

```
Step 1: 单机 for{} 循环 → 最小闭环
Step 2: + gRPC 远程执行
Step 3: + Kafka 异步状态上报
Step 4: + 补偿机制（中断/重试/补调度）
Step 5: + DAG 工作流引擎
Step 6: + 分片任务（单库，单机调度）
Step 7: + 分库分表 + 分布式调度（多库，多机协调）  ← 你在这里
```

从 Step 1 的 100 行 for 循环，到 Step 7 的分布式分片调度框架——每一步都在解决上一步的规模瓶颈。
