# Step 6：分片任务

> **目标**：在 Step 5 的 DAG 工作流引擎之上，构建**分片规则定义 → 子任务自动拆分 → 多节点并行执行 → 异步状态聚合**的完整分片任务框架，支持 Range 静态分片和 WeightedDynamicRange 加权动态分片两种策略。
>
> **完成后你能看到**：启动 MySQL + etcd + Kafka → 启动调度器 → 启动 3 个执行器节点 → 插入一个 Range 分片任务（totalNums=3, step=10000）→ 调度器抢占任务 → NormalTaskRunner 检测到 ShardingRule → 调用 CreateShardingChildren 拆分出 3 个子执行记录 → 每个子任务通过 gRPC 指定节点执行 → 子任务完成后 ShardingCompensator 扫描父执行记录 → 聚合所有子任务状态 → 父任务标记 SUCCESS → 释放锁 → 更新下次调度时间。

---

## 1. 架构总览

Step 6 在 NormalTaskRunner 之上引入了一个完整的**分片任务层**。核心思路：Task 携带 ShardingRule 定义分片策略，NormalTaskRunner 检测到 ShardingRule 后走分片分支——创建父执行记录 + N 个子执行记录，每个子任务通过 gRPC 指定节点并行执行，子任务完成时**不释放锁**，由 ShardingCompensator 异步扫描聚合结果，统一释放。

```
┌──────────────────────────────────────────────────────────────────────────────┐
│                            Scheduler 进程                                    │
│                                                                              │
│  ┌──────────────────────────────────────────────────────────────────────┐    │
│  │                      NormalTaskRunner.Run()                          │    │
│  │                                                                      │    │
│  │   task.ShardingRule == nil?                                          │    │
│  │         │                                                            │    │
│  │    ┌────┴────┐                                                       │    │
│  │    Yes       No                                                      │    │
│  │    │         │                                                       │    │
│  │    ▼         ▼                                                       │    │
│  │  handleNormalTask()    handleShardingTask()                          │    │
│  │  (Step 1~5 流程)       │                                             │    │
│  │                        ▼                                             │    │
│  │              ┌─────────────────────────────┐                         │    │
│  │              │ ExecutionService             │                         │    │
│  │              │ .CreateShardingChildren()    │                         │    │
│  │              │                              │                         │    │
│  │              │ 1. invoker.Prepare()         │ ← WeightedDynamic 才用  │    │
│  │              │ 2. registry.ListServices()   │ ← 获取执行器节点+权重   │    │
│  │              │ 3. ShardingRule.              │                         │    │
│  │              │    ToScheduleParams()        │ ← 按策略拆分参数        │    │
│  │              │ 4. repo.CreateShardingParent  │ ← 父记录 parentID=0    │    │
│  │              │ 5. repo.CreateShardingChildren│ ← N 个子记录           │    │
│  │              └──────────────┬───────────────┘                         │    │
│  │                             │                                         │    │
│  │                             ▼                                         │    │
│  │              ┌─────────────────────────────┐                         │    │
│  │              │  并行 goroutine × N          │                         │    │
│  │              │                              │                         │    │
│  │              │  for each child:             │                         │    │
│  │              │    WithSpecificNodeIDContext  │ ← 节点亲和性            │    │
│  │              │    invoker.Run(childExec)     │ ← gRPC → 指定节点执行  │    │
│  │              └──────────────┬───────────────┘                         │    │
│  │                             │                                         │    │
│  └─────────────────────────────┼────────────────────────────────────────┘    │
│                                │                                             │
│                                ▼                                             │
│  ┌──────────────────────────────────────────────────────────────────────┐    │
│  │                     Executor Node × N                                │    │
│  │                                                                      │    │
│  │  接收 gRPC Execute → 执行分片数据范围 → Report 状态                  │    │
│  └──────────────────────────────┬───────────────────────────────────────┘    │
│                                 │                                            │
│                                 ▼ Kafka produce / gRPC Report               │
│  ┌──────────────────────────────────────────────────────────────────────┐    │
│  │                     UpdateState (Terminal)                            │    │
│  │                                                                      │    │
│  │  isShardedTask (ShardingParentID > 0)?                               │    │
│  │    Yes → 跳过 releaseTask + UpdateNextTime（交给 Compensator）       │    │
│  │    No  → 正常释放 + 更新下次时间                                      │    │
│  └──────────────────────────────────────────────────────────────────────┘    │
│                                                                              │
│  ┌──────────────────────────────────────────────────────────────────────┐    │
│  │                     ShardingCompensator                               │    │
│  │                     （定时轮询）                                       │    │
│  │                                                                      │    │
│  │  1. FindShardingParents (status=RUNNING, parentID=0)                 │    │
│  │  2. 遍历每个 parent:                                                  │    │
│  │     FindShardingChildren(parentID)                                   │    │
│  │     → 所有子任务都终态？                                              │    │
│  │       No  → 跳过（等下次扫描）                                        │    │
│  │       Yes → anyFailed? → parent=FAILED                               │    │
│  │             allSuccess? → parent=SUCCESS                              │    │
│  │  3. releaseTask() + UpdateNextTime()                                 │    │
│  └──────────────────────────────────────────────────────────────────────┘    │
└──────────────────────────────────────────────────────────────────────────────┘
```

---

## 2. Step 5 → Step 6 变化对比

| 维度 | Step 5 (DAG 工作流) | Step 6 (分片任务) |
|------|---------------------|-------------------|
| 核心能力 | 多任务 DAG 编排与依赖推进 | 单任务拆分为 N 个分片并行执行 |
| Task 新增字段 | `ExecExpr` (DSL 表达式) | `ShardingRule` (分片规则 JSON) |
| TaskExecution 新增字段 | `PlanID`, `PlanExecID` | `ShardingParentID *int64` (三态语义) |
| Runner 变化 | 新增 PlanTaskRunner | NormalTaskRunner 增加 `handleShardingTask()` 分支 |
| 状态聚合 | 事件驱动（Kafka CompleteConsumer → NextStep） | 轮询驱动（ShardingCompensator 定时扫描） |
| 节点路由 | 负载均衡自动分配 | `WithSpecificNodeIDContext` 指定节点亲和 |
| 锁释放时机 | 子任务完成时事件驱动释放 | 子任务完成时**不释放**，Compensator 聚合后统一释放 |
| 新增组件 | PlanService, DSL 解析层, CompleteConsumer | ShardingRule 领域模型, ShardingCompensator, Invoker.Prepare |

---

## 3. 设计决策分析

### 3.1 为什么需要两种分片策略？

**Range（静态等分）**——适用于数据范围已知、分布均匀的场景：

- 参数：`totalNums`（分片数）、`step`（每片数据量）
- 生成：`[{start:0, end:step}, {start:step, end:2*step}, ...]`
- 特点：不依赖执行器节点信息，部署简单

**WeightedDynamicRange（加权动态分）**——适用于节点性能不均、需按权重分配负载的场景：

- 参数：`total_tasks`（总数据量）
- 额外依赖：执行器节点列表 + 每个节点的权重
- 生成：按权重比例分配范围，最后一个节点吃掉余数避免精度丢失
- 特点：需要 `Invoker.Prepare()` 获取业务元数据 + `registry.ListServices()` 获取节点权重

```go
// internal/domain/task.go — 两种策略的核心逻辑

// Range: 简单等分
func (r *RangeShardingRule) ToScheduleParams() []map[string]string {
    totalNums, _ := strconv.Atoi(r.Params["totalNums"])
    step, _ := strconv.Atoi(r.Params["step"])
    res := make([]map[string]string, 0, totalNums)
    for i := 0; i < totalNums; i++ {
        res = append(res, map[string]string{
            "start": strconv.Itoa(i * step),
            "end":   strconv.Itoa((i + 1) * step),
        })
    }
    return res
}

// WeightedDynamicRange: 按节点权重分配
func (r *WeightedDynamicRangeShardingRule) ToScheduleParams() []map[string]string {
    totalTasks, _ := strconv.Atoi(r.Params["total_tasks"])
    totalWeight := 0
    for _, inst := range r.ExecutorNodeInstances {
        totalWeight += inst.Weight
    }
    currentStart := 0
    res := make([]map[string]string, 0, len(r.ExecutorNodeInstances))
    for i, inst := range r.ExecutorNodeInstances {
        var count int
        if i == len(r.ExecutorNodeInstances)-1 {
            count = totalTasks - currentStart // 最后一个吃余数
        } else {
            count = totalTasks * inst.Weight / totalWeight
        }
        end := currentStart + count
        res = append(res, map[string]string{
            "start": strconv.Itoa(currentStart),
            "end":   strconv.Itoa(end),
        })
        currentStart = end
    }
    return res
}
```

**设计取舍**：
- Range 的 `totalNums` 和 `step` 是静态写死的，无法自动适应节点增减。如果 3 个节点变成 2 个，仍会生成 3 个子任务，其中一个必须由某节点多执行一份
- WeightedDynamicRange 每次调度都会重新查询节点列表和权重，天然适应弹性扩缩容
- 未来可扩展 Hash 分片、一致性哈希等策略，只需实现 `ToScheduleParams()` 接口

### 3.2 ShardingParentID 的三态设计

```
ShardingParentID = nil     →  普通任务（非分片）
ShardingParentID = *0      →  分片父任务（聚合节点）
ShardingParentID = *parentID (>0) → 分片子任务（指向父）
```

**为什么用 `*int64` 而不是 `int64`？**

`int64` 的零值是 `0`，无法区分"没设置"和"值为 0"。但分片场景下需要三种语义：
- 普通任务：`ShardingParentID IS NULL`（数据库层直接过滤）
- 父任务：`ShardingParentID = 0`（Compensator 扫描入口）
- 子任务：`ShardingParentID = {parentID}`（聚合时关联查找）

指针类型 `*int64` 天然支持 `nil / *0 / *N` 三态，映射到数据库的 `NULL / 0 / N`。

```sql
-- 数据库层的查询充分利用了三态语义
-- 查找所有分片父任务（Compensator 入口）
SELECT * FROM task_executions
WHERE sharding_parent_id = 0 AND status = 'RUNNING'
ORDER BY utime ASC;

-- 查找某父任务的所有子任务
SELECT * FROM task_executions
WHERE sharding_parent_id = ?;

-- 查找可重调度的执行记录（排除父任务本身）
SELECT * FROM task_executions
WHERE status = 'FAILED_RESCHEDULED'
  AND (sharding_parent_id IS NULL OR sharding_parent_id > 0);
```

### 3.3 为什么用 Compensator 轮询而不是事件驱动？

Step 5 的 DAG 工作流用的是**事件驱动**（Kafka CompleteConsumer → NextStep），为什么分片任务不复用这个模式？

**对比分析**：

| 维度 | 事件驱动（Step 5 DAG） | Compensator 轮询（Step 6 分片） |
|------|----------------------|-------------------------------|
| 触发时机 | 每个子任务完成即触发 | 定时批量扫描 |
| 聚合逻辑 | 检查单个后继节点的前置依赖 | 需要查询**所有**子任务状态 |
| 复杂度 | O(1) 每次事件 | O(N) 每次扫描一个父任务 |
| 容错性 | 事件丢失 = 流程卡住 | 天然幂等，丢了下次扫描还会补上 |
| 延迟 | 实时（毫秒级） | 取决于扫描间隔（秒级） |

**选择 Compensator 的核心原因**：

1. **聚合语义不同**：DAG 的 NextStep 只需检查"当前节点的前置都完成了吗？"——这是局部判断。分片的聚合是"所有 N 个子任务都终态了吗？"——这是全局判断。在事件驱动模型下，每个子任务完成时都要查一遍其他所有兄弟的状态，本质上也是 O(N) 查询，而且还有并发问题（两个子任务同时完成，都认为自己是最后一个）
2. **天然幂等与容错**：Compensator 每次扫描都是全量判断，不怕 Kafka 消息丢失或重复消费，不需要额外的去重逻辑
3. **锁管理简化**：分片场景下锁的持有者是父任务，所有子任务共享这把锁。如果事件驱动释放，需要解决"哪个子任务来释放"的竞争问题。Compensator 天然单点决策

**代价**：聚合延迟从毫秒级变成秒级。对于大数据分片场景，子任务执行本身就要分钟到小时级，秒级聚合延迟完全可以接受。

### 3.4 V1 单表 vs V2 分库分表

项目实现了两个版本的 ShardingCompensator：

- **V1**（`internal/compensator/sharding.go`）：单库单表，简单的 offset 分页轮询
- **V2**（`internal/compensator/shardingv2.go`）：多库多表，基于 ShardingLoopJob 框架的分布式轮询

V2 的引入是为了应对执行记录量级增长后的扩展性需求。当任务量达到千万级，单表存储执行记录成为瓶颈，需要将 `task_executions` 表按 taskID 分库分表。

这是一个完整的分库分表方案，包括：
- `pkg/sharding/strategy.go`：分片路由策略（db_hash + table_hash）
- `pkg/loopjob/shardingloopjob.go`：分片 LoopJob 框架（分布式锁 + 并发控制）
- `internal/repository/dao/sharding_task_execution.go`：分片 DAO（按 DB 分组批量写入）
- `scripts/mysql/init.sql`：V2 建库建表脚本（2 库 × 2 任务表 × 4 执行表）

> 后续第 4~6 节将按层详细分析。

---

## 4. 分片规则领域层

### 4.1 ShardingRule 结构

```go
// internal/domain/task.go

type ShardingRuleType string

const (
    ShardingRuleTypeRange                ShardingRuleType = "range"
    ShardingRuleTypeWeightedDynamicRange ShardingRuleType = "weighted-dynamic-range"
)

type ShardingRule struct {
    Type                  ShardingRuleType
    Params                map[string]string
    ExecutorNodeInstances []registry.ServiceInstance  // 运行时注入（WeightedDynamic 用）
}
```

`ShardingRule` 是 Task 的一个字段，序列化为 JSON 存储在 `tasks.sharding_rule` 列。`Type` 决定分片策略，`Params` 存储策略参数，`ExecutorNodeInstances` 是运行时注入的执行器节点列表（仅 WeightedDynamicRange 需要）。

### 4.2 策略分发

```go
// internal/domain/task.go

func (s *ShardingRule) ToScheduleParams() []map[string]string {
    switch s.Type {
    case ShardingRuleTypeRange:
        return (&RangeShardingRule{Params: s.Params}).ToScheduleParams()
    case ShardingRuleTypeWeightedDynamicRange:
        return (&WeightedDynamicRangeShardingRule{
            Params:                s.Params,
            ExecutorNodeInstances: s.ExecutorNodeInstances,
        }).ToScheduleParams()
    default:
        return nil
    }
}
```

简单的策略模式分发，未来新增分片类型只需加 case + 实现 `ToScheduleParams()`。

### 4.3 单元测试验证

```go
// internal/domain/sharding_rule_test.go

func TestRangeShardingRule(t *testing.T) {
    rule := &ShardingRule{
        Type:   ShardingRuleTypeRange,
        Params: map[string]string{"totalNums": "3", "step": "100000"},
    }
    params := rule.ToScheduleParams()
    assert.Equal(t, 3, len(params))
    assert.Equal(t, "0", params[0]["start"])
    assert.Equal(t, "100000", params[0]["end"])
    assert.Equal(t, "100000", params[1]["start"])
    assert.Equal(t, "200000", params[1]["end"])
    assert.Equal(t, "200000", params[2]["start"])
    assert.Equal(t, "300000", params[2]["end"])
}

func TestWeightedDynamicRangeShardingRule(t *testing.T) {
    rule := &ShardingRule{
        Type:   ShardingRuleTypeWeightedDynamicRange,
        Params: map[string]string{"total_tasks": "1000"},
        ExecutorNodeInstances: []registry.ServiceInstance{
            {Weight: 20}, {Weight: 30}, {Weight: 50},
        },
    }
    params := rule.ToScheduleParams()
    assert.Equal(t, 3, len(params))
    assert.Equal(t, "0", params[0]["start"])
    assert.Equal(t, "200", params[0]["end"])      // 20%
    assert.Equal(t, "200", params[1]["start"])
    assert.Equal(t, "500", params[1]["end"])       // 30%
    assert.Equal(t, "500", params[2]["start"])
    assert.Equal(t, "1000", params[2]["end"])      // 50%（含余数）
}
```

---

## 5. NormalTaskRunner 分片分支

### 5.1 Run() 的分支判断

```go
// internal/service/runner/normal_task_runner.go

func (r *NormalTaskRunner) Run(ctx context.Context, t domain.Task) error {
    // 1. 抢占任务
    exec, err := r.execSvc.Acquire(ctx, t)
    if err != nil { return err }

    // 2. 分支：有 ShardingRule → 走分片，否则走普通
    if t.ShardingRule == nil {
        return r.handleNormalTask(ctx, t, exec)
    }
    return r.handleShardingTask(ctx, t, exec)
}
```

判断极其简单：Task 有没有配置 ShardingRule。这是因为分片规则在任务创建时就已经确定，不存在运行时动态变化的情况。

### 5.2 handleShardingTask() 全流程

```go
// internal/service/runner/normal_task_runner.go

func (r *NormalTaskRunner) handleShardingTask(
    ctx context.Context, t domain.Task, exec domain.TaskExecution,
) error {
    // 1. 创建分片子执行记录（包含父记录创建）
    childExecs, err := r.execSvc.CreateShardingChildren(ctx, t, exec)
    if err != nil { return err }

    // 2. 并行发起每个子任务的执行
    var wg sync.WaitGroup
    for _, childExec := range childExecs {
        wg.Add(1)
        ce := childExec  // 闭包陷阱
        go func() {
            defer wg.Done()
            // 指定目标节点（节点亲和性）
            childCtx := ctx
            if ce.ExecutorNodeID != "" {
                childCtx = invoker.WithSpecificNodeIDContext(ctx, ce.ExecutorNodeID)
            }
            _ = r.invoker.Run(childCtx, t, ce)
        }()
    }
    wg.Wait()
    return nil
}
```

**关键设计点**：

1. **goroutine 并行**：N 个子任务同时发起 gRPC 调用，充分利用多执行器节点的并行能力
2. **节点亲和性**：通过 `WithSpecificNodeIDContext` 将 gRPC 请求路由到指定节点，确保分片数据由对应节点处理
3. **错误忽略**：`_ = r.invoker.Run()` 不处理错误——因为子任务的状态上报走独立通道（Kafka / gRPC Report），即使 goroutine 里的 Run 调用失败，子任务也会被超时补偿机制捕获
4. **WaitGroup 等待**：等所有 goroutine 发起调用后才返回。注意这里等的是"发起"而不是"完成"，因为 `invoker.Run()` 是阻塞到远程执行器接收请求，不是等执行结束

### 5.3 上下文注入：节点路由

```go
// internal/service/runner/normal_task_runner.go

func WithSpecificNodeIDContext(ctx context.Context, nodeID string) context.Context {
    return context.WithValue(ctx, balancer.SpecificNodeID{}, nodeID)
}

func WithExcludedNodeIDContext(ctx context.Context, nodeID string) context.Context {
    return context.WithValue(ctx, balancer.ExcludedNodeID{}, nodeID)
}
```

两种上下文注入服务于不同场景：
- `SpecificNodeID`：分片任务指定节点执行，由 gRPC `RoutingPicker` 消费
- `ExcludedNodeID`：重调度时排除上次失败的节点（Step 4 的能力），避免重蹈覆辙

### 5.4 分片任务的 Retry/Reschedule 特殊处理

```go
// internal/service/runner/normal_task_runner.go

func (r *NormalTaskRunner) isShardedTaskExecution(exec domain.TaskExecution) bool {
    return exec.ShardingParentID != nil && *exec.ShardingParentID > 0
}
```

当一个子任务需要 Retry 或 Reschedule 时：

- **跳过重新抢占**：子任务不需要独立抢锁，因为父任务还持有锁（直到 Compensator 聚合完成才释放）
- **正常执行重试逻辑**：创建新的执行记录、发起远程调用等流程不变
- **重调度时排除节点**：如果子任务在 NodeA 失败了，重调度时通过 `WithExcludedNodeIDContext` 避开 NodeA

---

## 6. ExecutionService.CreateShardingChildren

这是分片任务的**核心编排方法**，完成了从规则到执行记录的全部转换。

### 6.1 完整流程

```go
// internal/service/task/execution_service.go

func (s *executionService) CreateShardingChildren(
    ctx context.Context, t domain.Task, exec domain.TaskExecution,
) ([]domain.TaskExecution, error) {
    shardingRule := t.ShardingRule

    // ① WeightedDynamicRange 需要先获取业务元数据 + 节点列表
    if shardingRule.Type == domain.ShardingRuleTypeWeightedDynamicRange {
        // 调用 Invoker.Prepare 获取执行器的业务元数据
        bizMeta, err := s.invoker.Prepare(ctx, t)
        if err != nil { return nil, err }
        // 合并业务元数据到分片参数
        for k, v := range bizMeta {
            shardingRule.Params[k] = v
        }
        // 获取注册中心的执行器节点列表（含权重信息）
        instances, err := s.registry.ListServices(ctx, t.Cfg.ServiceName())
        if err != nil { return nil, err }
        shardingRule.ExecutorNodeInstances = instances
    }

    // ② 根据策略计算分片参数
    scheduleParams := shardingRule.ToScheduleParams()
    if len(scheduleParams) == 0 {
        return nil, errs.ErrTaskShardingRuleNotFound
    }

    // ③ 收集每个分片的目标节点 ID
    executorNodeIDs := make([]string, len(scheduleParams))
    for i, inst := range shardingRule.ExecutorNodeInstances {
        if i >= len(scheduleParams) { break }
        executorNodeIDs[i] = inst.Address
    }

    // ④ 创建父执行记录（ShardingParentID = 0）
    parentExec, err := s.repo.CreateShardingParent(ctx, exec)
    if err != nil { return nil, err }

    // ⑤ 创建 N 个子执行记录
    children, err := s.repo.CreateShardingChildren(
        ctx, parentExec, scheduleParams, executorNodeIDs,
    )
    return children, err
}
```

### 6.2 Invoker.Prepare 的作用

```go
// internal/service/invoker/grpc.go

func (g *GRPCInvoker) Prepare(ctx context.Context, t domain.Task) (map[string]string, error) {
    resp, err := g.client.Prepare(ctx, &executorv1.PrepareRequest{
        TaskId: t.ID,
    })
    if err != nil { return nil, err }
    return resp.Metadata, nil
}
```

`Prepare` 是 WeightedDynamicRange 专用的**预查询步骤**。执行器节点返回业务维度的元数据（比如总数据量），调度器据此动态计算分片范围。这解耦了"分片策略"和"业务数据量"：

- 任务创建时不需要知道精确的数据量
- 每次调度前动态获取最新数据量
- 执行器可以根据实际情况返回不同的元数据

对于 Range 策略，`totalNums` 和 `step` 已经在 ShardingRule.Params 里静态配置好了，不需要 Prepare。

### 6.3 Repository 层：CreateShardingParent + CreateShardingChildren

```go
// internal/repository/task_execution.go

func (r *taskExecutionRepository) CreateShardingParent(
    ctx context.Context, exec domain.TaskExecution,
) (domain.TaskExecution, error) {
    entity := r.toEntity(exec)
    entity.Status = domain.ExecStatusRunning  // 父任务直接进入 RUNNING
    parentID := int64(0)
    entity.ShardingParentID = &parentID       // 标记为父任务
    err := r.dao.CreateShardingParent(ctx, entity)
    if err != nil { return domain.TaskExecution{}, err }
    return r.toDomain(entity), nil
}

func (r *taskExecutionRepository) CreateShardingChildren(
    ctx context.Context,
    parent domain.TaskExecution,
    scheduleParams []map[string]string,
    executorNodeIDs []string,
) ([]domain.TaskExecution, error) {
    parentEntity := r.toEntity(parent)
    parentID := parent.ID

    children := make([]dao.TaskExecution, 0, len(scheduleParams))
    for i, params := range scheduleParams {
        child := parentEntity             // 值拷贝！复制父任务的基本信息
        child.ID = 0                      // 清零，由数据库生成
        child.Status = domain.ExecStatusPrepare
        child.ShardingParentID = &parentID  // 指向父任务

        // 指定执行器节点
        if i < len(executorNodeIDs) {
            child.ExecutorNodeID = executorNodeIDs[i]
        }

        // 合并分片参数到调度参数
        mergedParams := make(map[string]string)
        for k, v := range parent.ScheduleParams {
            mergedParams[k] = v           // 继承父任务的公共参数
        }
        for k, v := range params {
            mergedParams[k] = v           // 覆盖/追加分片特有参数
        }
        raw, _ := json.Marshal(mergedParams)
        child.ScheduleParams = string(raw)

        children = append(children, child)
    }

    err := r.dao.BatchCreate(ctx, children)
    if err != nil { return nil, err }

    result := make([]domain.TaskExecution, 0, len(children))
    for _, c := range children {
        result = append(result, r.toDomain(c))
    }
    return result, nil
}
```

**值拷贝设计**：子执行记录从父记录值拷贝而来（`child := parentEntity`），继承了 TaskID、ConfigSnapshot 等所有字段，然后只覆盖差异字段（ID、Status、ShardingParentID、ExecutorNodeID、ScheduleParams）。这避免了遗漏字段的风险。

**参数合并**：子任务的 ScheduleParams = 父任务公共参数 + 分片特有参数。分片参数（start/end）覆盖同名公共参数。

---

## 7. 状态上报：分片子任务的特殊处理

### 7.1 UpdateState 中的分片判断

```go
// internal/service/task/execution_service.go — UpdateState 方法（精简）

func (s *executionService) UpdateState(
    ctx context.Context, state domain.ExecutionState,
) error {
    // ... 更新执行记录状态 ...

    // 终态处理
    if state.IsTerminalStatus() {
        isShardedTask := exec.ShardingParentID != nil && *exec.ShardingParentID > 0

        if !isShardedTask {
            // 普通任务 / 分片父任务：正常释放锁 + 更新下次调度时间
            s.releaseTask(ctx, exec.TaskID)
            s.taskSvc.UpdateNextTime(ctx, task)
        }
        // 分片子任务：不释放锁，不更新 NextTime
        // → 由 ShardingCompensator 聚合完成后统一处理

        // 但无论是否分片，都发送完成事件
        s.producer.ProduceCompleteEvent(ctx, completeEvent)
    }
    return nil
}
```

**关键区分**：
- `isShardedTask = true`（ShardingParentID > 0）：跳过 `releaseTask` 和 `UpdateNextTime`，因为锁属于父任务，释放权归 Compensator
- `isShardedTask = false`（ShardingParentID == nil 或 == 0）：正常处理

**为什么父任务（ShardingParentID == 0）也走正常释放路径？** 因为父任务自身的状态是由 Compensator 更新的，Compensator 在 `handle()` 里会自己调用 `releaseTask` 和 `UpdateNextTime`。父任务不会进到 `UpdateState` 的终态分支——它的状态直接被 Compensator 写入。

---

## 8. ShardingCompensator：异步状态聚合

### 8.1 V1：单表版本

```go
// internal/compensator/sharding.go

type ShardingCompensator struct {
    nodeID        string
    batchSize     int
    sleepInterval time.Duration
    taskSvc       service.TaskService
    execSvc       service.ExecutionService
    taskAcquirer  service.TaskAcquirer
}

func (c *ShardingCompensator) Start(ctx context.Context) {
    offset := 0
    for {
        select {
        case <-ctx.Done():
            return
        default:
        }

        // 分页查询所有 RUNNING 状态的分片父任务
        parents, err := c.execSvc.FindShardingParents(ctx, offset, c.batchSize)
        if err != nil {
            time.Sleep(c.sleepInterval)
            offset = 0
            continue
        }
        if len(parents) == 0 {
            time.Sleep(c.sleepInterval)
            offset = 0
            continue
        }

        for _, parent := range parents {
            c.handle(ctx, parent)
        }
        offset += len(parents)
    }
}
```

V1 的 `Start()` 是一个简单的 offset 分页无限循环：
1. 查一批 RUNNING 状态的父任务（ShardingParentID = 0）
2. 逐个处理
3. 处理完一批后 offset 前移，继续查下一批
4. 查不到了就 sleep 一会儿，重置 offset 从头来

### 8.2 handle() 聚合逻辑

```go
// internal/compensator/sharding.go

func (c *ShardingCompensator) handle(
    ctx context.Context, parent domain.TaskExecution,
) {
    // 查询该父任务的所有子任务
    children, err := c.execSvc.FindShardingChildren(ctx, parent.ID)
    if err != nil { return }

    // 边缘情况：没有子任务 → 强制标记失败
    if len(children) == 0 {
        c.execSvc.UpdateState(ctx, domain.ExecutionState{
            ID:     parent.ID,
            Status: domain.ExecStatusFailed,
        })
        c.releaseAndUpdateNextTime(ctx, parent)
        return
    }

    // 判断是否所有子任务都到终态
    anyFailed := false
    allTerminal := true
    successCount := 0
    for _, child := range children {
        if !child.IsTerminalStatus() {
            allTerminal = false
            break
        }
        if child.Status == domain.ExecStatusFailed {
            anyFailed = true
        }
        if child.Status == domain.ExecStatusSuccess {
            successCount++
        }
    }

    // 还有子任务没结束 → 跳过，等下次扫描
    if !allTerminal {
        // 更新进度 progress = (successCount * 100) / len(children)
        progress := successCount * 100 / len(children)
        c.execSvc.UpdateProgress(ctx, parent.ID, progress)
        return
    }

    // 所有子任务都终态 → 聚合
    var finalStatus string
    if anyFailed {
        finalStatus = domain.ExecStatusFailed
    } else {
        finalStatus = domain.ExecStatusSuccess
    }

    c.execSvc.UpdateState(ctx, domain.ExecutionState{
        ID:     parent.ID,
        Status: finalStatus,
    })
    c.releaseAndUpdateNextTime(ctx, parent)
}

func (c *ShardingCompensator) releaseAndUpdateNextTime(
    ctx context.Context, parent domain.TaskExecution,
) {
    // 释放父任务的锁
    c.taskAcquirer.Release(ctx, parent.TaskID)
    // 更新下次调度时间
    task, _ := c.taskSvc.GetByID(ctx, parent.TaskID)
    c.taskSvc.UpdateNextTime(ctx, task)
}
```

**聚合规则**：
- 任一子任务 FAILED → 父任务 FAILED
- 全部子任务 SUCCESS → 父任务 SUCCESS
- 还有子任务未终态 → 更新 progress 后跳过

**进度计算**：`progress = (successCount * 100) / len(children)`，整数除法，粒度到百分比。

### 8.3 V2：分库分表版本

```go
// internal/compensator/shardingv2.go

type ShardingCompensatorV2 struct {
    nodeID        string
    batchSize     int
    sleepInterval time.Duration
    taskSvc       service.TaskService
    execSvc       service.ExecutionService
    taskAcquirer  service.TaskAcquirer
    offsets       syncx.Map[string, int]  // 每个分片表维护独立 offset
}
```

V2 和 V1 的 `handle()` 聚合逻辑**完全相同**，区别在于外层驱动：

- V1：单线程 offset 分页循环
- V2：使用 `ShardingLoopJob` 框架，每个分片（db×table）独立处理

```go
func (c *ShardingCompensatorV2) Start(ctx context.Context) {
    job := loopjob.NewShardingLoopJob(
        // ... 配置 ...
        bizFunc: func(ctx context.Context) error {
            dst := sharding.DstFromCtx(ctx)
            key := dst.DB + ":" + dst.Table
            offset, _ := c.offsets.Load(key)

            parents, err := c.execSvc.FindShardingParents(ctx, offset, c.batchSize)
            if err != nil { return err }
            if len(parents) == 0 {
                c.offsets.Store(key, 0)
                return loopjob.ErrNoMoreData
            }

            for _, parent := range parents {
                c.handle(ctx, parent)
            }
            c.offsets.Store(key, offset+len(parents))
            return nil
        },
    )
    job.Run(ctx)
}
```

V2 通过 `syncx.Map[string, int]` 为每个分片表维护独立的 offset，避免跨分片的偏移量混乱。

---

## 9. ShardingLoopJob 框架

这是 V2 的底层驱动框架，提供了**多分片并发处理 + 分布式锁 + 资源控制**的能力。

### 9.1 双层循环结构

```go
// pkg/loopjob/shardingloopjob.go

func (s *ShardingLoopJob) Run(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            return
        default:
        }

        // 外层循环：遍历所有分片
        dsts := s.shardingStrategy.Broadcast()
        for _, dst := range dsts {
            // 资源信号量控制并发数
            if err := s.resourceSemaphore.Acquire(); err != nil {
                break  // 达到并发上限，等下一轮
            }

            // 获取分布式锁
            key := fmt.Sprintf("%s:%s:%s", s.baseKey, dst.DB, dst.Table)
            lock := s.dclient.NewLock(key)
            if err := lock.Lock(ctx); err != nil {
                s.resourceSemaphore.Release()
                continue  // 抢锁失败，其他节点在处理，跳过
            }

            // 启动 goroutine 处理该分片
            go s.tableLoop(ctx, dst, lock)
        }

        time.Sleep(s.interval)
    }
}
```

### 9.2 tableLoop：单分片处理

```go
func (s *ShardingLoopJob) tableLoop(
    ctx context.Context, dst sharding.Dst, lock dclient.Lock,
) {
    defer func() {
        // 用 Background context 解锁，避免 ctx 取消后无法释放锁
        lock.Unlock(context.Background())
        s.resourceSemaphore.Release()
    }()

    shardCtx := sharding.CtxWithDst(ctx, dst)

    for {
        select {
        case <-ctx.Done():
            return
        default:
        }

        err := s.bizLoop(shardCtx, lock)
        if err != nil {
            return  // 业务出错或无数据，退出该分片
        }
    }
}
```

### 9.3 bizLoop：带锁续期的业务执行

```go
func (s *ShardingLoopJob) bizLoop(ctx context.Context, lock dclient.Lock) error {
    // 50 秒超时（留 10 秒给锁续期）
    bizCtx, cancel := context.WithTimeout(ctx, 50*time.Second)
    defer cancel()

    // 并发：业务执行 + 锁续期
    done := make(chan error, 1)
    go func() {
        done <- s.bizFunc(bizCtx)
    }()

    // 锁续期 ticker
    ticker := time.NewTicker(s.lockRefreshInterval)
    defer ticker.Stop()

    for {
        select {
        case err := <-done:
            return err
        case <-ticker.C:
            lock.Refresh(ctx)  // 续期分布式锁
        case <-bizCtx.Done():
            return bizCtx.Err()
        }
    }
}
```

**设计要点**：

1. **资源信号量**（`MaxCntResourceSemaphore`）：控制单个节点最多同时处理多少个分片，避免资源耗尽
2. **分布式锁**：每个分片（db×table）一把锁，多个调度器节点竞争。抢到锁的节点处理该分片，保证不重复处理
3. **锁续期**：业务执行期间定期 Refresh 锁的过期时间，防止长时间执行导致锁过期被其他节点抢走
4. **Background Unlock**：defer 中用 `context.Background()` 解锁，因为此时 ctx 可能已经被取消，如果用 ctx 解锁会失败，导致锁泄露

### 9.4 ResourceSemaphore

```go
// pkg/loopjob/resource.go

type MaxCntResourceSemaphore struct {
    mu       sync.Mutex
    curCount int
    maxCount int
}

func (s *MaxCntResourceSemaphore) Acquire() error {
    s.mu.Lock()
    defer s.mu.Unlock()
    if s.curCount >= s.maxCount {
        return errs.ErrExceedLimit
    }
    s.curCount++
    return nil
}

func (s *MaxCntResourceSemaphore) Release() {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.curCount--
}
```

简单的互斥锁 + 计数器实现。`maxCount` 通常配置为 CPU 核数或固定值，控制并发度。

---

## 10. 分片路由策略

### 10.1 ShardingStrategy

```go
// pkg/sharding/strategy.go

type ShardingStrategy struct {
    dbSharding    int  // 数据库分片数
    tableSharding int  // 表分片数
    dbPattern     string
    tablePattern  string
}

type Dst struct {
    DB          string  // 完整库名
    Table       string  // 完整表名
    DBSuffix    string  // 库后缀（如 "_0"）
    TableSuffix string  // 表后缀（如 "_1"）
}

func (s *ShardingStrategy) Shard(id int64) Dst {
    dbHash := id % int64(s.dbSharding)
    tabHash := (id / int64(s.dbSharding)) % int64(s.tableSharding)
    return Dst{
        DB:          fmt.Sprintf(s.dbPattern, dbHash),
        Table:       fmt.Sprintf(s.tablePattern, tabHash),
        DBSuffix:    fmt.Sprintf("_%d", dbHash),
        TableSuffix: fmt.Sprintf("_%d", tabHash),
    }
}

func (s *ShardingStrategy) Broadcast() []Dst {
    var dsts []Dst
    for db := 0; db < s.dbSharding; db++ {
        for tab := 0; tab < s.tableSharding; tab++ {
            dsts = append(dsts, Dst{...})
        }
    }
    return dsts
}
```

**双维分片**：先按 `id % dbSharding` 确定数据库，再按 `(id / dbSharding) % tableSharding` 确定表。这保证了 ID 连续的记录在不同库的不同表中均匀分布。

### 10.2 上下文传递

```go
// pkg/sharding/strategy.go

type dstKey struct{}

func CtxWithDst(ctx context.Context, dst Dst) context.Context {
    return context.WithValue(ctx, dstKey{}, dst)
}

func DstFromCtx(ctx context.Context) Dst {
    return ctx.Value(dstKey{}).(Dst)
}
```

V2 的 DAO 层通过 `DstFromCtx(ctx)` 获取当前操作应该路由到哪个库、哪个表，实现了**业务层无感知**的分片路由。

---

## 11. DAO 层实现

### 11.1 V1 单表 DAO（关键查询）

```go
// internal/repository/dao/task_execution.go

// 查找所有分片父任务（Compensator 入口）
func (d *GORMTaskExecutionDAO) FindShardingParents(
    ctx context.Context, offset, limit int,
) ([]TaskExecution, error) {
    var res []TaskExecution
    err := d.db.WithContext(ctx).
        Where("sharding_parent_id = ? AND status = ?", 0, "RUNNING").
        Order("utime ASC").
        Offset(offset).Limit(limit).
        Find(&res).Error
    return res, err
}

// 查找某父任务的所有子任务
func (d *GORMTaskExecutionDAO) FindShardingChildren(
    ctx context.Context, parentID int64,
) ([]TaskExecution, error) {
    var res []TaskExecution
    err := d.db.WithContext(ctx).
        Where("sharding_parent_id = ?", parentID).
        Find(&res).Error
    return res, err
}

// 查找可重调度的执行记录（排除父任务）
func (d *GORMTaskExecutionDAO) FindReschedulableExecutions(
    ctx context.Context, offset, limit int,
) ([]TaskExecution, error) {
    var res []TaskExecution
    err := d.db.WithContext(ctx).
        Where("status = ? AND (sharding_parent_id IS NULL OR sharding_parent_id > 0)",
            "FAILED_RESCHEDULED").
        Order("utime ASC").
        Offset(offset).Limit(limit).
        Find(&res).Error
    return res, err
}
```

`FindReschedulableExecutions` 的 `sharding_parent_id IS NULL OR sharding_parent_id > 0` 条件巧妙地排除了父任务（parentID = 0），因为父任务的状态由 Compensator 管理，不应该被重调度模块处理。

### 11.2 V2 分片 DAO（BatchCreate）

```go
// internal/repository/dao/sharding_task_execution.go

func (d *ShardingTaskExecutionDAO) BatchCreate(
    ctx context.Context, execs []TaskExecution,
) error {
    // ① 按数据库分组
    dbGroups := make(map[string][]TaskExecution)
    for i := range execs {
        // 使用 snowflake ID 生成，taskID 嵌入分片信息
        id := idPkg.GenerateID(execs[i].TaskID % 1024)
        execs[i].ID = id
        dst := d.shardingStrategy.Shard(id)
        execs[i].Table = dst.Table
        dbGroups[dst.DB] = append(dbGroups[dst.DB], execs[i])
    }

    // ② 按库并发写入
    g, ctx := errgroup.WithContext(ctx)
    for dbName, group := range dbGroups {
        db := dbName
        execGroup := group
        g.Go(func() error {
            // 按表分组，生成 raw SQL
            tableGroups := make(map[string][]TaskExecution)
            for _, e := range execGroup {
                tableGroups[e.Table] = append(tableGroups[e.Table], e)
            }

            // 拼接并执行 SQL
            for table, execs := range tableGroups {
                sql := buildInsertSQL(table, execs)
                if err := d.dbs[db].Exec(sql).Error; err != nil {
                    return err
                }
            }
            return nil
        })
    }
    return g.Wait()
}
```

V2 的 BatchCreate 是一个三层结构：
1. **Snowflake ID 生成**：使用 `taskID % 1024` 作为机器号，ID 中嵌入分片路由信息
2. **按库分组**：通过 `Shard(id)` 确定每条记录的目标库和表
3. **并发写入**：errgroup 按库并发，每个库内按表拼接 raw SQL 批量插入

---

## 12. 数据库 Schema

### 12.1 V1 单表（核心字段变化）

```sql
-- task_executions 表新增字段
ALTER TABLE task_executions ADD COLUMN sharding_parent_id BIGINT DEFAULT NULL;
CREATE INDEX idx_sharding_parent_id ON task_executions(sharding_parent_id);

-- tasks 表新增字段
ALTER TABLE tasks ADD COLUMN sharding_rule JSON;
```

### 12.2 V2 分库分表

```sql
-- scripts/mysql/init.sql V2 建表脚本（2 库 × 2 任务表 × 4 执行表）

CREATE DATABASE IF NOT EXISTS scheduler_db_0;
CREATE DATABASE IF NOT EXISTS scheduler_db_1;

-- scheduler_db_0 中的表
CREATE TABLE scheduler_db_0.task_info_0 (...);
CREATE TABLE scheduler_db_0.task_info_1 (...);
CREATE TABLE scheduler_db_0.task_execution_0 (...);
CREATE TABLE scheduler_db_0.task_execution_1 (...);
CREATE TABLE scheduler_db_0.task_execution_2 (...);
CREATE TABLE scheduler_db_0.task_execution_3 (...);

-- scheduler_db_1 类似结构
```

分片规则：`2 DB × 4 Table = 8 个执行记录分片`，每个分片有独立索引，支撑千万级执行记录。

---

## 13. IOC 组装

```go
// ioc/compensator.go

func InitShardingCompensator(
    nodeID string,
    taskSvc service.TaskService,
    execSvc service.ExecutionService,
    taskAcquirer service.TaskAcquirer,
) *compensator.ShardingCompensator {
    cfg := viper.Sub("compensator.sharding")
    return compensator.NewShardingCompensator(
        nodeID,
        cfg.GetInt("batchSize"),
        cfg.GetDuration("sleepInterval"),
        taskSvc, execSvc, taskAcquirer,
    )
}

// ioc/task.go — ShardingCompensator 作为 t3 注册

func InitTasks(
    scheduler *loopjob.LoopJob,
    timeoutCompensator *compensator.TimeoutCompensator,
    shardingCompensator *compensator.ShardingCompensator,
    retryCompensator *compensator.RetryCompensator,
    rescheduleCompensator *compensator.RescheduleCompensator,
    completeConsumer *event.CompleteConsumer,
) []*loopjob.LoopJob {
    return []*loopjob.LoopJob{
        /* t0 */ scheduler,
        /* t1 */ timeoutCompensator,
        /* t2 */ retryCompensator,
        /* t3 */ shardingCompensator,
        /* t4 */ rescheduleCompensator,
        /* t5 */ completeConsumer,
    }
}
```

ShardingCompensator 和其他 Compensator（Timeout、Retry、Reschedule）平级，作为独立的 LoopJob 注册到启动列表。

---

## 14. 完整数据流

```
用户创建任务
  → Task { ShardingRule: {Type: "range", Params: {totalNums: 3, step: 10000}} }
  → tasks 表 (sharding_rule JSON)
      │
      ▼
调度器 LoopJob 扫描可调度任务
  → TaskAcquirer.Acquire() 抢占
  → Runner Dispatcher 路由到 NormalTaskRunner
      │
      ▼
NormalTaskRunner.Run()
  → task.ShardingRule != nil → handleShardingTask()
      │
      ▼
ExecutionService.CreateShardingChildren()
  ① [WeightedDynamic] Invoker.Prepare() → 获取业务元数据
  ② [WeightedDynamic] registry.ListServices() → 获取节点权重
  ③ ShardingRule.ToScheduleParams()
     Range: [{start:0, end:10000}, {start:10000, end:20000}, {start:20000, end:30000}]
  ④ repo.CreateShardingParent()
     → INSERT task_executions (status=RUNNING, sharding_parent_id=0)
  ⑤ repo.CreateShardingChildren()
     → INSERT × 3 (status=PREPARE, sharding_parent_id=parentID)
      │
      ▼
并行 goroutine × 3
  → WithSpecificNodeIDContext(ctx, nodeID)
  → GRPCInvoker.Run(childExec)
  → gRPC Execute → Executor Node
      │
      ▼
Executor Node 执行分片数据
  → Report 状态 (progress + final status)
  → Kafka produce / gRPC Report
      │
      ▼
UpdateState (子任务终态)
  → isShardedTask = true (ShardingParentID > 0)
  → 跳过 releaseTask + UpdateNextTime
  → 发送 completeEvent
      │
      ▼
ShardingCompensator 定时扫描
  → FindShardingParents (status=RUNNING, parentID=0)
  → FindShardingChildren(parentID)
  → 所有子任务终态?
     → anyFailed → parent.status = FAILED
     → allSuccess → parent.status = SUCCESS
  → releaseTask(parent.TaskID)
  → UpdateNextTime(task)
      │
      ▼
下一轮调度...
```

---

## 15. 集成测试

### 15.1 分片 DAO 测试

```go
// internal/test/integration/sharding_task_test.go
// 381 行，覆盖 ShardingTaskDAO 的全部 CRUD 操作：
// Create, GetByID, FindByPlanID, FindSchedulableTasks,
// Acquire, Renew, Release, UpdateNextTime, UpdateScheduleParams

// internal/test/integration/sharding_task_execution_test.go
// 571 行，覆盖 ShardingTaskExecutionDAO 的全部操作：
// Create, CreateShardingParent, BatchCreate, GetByID,
// UpdateStatus, FindRetryable, FindShardingParents, FindShardingChildren,
// UpdateRetryResult, SetRunningState, UpdateProgress,
// FindReschedulable, FindByPlanID, FindByTaskID, FindTimeout
```

### 15.2 E2E 中断与重调度测试

```go
// internal/test/integration/sharding_interrupt/sharding_task_test.go
// 593 行，端到端测试场景：
//
// 1. 启动 3 个 MockExecutorService
// 2. 插入 Range 分片任务 (totalNums=3, step=10)
// 3. 启动调度器 → 3 个子任务分配到 3 个节点
// 4. 模拟 Node1 执行中断（超时）
// 5. 验证 TimeoutCompensator 检测到 Node1 的子任务超时
// 6. 验证 RescheduleCompensator 重调度到其他节点
// 7. 验证 ShardingCompensator 最终聚合状态
// 8. 全部成功 → 父任务标记 SUCCESS
```

### 15.3 Example

```go
// example/sharding/start_test.go — 创建分片任务

func TestStart(t *testing.T) {
    db := ioc.InitDB()
    now := time.Now().UnixMilli()
    db.Create(&dao.Task{
        Name:   "sharding_test",
        Type:   "normal",
        Status: "running",
        Cron:   "*/30 * * * * ?",
        ShardingRule: `{
            "Type": "range",
            "Params": {"totalNums": "10", "step": "1000"}
        }`,
        NextTime: now,
        Cfg:      `{"grpc": {"serviceName": "test-executor", ...}}`,
    })
}

// example/sharding/server.go — 执行器节点

func main() {
    node := executor.NewExecutorNode(
        executor.WithGRPCServer(port),
        executor.WithEtcdRegistry(etcdAddrs, serviceName),
        executor.WithExecuteFunc(func(ctx context.Context, req *pb.ExecuteRequest) error {
            // 从 ScheduleParams 中读取 start/end，处理该范围的数据
            start := req.ScheduleParams["start"]
            end := req.ScheduleParams["end"]
            log.Printf("Processing range [%s, %s)", start, end)
            return nil
        }),
    )
    node.Start()
}
```

---

## 16. 实现不足与优化方向

### 16.1 Range 策略无法自适应节点数

**现状**：Range 的 `totalNums` 写死在 ShardingRule.Params 中，与实际执行器节点数无关。如果配置 totalNums=5 但只有 3 个节点，会有 2 个子任务被随机分配；如果 5 个节点只配 totalNums=3，则 2 个节点闲置。

**优化**：让 Range 策略也查询 `registry.ListServices()` 获取节点数，动态调整 totalNums。

### 16.2 Compensator 的扫描延迟

**现状**：子任务完成到父任务聚合之间有 `sleepInterval` 级别的延迟（通常秒级）。

**优化**：混合模式——Kafka completeEvent 触发一次即时聚合尝试（乐观路径），Compensator 作为兜底（悲观路径）。这样大部分情况下延迟降到毫秒级，异常情况由 Compensator 保底。

### 16.3 子任务部分失败时的策略

**现状**：任一子任务失败 → 父任务失败。这是最严格的策略，但某些场景下可能希望"3/5 成功就算成功"。

**优化**：在 ShardingRule 中增加 `FailureThreshold` 参数，定义容忍的失败比例或数量。

### 16.4 分片参数的单一性

**现状**：分片参数只支持 `start/end` 的 Range 模型，不支持 Hash、List、自定义分片键等。

**优化**：抽象 `ShardingStrategy` 接口，支持更多分片模式。比如 Hash 分片可以传入数据的 hash key 列表，让每个子任务处理特定 hash 范围。

### 16.5 V2 的 Snowflake ID 依赖

**现状**：V2 使用 `taskID % 1024` 作为 Snowflake 机器号，存在潜在的 ID 冲突风险（多个 taskID 映射到同一机器号）。

**优化**：使用独立的机器号分配服务（如基于 etcd 的自增 ID 分配），或改用 UUID + 分片路由的方式。

---

## 17. 部署验证

### 前置条件

- MySQL 运行（V1 单库或 V2 多库）
- etcd 运行（3 节点或单节点）
- Kafka 运行
- 至少 3 个执行器节点注册到 etcd

### 验证步骤

1. **创建分片任务**
   ```sql
   INSERT INTO tasks (name, type, status, cron, sharding_rule, next_time, cfg)
   VALUES ('test_sharding', 'normal', 'running', '*/30 * * * * ?',
           '{"Type":"range","Params":{"totalNums":"3","step":"10000"}}',
           UNIX_TIMESTAMP(NOW()) * 1000,
           '{"grpc":{"serviceName":"test-executor","method":"Execute"}}');
   ```

2. **启动调度器**
   ```bash
   go run cmd/scheduler/main.go
   ```

3. **启动 3 个执行器**
   ```bash
   # 终端 1-3，分别用不同端口
   go run example/sharding/server.go --port=8081
   go run example/sharding/server.go --port=8082
   go run example/sharding/server.go --port=8083
   ```

4. **观察日志**
   - 调度器日志：`handleShardingTask: created 3 children`
   - 执行器日志：`Processing range [0, 10000)` / `[10000, 20000)` / `[20000, 30000)`
   - Compensator 日志：`ShardingCompensator: parent {id} all children terminal, status=SUCCESS`

5. **验证数据库**
   ```sql
   -- 父任务
   SELECT id, status, sharding_parent_id FROM task_executions
   WHERE sharding_parent_id = 0;
   -- 预期: status=SUCCESS

   -- 子任务
   SELECT id, status, sharding_parent_id, schedule_params FROM task_executions
   WHERE sharding_parent_id > 0;
   -- 预期: 3 条, 各自 status=SUCCESS, schedule_params 包含不同 start/end
   ```

### 验收标准

- [x] Task 支持 ShardingRule JSON 配置
- [x] NormalTaskRunner 自动检测分片规则并走分片分支
- [x] Range 策略正确生成 N 个等分参数
- [x] WeightedDynamicRange 策略按权重分配且余数归最后一个节点
- [x] 子执行记录的 ShardingParentID 正确指向父记录
- [x] 子任务通过 gRPC 指定节点执行（节点亲和性）
- [x] 子任务完成时不释放锁，不更新 NextTime
- [x] ShardingCompensator 正确聚合子任务状态
- [x] 父任务在所有子任务终态后更新为 SUCCESS/FAILED
- [x] Compensator 聚合后释放锁 + 更新下次调度时间
- [x] 重试/重调度跳过分片子任务的锁管理
- [x] V2 分库分表方案的 ShardingLoopJob 正确工作
- [x] E2E 测试覆盖中断 + 重调度 + 聚合的完整生命周期

---

## 18. 面试谈资

### 可以主动提到的点

1. **三态 ShardingParentID 设计**：解释为什么用 `*int64` 而不是 `int64`，SQL 层如何利用 NULL/0/N 三态做精确查询。这展示了对 Go 指针语义和数据库 NULL 语义的深刻理解
2. **Compensator vs Event-Driven 的取舍**：从聚合语义、并发安全、容错性三个维度论证为什么分片用轮询而非事件驱动。这展示了在方案选型时不是简单套用"最新最潮"的模式，而是根据具体场景做务实选择
3. **ShardingLoopJob 的锁续期 + Background Unlock**：解释为什么 defer 中要用 `context.Background()` 而不是 ctx，这是一个很容易写出 bug 的点
4. **值拷贝创建子记录**：`child := parentEntity` 的好处和潜在陷阱（如果字段含 slice/map 会浅拷贝）。在面试中主动提到陷阱并解释如何规避，加分

### 可能被追问的问题

| 问题 | 答题方向 |
|------|---------|
| 分片任务和 MapReduce 什么关系？ | ShardingRule 相当于 Map 阶段的分片策略，ShardingCompensator 相当于简化的 Reduce。但没有 Shuffle 阶段，因为每个分片独立处理，不需要跨分片交换数据 |
| 如果子任务执行了一半 Scheduler 挂了怎么办？ | 已发起的 gRPC 请求不受影响，执行器继续执行并上报状态。Scheduler 重启后 Compensator 会继续扫描父任务，补上聚合逻辑 |
| 为什么不用 Channel 而用 WaitGroup？ | WaitGroup 只需要等所有 goroutine 完成，不需要收集返回值。子任务的结果通过独立的状态上报通道（Kafka/gRPC Report）回传，不需要 goroutine 返回 |
| V2 的 Snowflake ID 冲突如何处理？ | 代码中有 ID 重复的重试逻辑。但更根本的优化是使用独立的机器号分配服务 |
| 如何保证 Compensator 和 UpdateState 不产生状态竞争？ | 子任务的 UpdateState 只更新子记录状态，父任务的状态只由 Compensator 更新。两者操作的是不同行，不存在竞争 |
