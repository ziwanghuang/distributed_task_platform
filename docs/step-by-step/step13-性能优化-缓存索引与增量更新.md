# Step 13：性能优化 — 缓存、索引与增量更新

> **目标**：针对调度引擎中 4 个高频路径的性能瓶颈，逐一给出代码级优化方案——优先级队列、DAG 缓存、O(1) 索引查找、服务发现增量更新——降低调度延迟、减少无效 DB 查询和 CPU 消耗。
>
> **完成后你能看到**：高优任务在秒级内被调度（不再被低优任务阻塞）→ DAG 工作流二次执行命中缓存（0 次 DB 查询 + 0 次 ANTLR 解析）→ Plan.GetTask 从 O(n) 降至 O(1) → Resolver 不再因单个节点上下线而全量拉取服务列表。

---

## 1. Step 12 → Step 13 变更对比

| 维度 | Step 12 | Step 13（性能优化：缓存、索引与增量更新） |
|------|---------|------|
| **关注点** | 功能完善与扩展 | 性能优化与效率提升 |
| **调度查询** | `ORDER BY next_time ASC`，无优先级 | `ORDER BY priority DESC, next_time ASC`，支持多级队列 |
| **DAG 工作流** | 每次执行重新查 DB + ANTLR 解析 | 两层缓存（进程内 LRU + Redis），version 主动失效 |
| **Plan 节点查找** | `GetTask` 线性遍历 O(n) | `buildIndex()` 构建 map 索引 O(1) |
| **服务发现** | Watch 事件触发全量 `ListServices` | Event 携带 Instance 数据，增量 add/remove |
| **影响范围** | domain/dao/service/resolver | 同上，但改动更精准、无功能变更 |

---

## 2. 优化点总览

```
┌─────────────────────────────────────────────────────────────────────────┐
│                        性能优化四大方向                                  │
│                                                                         │
│  ┌──────────────────┐  ┌──────────────────┐  ┌──────────────────────┐  │
│  │ 13.1 优先级队列   │  │ 13.2 DAG 缓存    │  │ 13.3 O(1) 索引     │  │
│  │                  │  │                  │  │                      │  │
│  │ FindSchedulable  │  │ PlanService      │  │ Plan.GetTask         │  │
│  │ Tasks 排序改造   │  │ getPlanData 缓存 │  │ map 替代线性遍历     │  │
│  │                  │  │                  │  │                      │  │
│  │ priority DESC,   │  │ L1: 进程内 LRU   │  │ buildIndex() 在     │  │
│  │ next_time ASC    │  │ L2: Redis 分布式  │  │ taskToPlan 时构建   │  │
│  │                  │  │ version 失效     │  │                      │  │
│  └──────────────────┘  └──────────────────┘  └──────────────────────┘  │
│                                                                         │
│  ┌────────────────────────────────────────────────────────────────────┐ │
│  │ 13.4 Resolver 增量更新                                             │ │
│  │                                                                    │ │
│  │ Event 结构补充 Instance 字段 → etcd Subscribe 解析 Value →         │ │
│  │ Resolver 维护本地 map，增量 add/remove，不再 ListServices          │ │
│  └────────────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## 3. 优化点 13.1：任务优先级队列

### 3.1 现状分析

**当前代码**（`internal/repository/dao/task.go:FindSchedulableTasks`）：

```go
func (g *GORMTaskDAO) FindSchedulableTasks(ctx context.Context, preemptedTimeoutMs int64, limit int) ([]*Task, error) {
    var tasks []*Task
    now := time.Now().UnixMilli()
    err := g.db.WithContext(ctx).
        Where("next_time <= ? AND (status = ? OR (status = ? AND utime <= ?))",
            now, StatusActive, StatusPreempted, now-preemptedTimeoutMs).
        Order("next_time ASC").  // ← 只按时间排序，无优先级区分
        Limit(limit).
        Find(&tasks).Error
    // ...
}
```

**问题**：
- 所有任务一视同仁，`ORDER BY next_time ASC` 意味着先到期的先调度
- 高优任务（如告警处理、数据修复）可能排在数百个低优定时任务后面
- 极端情况下，一批 `batchSize=100` 的低优任务填满了调度窗口，高优任务要等下一轮

### 3.2 方案设计

#### 3.2.1 Domain 层：增加 Priority 字段

```go
// internal/domain/task.go

type TaskPriority int

const (
    TaskPriorityLow      TaskPriority = 0   // 日常定时任务
    TaskPriorityNormal   TaskPriority = 5   // 默认优先级
    TaskPriorityHigh     TaskPriority = 10  // 数据修复、报表生成
    TaskPriorityCritical TaskPriority = 15  // 告警处理、资金结算
)

type Task struct {
    ID       int64
    Name     string
    Priority TaskPriority  // 新增
    // ... 其他字段不变
}
```

#### 3.2.2 DAO 层：Task 结构体与 SQL 改造

```go
// internal/repository/dao/task.go

type Task struct {
    // ... 现有字段
    Priority int `gorm:"type:tinyint;not null;default:5;index:idx_priority_next_time_status,priority:1;comment:'任务优先级: 0=LOW, 5=NORMAL, 10=HIGH, 15=CRITICAL'"`
    // ... 
}
```

**SQL 改造**：

```go
func (g *GORMTaskDAO) FindSchedulableTasks(ctx context.Context, preemptedTimeoutMs int64, limit int) ([]*Task, error) {
    var tasks []*Task
    now := time.Now().UnixMilli()
    err := g.db.WithContext(ctx).
        Where("next_time <= ? AND (status = ? OR (status = ? AND utime <= ?))",
            now, StatusActive, StatusPreempted, now-preemptedTimeoutMs).
        Order("priority DESC, next_time ASC").  // ← 优先级降序 + 时间升序
        Limit(limit).
        Find(&tasks).Error
    if err != nil {
        return nil, err
    }
    return tasks, nil
}
```

#### 3.2.3 数据库索引设计

```sql
-- 新增复合索引，覆盖优先级排序场景
ALTER TABLE tasks ADD INDEX idx_priority_next_time_status 
    (priority DESC, next_time ASC, status);

-- 旧索引 idx_next_time_status_utime 保留（续约等场景仍需使用）
```

**索引选择逻辑**：

| 查询场景 | 使用索引 | 排序方式 |
|---------|---------|---------|
| `FindSchedulableTasks`（调度主循环） | `idx_priority_next_time_status` | `priority DESC, next_time ASC` |
| `Renew`（续约查询） | `idx_schedule_node_id_status` | 无排序 |
| `Release`（释放查询） | 主键 | 无排序 |

#### 3.2.4 高级方案：多级队列

对于更极端的场景，可以引入多级队列思想：

```go
// internal/service/scheduler/priority_queue.go

type PriorityQueueScheduler struct {
    taskAcquirer TaskAcquirer
    limiter      loadchecker.LoadChecker  // 令牌桶限流器
}

// Schedule 分级调度：CRITICAL 不受限流约束
func (s *PriorityQueueScheduler) Schedule(ctx context.Context) error {
    // 第一优先级：CRITICAL 任务，不受限流约束
    criticalTasks, _ := s.taskAcquirer.FindByPriority(ctx, TaskPriorityCritical, 10)
    for _, task := range criticalTasks {
        go s.dispatch(ctx, task)  // 立即调度，不经过 LoadChecker
    }

    // 第二优先级：HIGH/NORMAL/LOW 任务，受限流约束
    normalTasks, _ := s.taskAcquirer.FindSchedulableTasks(ctx, preemptedTimeout, batchSize)
    for _, task := range normalTasks {
        sleepDuration := s.limiter.Check(ctx)  // LoadChecker 控制调度速率
        if sleepDuration > 0 {
            time.Sleep(sleepDuration)
        }
        go s.dispatch(ctx, task)
    }
    return nil
}
```

### 3.3 设计决策

| 维度 | 简单排序方案 | 多级队列方案 |
|------|-----------|-----------|
| **实现复杂度** | 低，只改 SQL | 中，需要拆分调度循环 |
| **优先级保证** | 软保证（同一批次内有序） | 硬保证（CRITICAL 独立通道） |
| **限流影响** | CRITICAL 仍受 LoadChecker 约束 | CRITICAL 绕过限流 |
| **适用场景** | 日常调度优化 | 金融级、强 SLA 场景 |

**决策**：先实施简单排序方案（改动最小、收益最直接），多级队列作为后续迭代方向。

---

## 4. 优化点 13.2：DAG 工作流缓存

### 4.1 现状分析

**当前代码**（`internal/service/task/plan.go:getPlanData`）：

```go
func (p planService) getPlanData(ctx context.Context, planID int64) (...) {
    var g errgroup.Group
    // 查询 1：获取 Plan 基本信息（task 表）
    g.Go(func() error {
        plan, eerr = p.repo.GetByID(ctx, planID)
        return eerr
    })
    // 查询 2：获取 Plan 下所有子任务（task 表）
    g.Go(func() error {
        planTasks, eerr = p.repo.FindByPlanID(ctx, planID)
        return eerr
    })
    // 查询 3：获取 Plan 执行记录（task_execution 表）
    g.Go(func() error {
        execs, eerr := p.executionRepo.FindByTaskID(ctx, planID)
        // ...
    })
    g.Wait()
    // 查询 4：获取子任务执行记录（task_execution 表）
    planTaskExecs, err = p.executionRepo.FindExecutionsByPlanExecID(ctx, planExection.ID)
    return ...
}
```

然后在 `GetPlan` 中还会调用 ANTLR4 解析 DSL：

```go
astPlan, err := parser.NewAstPlan(plan.ExecExpr)  // CPU 密集操作
```

**问题**：
- **4 次 DB 查询**：每次 DAG 执行（包括 NextStep 触发的后续节点）都完整查询
- **ANTLR4 解析**：`parser.NewAstPlan` 涉及词法分析 + 语法分析 + AST 构建，CPU 消耗显著
- **DAG 定义是静态的**：Plan 的任务列表和 DSL 表达式在创建后不会变，每次重新解析完全浪费
- **NextStep 高频调用**：一个 5 节点的 DAG 执行一轮，`GetPlan` 至少被调用 5 次

### 4.2 方案设计：两层缓存

```
┌─────────────────────────────────────────────────────────────────┐
│                      缓存架构                                    │
│                                                                  │
│  GetPlan(planID)                                                │
│      │                                                           │
│      ▼                                                           │
│  ┌──────────────────────┐    miss    ┌───────────────────────┐  │
│  │  L1: 进程内 LRU 缓存  │ ────────► │  L2: Redis 分布式缓存  │  │
│  │  sync.RWMutex 保护    │           │  JSON 序列化存储       │  │
│  │  key: planID          │  ◄──────  │  key: plan:{planID}   │  │
│  │  容量: 100 条          │    hit    │  无 TTL，version 失效  │  │
│  └──────────┬───────────┘           └──────────┬────────────┘  │
│             │ miss                              │ miss          │
│             ▼                                   ▼               │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │  Origin: DB 查询 + ANTLR4 解析（getPlanData + taskToPlan）│   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                  │
│  失效策略：task.Version 变更时主动删除两层缓存                     │
└─────────────────────────────────────────────────────────────────┘
```

#### 4.2.1 缓存 Key 设计

```
L1 Key: planID (int64)
L2 Key: "plan_cache:{planID}:{version}"
```

**为什么用 version 而不用 TTL？**
- Plan 的 DAG 结构是静态的，除非修改了任务关系（version 变更），否则缓存永远有效
- TTL 会导致缓存到期后无意义的重新加载
- version 失效是精确的：只在结构真正变更时才重新构建

#### 4.2.2 缓存数据结构

```go
// internal/service/task/plan_cache.go

// CachedPlanData 缓存的 Plan 静态数据（不含执行状态）
// 注意：只缓存 DAG 结构（任务列表 + AST 解析结果），不缓存执行状态
type CachedPlanData struct {
    Plan      domain.Task              // Plan 基本信息
    Tasks     []domain.Task            // Plan 下所有子任务
    AstPlan   parser.TaskPlan          // ANTLR4 解析结果（缓存重点）
    Version   int64                    // 缓存版本号
}

// PlanCache 两层缓存实现
type PlanCache struct {
    mu       sync.RWMutex
    lru      map[int64]*CachedPlanData  // L1: 进程内缓存
    lruOrder []int64                     // LRU 淘汰顺序
    maxSize  int                         // L1 容量上限
    
    redis    redis.Cmdable              // L2: Redis 客户端
}

func NewPlanCache(redisClient redis.Cmdable, maxSize int) *PlanCache {
    return &PlanCache{
        lru:     make(map[int64]*CachedPlanData),
        maxSize: maxSize,
        redis:   redisClient,
    }
}
```

#### 4.2.3 缓存读取逻辑

```go
// Get 两层缓存读取：L1 → L2 → Origin
func (c *PlanCache) Get(ctx context.Context, planID int64) (*CachedPlanData, bool) {
    // L1: 进程内缓存
    c.mu.RLock()
    if cached, ok := c.lru[planID]; ok {
        c.mu.RUnlock()
        return cached, true
    }
    c.mu.RUnlock()

    // L2: Redis 缓存
    key := fmt.Sprintf("plan_cache:%d", planID)
    val, err := c.redis.Get(ctx, key).Bytes()
    if err == nil {
        var cached CachedPlanData
        if json.Unmarshal(val, &cached) == nil {
            // 回填 L1
            c.put(planID, &cached)
            return &cached, true
        }
    }
    return nil, false
}

// Put 写入两层缓存
func (c *PlanCache) Put(ctx context.Context, planID int64, data *CachedPlanData) {
    // L1
    c.put(planID, data)
    
    // L2
    val, _ := json.Marshal(data)
    key := fmt.Sprintf("plan_cache:%d", planID)
    c.redis.Set(ctx, key, val, 0)  // 无 TTL
}

// Invalidate 主动失效（version 变更时调用）
func (c *PlanCache) Invalidate(ctx context.Context, planID int64) {
    // L1
    c.mu.Lock()
    delete(c.lru, planID)
    c.mu.Unlock()
    
    // L2
    key := fmt.Sprintf("plan_cache:%d", planID)
    c.redis.Del(ctx, key)
}
```

#### 4.2.4 PlanService 改造

```go
type planService struct {
    repo          repository.TaskRepository
    executionRepo repository.TaskExecutionRepository
    cache         *PlanCache  // 新增
}

func (p planService) GetPlan(ctx context.Context, planID int64) (domain.Plan, error) {
    // 1. 尝试从缓存读取 DAG 静态结构
    cached, hit := p.cache.Get(ctx, planID)
    
    var plan domain.Task
    var tasks []domain.Task
    var astPlan parser.TaskPlan
    
    if hit {
        // 缓存命中：跳过 2 次 DB 查询 + ANTLR4 解析
        plan = cached.Plan
        tasks = cached.Tasks
        astPlan = cached.AstPlan
    } else {
        // 缓存未命中：走原始路径
        var err error
        plan, err = p.repo.GetByID(ctx, planID)
        if err != nil {
            return domain.Plan{}, err
        }
        tasks, err = p.repo.FindByPlanID(ctx, planID)
        if err != nil {
            return domain.Plan{}, err
        }
        astPlan, err = parser.NewAstPlan(plan.ExecExpr)
        if err != nil {
            return domain.Plan{}, err
        }
        // 写入缓存
        p.cache.Put(ctx, planID, &CachedPlanData{
            Plan:    plan,
            Tasks:   tasks,
            AstPlan: astPlan,
            Version: plan.Version,
        })
    }
    
    // 2. 执行状态仍需实时查询（不缓存，因为每次执行都不同）
    planExec, taskExecs, err := p.getExecutionData(ctx, planID)
    if err != nil {
        return domain.Plan{}, err
    }
    
    // 3. 组装完整的 Plan（用缓存的静态结构 + 实时的执行状态）
    return p.buildPlan(plan, astPlan, tasks, planExec, taskExecs)
}
```

### 4.3 性能收益估算

| 操作 | 无缓存耗时 | 有缓存耗时 | 优化幅度 |
|------|-----------|-----------|---------|
| DB 查询（Plan + Tasks） | ~2ms × 2 = 4ms | 0ms（缓存命中） | **100%** |
| ANTLR4 解析 | ~1-5ms（取决于 DSL 复杂度） | 0ms（缓存命中） | **100%** |
| 执行状态查询 | ~2ms × 2 = 4ms | ~2ms × 2 = 4ms | 不变（必须实时） |
| **总计** | ~9-13ms | ~4ms | **55-70%** |

对于一个 5 节点的 DAG，一轮执行 `GetPlan` 被调用约 5 次：
- **无缓存**：5 × 13ms = 65ms
- **有缓存**：首次 13ms + 4 × 4ms = 29ms（**节省 55%**）

### 4.4 缓存一致性保证

```go
// 在 Task 的 Create/Update 操作后主动失效缓存
func (p planService) CreateTask(ctx context.Context, planID int64, task domain.Task) error {
    // ... 创建任务逻辑
    
    // 任务变更 → 失效 Plan 缓存
    p.cache.Invalidate(ctx, planID)
    return nil
}
```

**一致性模型**：最终一致性。
- 正常路径：修改 Plan 结构 → 主动 Invalidate → 下次读取回源 → 缓存更新
- 异常路径：Invalidate 失败 → version 号不匹配 → 下次读取检测到 version 变化 → 回源重建
- 不存在脏数据风险：执行状态始终实时查询，缓存只存静态结构

---

## 5. 优化点 13.3：PlanTask 线性搜索 O(n) → O(1)

### 5.1 现状分析

**当前代码**（`internal/domain/plan.go:GetTask`）：

```go
func (p Plan) GetTask(name string) (*PlanTask, bool) {
    for idx := range p.Steps {
        if p.Steps[idx].Name == name {
            return p.Steps[idx], true
        }
    }
    return nil, false
}
```

**问题**：
- **O(n) 线性搜索**：遍历 `Steps` 切片匹配 `Name`
- **高频调用**：`NextStep` 中每个后继节点都要 `GetTask`；`CheckPre` 中每个前驱也涉及查找
- **累积效应**：一个 10 节点的 DAG，执行一轮 `NextStep` 调用约 10 次，每次 `GetTask` 最坏遍历 10 个节点 → 100 次比较
- 虽然单次遍历快（内存操作），但在大型 DAG（50+ 节点）或高频调度场景下，累积的 CPU 消耗不可忽视

### 5.2 方案设计

#### 5.2.1 构建 Name → PlanTask 索引

```go
// internal/domain/plan.go

type Plan struct {
    ID        int64
    Name      string
    // ... 现有字段

    Steps []*PlanTask
    Root  []*PlanTask
    
    // 新增：Name → PlanTask 的 O(1) 查找索引
    nameIndex map[string]*PlanTask
}

// buildIndex 在 Plan 构建完成后一次性建立索引
func (p *Plan) buildIndex() {
    p.nameIndex = make(map[string]*PlanTask, len(p.Steps))
    for _, step := range p.Steps {
        p.nameIndex[step.Name] = step
    }
}

// GetTask 使用索引的 O(1) 查找，替代原来的 O(n) 遍历
func (p Plan) GetTask(name string) (*PlanTask, bool) {
    if p.nameIndex != nil {
        task, ok := p.nameIndex[name]
        return task, ok
    }
    // 降级：索引未构建时回退到线性搜索（兼容性保证）
    for idx := range p.Steps {
        if p.Steps[idx].Name == name {
            return p.Steps[idx], true
        }
    }
    return nil, false
}
```

#### 5.2.2 在 taskToPlan 中触发索引构建

```go
// internal/service/task/plan.go:taskToPlan

func (p planService) taskToPlan(ta domain.Task, exec domain.TaskExecution, 
    tasks []domain.Task, executions map[int64]domain.TaskExecution) (domain.Plan, error) {
    
    plan := domain.Plan{
        // ... 现有字段赋值
    }
    
    // ... 构建 planTasks、设置前驱后继（现有逻辑不变）
    
    plan.Steps = planTasks

    // 新增：构建索引
    plan.buildIndex()
    
    return plan, nil
}
```

### 5.3 复杂度对比

| 操作 | 优化前 | 优化后 |
|------|--------|--------|
| `GetTask` 单次 | O(n) | O(1) 均摊 |
| `buildIndex` 一次性 | 不存在 | O(n) |
| 10 节点 DAG 一轮 NextStep | O(n²) = 100 次比较 | O(n) = 10 次 map 查找 + 10 次构建 |
| 50 节点 DAG | O(n²) = 2500 次比较 | O(n) = 50 次 map 查找 + 50 次构建 |

**空间开销**：额外的 `map[string]*PlanTask` 只存指针，10 个节点约 ~800 字节，完全可忽略。

### 5.4 注意事项

1. **`buildIndex` 在 `taskToPlan` 中调用**：这是 Plan 对象唯一的构建路径，确保所有 Plan 实例都有索引
2. **`nameIndex` 不导出**：索引是内部实现细节，不暴露给外部
3. **线性搜索降级**：`nameIndex == nil` 时自动回退，保证向后兼容
4. **值得注意的是**：`PlanTask.SetPre` 和 `SetNext` 已经在内部使用 `map[string]*PlanTask`（`taskMap`），说明 map 索引在这个场景是自然的选择

---

## 6. 优化点 13.4：Resolver 全量拉取 → 增量更新

### 6.1 现状分析

**当前代码**（`pkg/grpc/resolver.go:watch`）：

```go
func (g *executorResolver) watch() {
    events := g.registry.Subscribe(g.target.Endpoint())
    for {
        select {
        case <-events:
            g.resolve()  // ← 每次 Watch 事件都触发全量拉取
        case <-g.close:
            return
        }
    }
}
```

**`resolve` 方法**：

```go
func (g *executorResolver) resolve() {
    serviceName := g.target.Endpoint()
    ctx, cancel := context.WithTimeout(context.Background(), g.timeout)
    instances, err := g.registry.ListServices(ctx, serviceName)  // ← 全量拉取
    cancel()
    // ... 转换为 gRPC address 列表
}
```

**etcd Subscribe 的实现**（`pkg/grpc/registry/etcd/registry.go`）：

```go
for _, event := range resp.Events {
    res <- registry.Event{
        Type: typesMap[event.Type],
        // ← Instance 字段未填充！etcd 的 event.Kv.Value 被丢弃了
    }
}
```

**问题**：
- **根因**：`Subscribe` 发出的 Event 只有 `Type`（Add/Delete），没有 `Instance` 数据
- **后果**：Resolver 收到事件后，无法知道哪个实例变更了，只能调用 `ListServices` 全量拉取
- **性能影响**：假设 100 个 executor 节点，1 个节点上下线 → 触发 1 次 etcd 前缀查询 → 返回 100 条记录 → 全部转换为 gRPC Address
- **网络开销**：每个 ServiceInstance JSON 约 200 字节 × 100 = 20KB/次，高频变更时网络带宽不可忽视

### 6.2 方案设计：两步修复

#### 6.2.1 第一步：Event 结构补充 Instance 字段

**Event 结构已有 Instance 字段**（`pkg/grpc/registry/types.go`）：

```go
type Event struct {
    Type     EventType
    Instance ServiceInstance  // ← 已经定义，但 etcd 实现未填充
}
```

**修复 etcd Subscribe**（`pkg/grpc/registry/etcd/registry.go`）：

```go
func (r *Registry) Subscribe(name string) <-chan registry.Event {
    // ... 现有 context 和 watch 设置不变
    
    go func() {
        for {
            select {
            case resp := <-ch:
                if resp.Canceled {
                    return
                }
                if resp.Err() != nil {
                    continue
                }
                for _, event := range resp.Events {
                    evt := registry.Event{
                        Type: typesMap[event.Type],
                    }
                    // 修复：从 etcd event 中解析 Instance 数据
                    if event.Kv != nil && len(event.Kv.Value) > 0 {
                        var si registry.ServiceInstance
                        if err := json.Unmarshal(event.Kv.Value, &si); err == nil {
                            evt.Instance = si
                        }
                    }
                    // DELETE 事件：Kv.Value 可能为空，但 PrevKv 有数据
                    if event.Type == mvccpb.DELETE && event.PrevKv != nil {
                        var si registry.ServiceInstance
                        if err := json.Unmarshal(event.PrevKv.Value, &si); err == nil {
                            evt.Instance = si
                        }
                    }
                    res <- evt
                }
            case <-ctx.Done():
                return
            }
        }
    }()
    return res
}
```

**注意**：DELETE 事件需要启用 etcd 的 `WithPrevKV()` 选项才能获取删除前的值：

```go
ch := r.client.Watch(ctx, r.serviceKey(name), 
    clientv3.WithPrefix(),
    clientv3.WithPrevKV(),  // 新增：DELETE 事件携带删除前的 KV
)
```

#### 6.2.2 第二步：Resolver 维护本地集合做增量更新

```go
// pkg/grpc/resolver.go

type executorResolver struct {
    target   resolver.Target
    cc       resolver.ClientConn
    registry registry.Registry
    close    chan struct{}
    timeout  time.Duration
    
    // 新增：本地实例缓存，用于增量更新
    mu        sync.RWMutex
    instances map[string]registry.ServiceInstance  // key: instance.Address
}

// watch 增量更新版本
func (g *executorResolver) watch() {
    events := g.registry.Subscribe(g.target.Endpoint())
    for {
        select {
        case event := <-events:
            g.handleEvent(event)
        case <-g.close:
            return
        }
    }
}

// handleEvent 根据事件类型增量更新本地实例集合
func (g *executorResolver) handleEvent(event registry.Event) {
    g.mu.Lock()
    defer g.mu.Unlock()
    
    // 如果 Instance 为空（降级场景），回退到全量拉取
    if event.Instance.Address == "" {
        g.mu.Unlock()
        g.resolve()
        g.mu.Lock()
        return
    }
    
    switch {
    case event.Type.IsAdd():
        g.instances[event.Instance.Address] = event.Instance
    case event.Type.IsDelete():
        delete(g.instances, event.Instance.Address)
    }
    
    // 重建 gRPC 地址列表
    g.updateState()
}

// updateState 将本地实例缓存转换为 gRPC 地址列表并更新
func (g *executorResolver) updateState() {
    addresses := make([]resolver.Address, 0, len(g.instances))
    for _, ins := range g.instances {
        addresses = append(addresses, resolver.Address{
            Addr:       ins.Address,
            ServerName: ins.Name,
            Attributes: attributes.New(initCapacityStr, ins.InitCapacity).
                WithValue(maxCapacityStr, ins.MaxCapacity).
                WithValue(increaseStepStr, ins.IncreaseStep).
                WithValue(growthRateStr, ins.GrowthRate).
                WithValue(nodeIDStr, ins.ID),
        })
    }
    err := g.cc.UpdateState(resolver.State{Addresses: addresses})
    if err != nil {
        g.cc.ReportError(err)
    }
}
```

#### 6.2.3 初始化时填充本地缓存

```go
func (r *resolverBuilder) Build(target resolver.Target, cc resolver.ClientConn, _ resolver.BuildOptions) (resolver.Resolver, error) {
    res := &executorResolver{
        target:    target,
        cc:        cc,
        registry:  r.r,
        close:     make(chan struct{}, 1),
        timeout:   r.timeout,
        instances: make(map[string]registry.ServiceInstance),  // 初始化本地缓存
    }
    res.resolve()           // 首次全量拉取，填充 instances map
    go res.watch()          // 后续增量更新
    return res, nil
}

// resolve 改造：全量拉取后同步更新本地缓存
func (g *executorResolver) resolve() {
    serviceName := g.target.Endpoint()
    ctx, cancel := context.WithTimeout(context.Background(), g.timeout)
    instances, err := g.registry.ListServices(ctx, serviceName)
    cancel()
    if err != nil {
        g.cc.ReportError(err)
        return
    }
    
    // 全量替换本地缓存
    g.mu.Lock()
    g.instances = make(map[string]registry.ServiceInstance, len(instances))
    for _, ins := range instances {
        g.instances[ins.Address] = ins
    }
    g.updateState()
    g.mu.Unlock()
}
```

### 6.3 增量 vs 全量对比

| 维度 | 全量拉取（现状） | 增量更新（优化后） |
|------|----------------|-------------------|
| **etcd 查询** | 每次事件 1 次 GetPrefix | 仅首次初始化 1 次 |
| **网络开销** | O(N) 每次（N = 实例数） | O(1) 每次（单个实例数据） |
| **CPU 开销** | N 次 JSON 反序列化 + N 次 Address 构建 | 1 次 JSON 反序列化 + N 次 Address 构建 |
| **100 节点 + 10 次/分钟变更** | 1000 次 JSON 反序列化/分钟 | 10 次 JSON 反序列化/分钟 |
| **一致性** | 强一致（每次全量） | 最终一致（依赖 etcd Watch 可靠性） |
| **容错** | 天然容错（每次重建） | 需要降级机制（Instance 为空时回退全量） |

### 6.4 降级与容错

```go
// 场景 1：etcd Watch 断连重连
// etcd client 重连后会从断连点继续 Watch，不会丢失事件
// 但如果断连时间超过 etcd 的 compact interval，可能丢失历史事件
// 此时 Watch channel 会收到 Compacted 错误 → 需要全量重建

// 场景 2：Instance 数据为空（旧版本 Subscribe 或反序列化失败）
// handleEvent 中检测到 Address == "" → 自动降级为全量拉取

// 场景 3：定期校准
// 可选：每 5 分钟执行一次全量 resolve()，与本地缓存对账
// 发现差异时以全量结果为准
```

---

## 7. 设计决策分析

### 7.1 为什么优先级用数字而不是 ENUM？

| 方案 | 优点 | 缺点 |
|------|------|------|
| **ENUM（LOW/NORMAL/HIGH/CRITICAL）** | 可读性好 | 扩展性差（MySQL ALTER TABLE 加枚举值需要 DDL） |
| **数字（0/5/10/15）** | 可排序、可扩展、可比较 | 需要文档说明含义 |

**决策**：使用数字。间隔留 5 是为了将来可以插入中间级别（如 3=MEDIUM_LOW），不需要修改数据库 schema。

### 7.2 为什么 DAG 缓存不缓存执行状态？

执行状态（`TaskExecution`）每次都不同——同一个 DAG 的两次执行，各节点的完成时间、错误信息都不一样。如果缓存了执行状态，就需要在每个节点状态变更时精确更新缓存，引入复杂的并发同步问题。

**设计原则**：缓存静态的、变化频率低的数据；实时查询动态的、每次都不同的数据。

### 7.3 Resolver 增量更新是否会导致不一致？

**理论上不会**：etcd Watch 保证有序交付事件，只要处理逻辑正确（add 加入、delete 移除），本地集合就与 etcd 保持一致。

**实际风险**：Watch 断连超过 etcd compaction interval 时可能丢失事件。解决方案是检测 `resp.CompactRevision` 并触发全量重建。

---

## 8. 面试高频 Q&A

### Q1: 优先级队列为什么不用 Redis Sorted Set？

用 Redis ZSet 确实可以实现更灵活的优先级队列（score = priority * 10^13 + timestamp），但**引入了一个额外的数据存储**和一致性问题——任务的创建/更新/删除需要同步到 Redis。当前方案直接在 MySQL 查询中加 `ORDER BY priority DESC, next_time ASC`，利用**已有的联合索引**完成排序，零额外基础设施，对已有架构无侵入。

如果到了千万级任务量、MySQL 排序成为瓶颈时，才需要考虑 Redis ZSet + 异步同步的方案。

### Q2: DAG 缓存的两层设计（L1 进程内 + L2 Redis）是否过度设计？

要看部署模式：
- **单节点**：只需要 L1 进程内缓存，Redis 层确实多余
- **多节点**：L2 Redis 避免每个调度节点都回源查 DB + ANTLR 解析。假设 3 个调度节点，一个 Plan 首次加载后其他节点直接从 Redis 获取，**节省 2 次 DB 查询 + 2 次 ANTLR 解析**

当前项目是单节点 showcase，但架构上为多节点预留了扩展能力，这是面试中的加分点。

### Q3: buildIndex 的 O(n) 构建成本是否超过 O(n) 搜索的收益？

`buildIndex` 是一次性的 O(n)，而 `GetTask` 在一个 DAG 执行过程中被调用 O(n) 次（每个节点的 NextStep 都会调用）。所以总开销从 O(n²) 降到 O(n)。

对于小 DAG（3-5 个节点），差异不明显。但对于大型 DAG（50+ 节点，如数据管线），O(n²) = 2500 次 vs O(n) = 50 次构建 + 50 次 O(1) 查找，差距显著。而且 `buildIndex` 的成本可以和 DAG 缓存叠加——缓存命中时 `buildIndex` 也不需要执行。

### Q4: Resolver 增量更新如何处理 etcd Watch 断连？

etcd client v3 内置了自动重连和 Watch 恢复机制。正常断连重连后，会从上次的 revision 继续 Watch，不会丢失事件。

极端情况：断连时间超过 etcd 的 `--auto-compaction-retention`（默认 5 分钟），历史 revision 被压缩，Watch 会返回 `ErrCompacted`。此时需要：
1. 捕获 `resp.CompactRevision > 0`
2. 调用 `resolve()` 全量重建本地缓存
3. 以新的 revision 重新开始 Watch

这和 Kafka consumer 的 offset 过期重置逻辑类似——正常路径走增量，异常路径降级为全量。

### Q5: 四个优化的实施优先级如何排？

| 优先级 | 优化点 | 理由 |
|--------|--------|------|
| **P0** | 13.3 O(1) 索引 | 改动最小（~20 行），零风险，立即生效 |
| **P1** | 13.1 优先级队列 | 改动小（SQL + Domain 字段），但需要数据库 migration |
| **P2** | 13.4 增量更新 | 改动中等，需要处理降级逻辑 |
| **P3** | 13.2 DAG 缓存 | 改动最大，涉及缓存一致性，需要充分测试 |

原则：先做**高收益低风险**的（索引、排序），再做**高收益高复杂度**的（缓存、增量更新）。

---

## 9. 实现局限与改进方向

| # | 现状 | 改进方案 | 优先级 |
|---|------|---------|--------|
| 1 | 优先级只支持 4 个固定级别 | 支持自定义优先级区间，或动态优先级（随等待时间提升） | 低 |
| 2 | DAG 缓存未考虑内存上限 | LRU 淘汰策略 + 缓存指标监控（命中率、淘汰次数） | 中 |
| 3 | Resolver 增量更新无定期校准 | 添加 5 分钟一次的全量对账机制 | 中 |
| 4 | 优先级队列无饥饿保护 | 低优任务等待超过阈值后自动提升优先级（aging 机制） | 低 |
| 5 | 缓存 Invalidate 在分布式场景下需要广播 | 使用 Redis Pub/Sub 通知其他节点失效本地 L1 缓存 | 中 |
| 6 | buildIndex 每次 `taskToPlan` 都重建 | 结合 DAG 缓存，索引也被缓存，进一步减少构建次数 | 低 |
