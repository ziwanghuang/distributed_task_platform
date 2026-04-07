# Step 4：补偿机制（V1 版本）

> **目标**：在 Step 3 的异步事件系统之上，构建**重试、重调度、超时中断、分片聚合**四大补偿器，实现任务执行的全方位容错保障。
>
> **完成后你能看到**：启动 MySQL + etcd + Kafka → 启动调度器（4 个补偿器自动随服务启动）→ 启动执行器 → 模拟任务执行失败 → 重试补偿器自动发起指数退避重试 → 模拟执行器宕机 → 重调度补偿器将任务转移到健康节点 → 模拟任务超时 → 中断补偿器发送 gRPC 中断信号 → 分片任务执行完毕 → 分片聚合补偿器汇总子任务状态确定父任务最终结果。

---

## 1. 架构总览

Step 4 在 Step 3 的事件驱动架构上，增加了 4 个**后台补偿器**，它们以独立 goroutine 持续运行，周期性扫描数据库中需要补偿的执行记录：

```
                                   ┌───────────────────────────────────────┐
                                   │          Scheduler 进程                │
                                   │                                       │
┌──────────┐                       │  ┌─────────────────────────────────┐  │
│          │  gRPC Execute         │  │     Compensator Goroutines     │  │
│ Executor │◄──────────────────────┤  │                                 │  │
│  Node 1  │                       │  │  ┌──────────┐  ┌────────────┐  │  │
│          │──gRPC Report─────────►│  │  │  Retry   │  │ Reschedule │  │  │
└──────────┘                       │  │  │补偿器     │  │  补偿器     │  │  │
                                   │  │  └─────┬────┘  └─────┬──────┘  │  │
┌──────────┐  gRPC Interrupt       │  │        │             │         │  │
│          │◄──────────────────────┤  │  ┌─────┴────┐  ┌─────┴──────┐  │  │
│ Executor │                       │  │  │Interrupt │  │  Sharding  │  │  │
│  Node 2  │                       │  │  │补偿器     │  │  补偿器     │  │  │
│          │                       │  │  └──────────┘  └────────────┘  │  │
└──────────┘                       │  └────────────┬────────────────┘  │
                                   │               │ 扫描 + 更新        │
                                   │               ▼                   │
                                   │  ┌─────────────────────────────┐  │
                                   │  │     ExecutionService        │  │
                                   │  │     (状态机 + 重试计算)       │  │
                                   │  └──────────────┬──────────────┘  │
                                   │                 │                 │
                                   │  ┌──────────────▼──────────────┐  │
                                   │  │          MySQL              │  │
                                   │  │   task_executions 表        │  │
                                   │  │   (status / retry_count /   │  │
                                   │  │    next_retry_time /        │  │
                                   │  │    deadline / ...)          │  │
                                   │  └─────────────────────────────┘  │
                                   └───────────────────────────────────┘

数据流：
───────────────────────────────────────────────────────────
1. 重试：  DB(FAILED_RETRYABLE) ──► RetryCompensator ──► Runner.Retry ──► Invoker.Run（排除故障节点）
2. 重调度：DB(FAILED_RESCHEDULED) ──► RescheduleCompensator ──► Runner.Reschedule ──► Invoker.Run（指定原节点）
3. 中断：  DB(RUNNING + deadline过期) ──► InterruptCompensator ──► gRPC Interrupt ──► 更新状态
4. 分片：  DB(ShardingParent + RUNNING) ──► ShardingCompensator ──► 查子任务 ──► 汇总父任务状态
```

### Step 3 vs Step 4 对比

| 维度 | Step 3 | Step 4 |
|------|--------|--------|
| **失败处理** | 无，任务失败即终态 | 指数退避重试 + 排除故障节点 |
| **节点故障** | 无感知 | 重调度补偿器自动迁移任务 |
| **超时检测** | 无 | Deadline 机制 + gRPC 中断信号 |
| **分片任务** | 子任务独立运行，父任务无终态 | 补偿器自动汇总子任务结果，驱动父任务终态 |
| **后台任务** | 2 个 MQ 消费者 | 2 个 MQ 消费者 + **4 个补偿器** |
| **核心新增文件** | — | 4 个补偿器 + 重试策略包 + Runner Retry/Reschedule |

### 新增 / 修改文件清单

| 文件 | 说明 |
|------|------|
| `internal/compensator/retry.go` | 重试补偿器 V1 |
| `internal/compensator/reschedule.go` | 重调度补偿器 V1 |
| `internal/compensator/interrupt.go` | 中断补偿器 V1 |
| `internal/compensator/sharding.go` | 分片聚合补偿器 V1 |
| `pkg/retry/strategy/exponential.go` | 指数退避重试策略 |
| `pkg/retry/strategy/types.go` | 重试策略接口定义 |
| `pkg/retry/retry.go` | 重试策略工厂 |
| `internal/service/runner/normal_task_runner.go` | Runner.Retry / Reschedule 方法（修改） |
| `internal/service/task/execution_service.go` | updateRetryState 重试状态机（修改） |
| `internal/repository/dao/task_execution.go` | FindRetryable / FindReschedulable / FindTimeout 查询（修改） |
| `internal/domain/task.go` | RetryConfig + ToRetryComponentConfig（修改） |
| `internal/domain/task_execution.go` | Deadline / RetryCount / NextRetryTime 字段（修改） |
| `ioc/task.go` | 注册 4 个补偿器为后台任务（修改） |
| `config/config.yaml` | compensator 配置段（修改） |

---

## 2. 为什么需要补偿机制

### 2.1 分布式系统的"不可靠性"

在分布式任务调度场景中，以下故障是**必然会发生**的：

| 故障类型 | 典型场景 | 如果不补偿会怎样 |
|----------|---------|-----------------|
| 网络抖动 | 执行器和调度器之间网络瞬断 | 任务永远停在 FAILED 状态，不会被重新执行 |
| 节点宕机 | 执行器 OOM / 机器重启 | 任务丢失，无人接管 |
| 任务超时 | 死锁、无限循环、外部依赖阻塞 | 执行器资源被占满，其他任务无法调度 |
| 分片不完整 | 子任务部分失败 | 父任务永远停在 RUNNING 状态，锁无法释放 |

### 2.2 设计决策：轮询补偿 vs 事件驱动补偿

| 维度 | 轮询补偿（本项目 V1） | 事件驱动补偿 |
|------|---------------------|-------------|
| **触发方式** | 定期扫描数据库 | 失败时即刻触发 |
| **实时性** | 有延迟（≤ MinDuration） | 接近实时 |
| **可靠性** | 极高，不依赖事件不丢失 | 依赖 MQ 可靠性，存在消息丢失风险 |
| **实现复杂度** | 低，简单循环即可 | 中，需要事件编排 + 幂等保障 |
| **对数据库压力** | 有，周期性查询 | 低，按需触发 |
| **适用阶段** | V1 单节点 | V2 可结合使用 |

**选择轮询的理由**：V1 阶段优先保证**可靠性**。轮询补偿天然具备"兜底"能力——即使某次事件丢失、状态更新失败，下一轮扫描仍然能捕获到需要补偿的记录。这是分布式调度系统中最安全的容错策略。

---

## 3. 核心设计：指数退避重试策略

在讲解四个补偿器之前，先介绍它们共用的重试策略基础设施。

### 3.1 策略接口设计

```go
// pkg/retry/strategy/types.go
type Strategy interface {
    // NextWithRetries 无状态模式：传入重试次数，返回下次间隔
    // 补偿器从 DB 读取 retryCount 后调用
    NextWithRetries(retries int32) (time.Duration, bool)

    // Next 有状态模式：内部自增计数器
    // 适用于单次调用链中的重试（如 RPC 调用）
    Next() (time.Duration, bool)

    // Report 上报错误信息（预留自适应扩展点）
    Report(err error) Strategy
}
```

**双模式设计的考量**：

- `NextWithRetries`（无状态）：补偿器场景。补偿器从数据库读取 `retryCount`，传入计算下次间隔，不依赖内存状态。这对于多节点部署至关重要——任何一个调度节点都能正确计算重试时间。
- `Next`（有状态）：单次调用链场景。使用 `atomic.AddInt32` 保证并发安全的内部计数。

### 3.2 指数退避算法实现

```go
// pkg/retry/strategy/exponential.go

// 退避公式：interval = initialInterval × 2^(retries-1)
//
// 例如 initialInterval=1s, maxInterval=30s, maxRetries=3：
//   第1次重试: 1s × 2^0 = 1s
//   第2次重试: 1s × 2^1 = 2s
//   第3次重试: 1s × 2^2 = 4s
//   第4次：retries > maxRetries → return (0, false)，不再重试

type ExponentialBackoffRetryStrategy struct {
    initialInterval    time.Duration  // 初始间隔
    maxInterval        time.Duration  // 最大间隔上限
    maxRetries         int32          // 最大重试次数（≤0 表示无限重试）
    retries            int32          // 当前重试次数（atomic，仅 Next() 使用）
    maxIntervalReached atomic.Value   // 优化标记：达到上限后跳过 Pow 计算
}
```

核心计算逻辑：

```go
func (s *ExponentialBackoffRetryStrategy) nextWithRetries(retries int32) (time.Duration, bool) {
    // 1. 判断是否超过最大重试次数
    if s.maxRetries <= 0 || retries <= s.maxRetries {
        // 2. 优化：如果已达到最大间隔，直接返回，避免重复计算 math.Pow
        if reached, ok := s.maxIntervalReached.Load().(bool); ok && reached {
            return s.maxInterval, true
        }
        // 3. 指数退避计算
        interval := s.initialInterval * time.Duration(math.Pow(2, float64(retries-1)))
        // 4. 溢出保护 + 上限检查
        if interval <= 0 || interval > s.maxInterval {
            s.maxIntervalReached.Store(true)
            return s.maxInterval, true
        }
        return interval, true
    }
    // 5. 超过最大重试次数，停止重试
    return 0, false
}
```

### 3.3 设计决策：为什么选择指数退避

| 策略 | 退避模式 | 适用场景 | 缺点 |
|------|---------|---------|------|
| **固定间隔** | 1s → 1s → 1s | 确定性故障（如配置错误） | 对瞬时故障恢复慢，对持续故障浪费资源 |
| **线性退避** | 1s → 2s → 3s | 负载渐增场景 | 增长太慢，对网络抖动不友好 |
| **指数退避** ✅ | 1s → 2s → 4s → 8s | 网络抖动、服务过载 | 可能间隔过长（需 maxInterval 限制） |
| **指数退避 + 抖动** | 1s ± random → 2s ± random | 高并发重试风暴 | 实现稍复杂 |

**选择指数退避的理由**：
1. 网络抖动通常是短暂的，指数退避能在前几次快速重试中捕获恢复窗口
2. 如果服务持续不可用，快速增长的间隔避免对下游服务形成"重试风暴"
3. `maxInterval` 兜底确保间隔不会无限增长
4. `maxIntervalReached` 优化标记避免每次都做 `math.Pow` 浮点计算

> **面试加分点**：当前 V1 版本未加抖动（jitter）。在多调度节点场景下，所有节点可能在相近时刻同时重试同一批任务，形成"惊群效应"。V2 优化方向之一是在退避间隔上叠加随机抖动：`interval = baseInterval + random(0, baseInterval * 0.3)`。

### 3.4 领域模型集成

```go
// internal/domain/task.go
type RetryConfig struct {
    MaxRetries      int32 `json:"maxRetries"`      // 最大重试次数
    InitialInterval int64 `json:"initialInterval"`  // 初始间隔（毫秒）
    MaxInterval     int64 `json:"maxInterval"`      // 最大间隔（毫秒）
}

// 转换为 retry 组件配置，桥接领域模型和基础设施层
func (r *RetryConfig) ToRetryComponentConfig() retry.Config {
    return retry.Config{
        Type: "exponential",
        ExponentialBackoff: &retry.ExponentialBackoffConfig{
            InitialInterval: time.Duration(r.InitialInterval) * time.Millisecond,
            MaxInterval:     time.Duration(r.MaxInterval) * time.Millisecond,
            MaxRetries:      r.MaxRetries,
        },
    }
}
```

这里有一个关键的**反腐败层**设计：`RetryConfig`（领域模型）通过 `ToRetryComponentConfig()` 转换为 `retry.Config`（基础设施层），避免领域层直接依赖 `pkg/retry` 的数据结构。

---

## 4. 补偿器一：重试补偿器（RetryCompensator）

### 4.1 职责

定期扫描 `FAILED_RETRYABLE` 状态且 `next_retry_time ≤ now` 的执行记录，通过 `Runner.Retry` 重新发起执行。

### 4.2 完整代码实现

```go
// internal/compensator/retry.go

type RetryConfig struct {
    MaxRetryCount          int64         `yaml:"maxRetryCount"`
    PrepareTimeoutWindowMs int64         `yaml:"prepareTimeoutWindowMs"`
    BatchSize              int           `yaml:"batchSize"`
    MinDuration            time.Duration `yaml:"minDuration"`
}

type RetryCompensator struct {
    runner  runner.Runner           // 重试执行器
    execSvc task.ExecutionService   // 查询可重试记录
    config  RetryConfig
    logger  *elog.Component
}
```

**Start 方法——补偿循环骨架**：

```go
func (r *RetryCompensator) Start(ctx context.Context) {
    r.logger.Info("重试补偿器启动")
    for {
        select {
        case <-ctx.Done():
            r.logger.Info("重试补偿器停止")
            return
        default:
            startTime := time.Now()

            err := r.retry(ctx)
            if err != nil {
                r.logger.Error("重试失败", elog.FieldErr(err))
            }

            // 防空转：确保最小等待时间
            elapsed := time.Since(startTime)
            if elapsed < r.config.MinDuration {
                select {
                case <-ctx.Done():
                    return
                case <-time.After(r.config.MinDuration - elapsed):
                }
            }
        }
    }
}
```

**retry 方法——一轮补偿逻辑**：

```go
func (r *RetryCompensator) retry(ctx context.Context) error {
    // 1. 查找可重试的执行记录
    executions, err := r.execSvc.FindRetryableExecutions(ctx, r.config.BatchSize)
    if err != nil {
        return fmt.Errorf("查找可重试任务失败: %w", err)
    }
    if len(executions) == 0 {
        return nil
    }

    // 2. 逐个处理，单个失败不影响其他
    for i := range executions {
        err = r.runner.Retry(ctx, executions[i])
        if err != nil {
            r.logger.Error("重试任务失败",
                elog.Int64("executionId", executions[i].ID),
                elog.String("taskName", executions[i].Task.Name),
                elog.Int64("retryCount", executions[i].RetryCount),
                elog.FieldErr(err))
            continue  // ← 关键：continue 而非 return，确保批次内尽力处理
        }
    }
    return nil
}
```

### 4.3 防空转机制（MinDuration 模式）

四个 V1 补偿器都采用相同的防空转模式，这里统一分析：

```
时间线：
────────────────────────────────────────────────────

情况 1：有任务需要处理（耗时 800ms，MinDuration=1s）
  |── retry() 处理 ──|── sleep 200ms ──|── 下一轮 ──|

情况 2：无任务（耗时 2ms，MinDuration=1s）
  |──|────── sleep 998ms ──────|── 下一轮 ──|

情况 3：处理耗时超过 MinDuration（耗时 3s）
  |────── retry() 处理 ──────|── 立即下一轮 ──|
```

**为什么用 `MinDuration` 而不是固定 `time.Sleep`？**

固定 sleep 的问题：如果处理一批任务用了 5s，再 sleep 1s，总间隔变成 6s。而 MinDuration 模式保证"每轮至少 1s，但处理耗时长时不额外等待"。

**为什么在 sleep 中也检查 `ctx.Done()`？**

```go
select {
case <-ctx.Done():
    return         // 优雅停机，不等待 MinDuration 剩余时间
case <-time.After(r.config.MinDuration - elapsed):
}
```

如果不检查 `ctx.Done()`，服务停机时补偿器可能要等待最长 `MinDuration` 才能退出，影响停机速度。

### 4.4 DAO 层查询

```go
// internal/repository/dao/task_execution.go
func (g *GORMTaskExecutionDAO) FindRetryableExecutions(ctx context.Context, limit int) ([]TaskExecution, error) {
    var executions []TaskExecution
    now := time.Now().UnixMilli()

    err := g.db.WithContext(ctx).
        Where(`status=? AND next_retry_time <= ?`, TaskExecutionStatusFailedRetryable, now).
        Where("next_retry_time <= ?", now).  // 确保到了可以执行的时间
        Limit(limit).
        Find(&executions).Error
    return executions, err
}
```

查询条件解读：
- `status = 'FAILED_RETRYABLE'`：只扫描可重试状态的记录
- `next_retry_time <= now`：只扫描已到达重试时间点的记录（由指数退避算法计算得出）
- `Limit(limit)`：批量控制，避免一次加载过多数据

> **注意**：当前查询没有 `ORDER BY`，在高并发下可能导致同一批记录被重复扫描。V2 版本通过分布式锁 + 分片扫描解决此问题。

---

## 5. 补偿器二：重调度补偿器（RescheduleCompensator）

### 5.1 职责

扫描 `FAILED_RESCHEDULED` 状态的执行记录，通过 `Runner.Reschedule` 将任务重新调度到**指定节点**。

### 5.2 与重试补偿器的关键区别

| 维度 | 重试（Retry） | 重调度（Reschedule） |
|------|-------------|---------------------|
| **触发状态** | `FAILED_RETRYABLE` | `FAILED_RESCHEDULED` |
| **谁决定触发** | 调度器根据错误类型自动判断 | 执行器主动上报（含 `RequestReschedule` 标记） |
| **节点路由** | 排除故障节点，随机选择其他节点 | 指定到原执行节点重新执行 |
| **适用场景** | 网络抖动、临时错误 | 节点恢复后需要在同一节点继续（保持本地状态） |
| **重试次数限制** | 有，受 `maxRetries` 控制 | 无次数限制 |
| **退避策略** | 指数退避 | 无退避，发现即执行 |

### 5.3 核心实现

```go
// internal/compensator/reschedule.go

type RescheduleConfig struct {
    BatchSize   int
    MinDuration time.Duration
}

type RescheduleCompensator struct {
    runner  runner.Runner
    execSvc task.ExecutionService
    config  RescheduleConfig
    logger  *elog.Component
}
```

补偿逻辑与 RetryCompensator 结构完全一致（**模板方法模式**），区别仅在于：

```go
func (r *RescheduleCompensator) reschedule(ctx context.Context) error {
    // 查找可重调度的执行记录
    executions, err := r.execSvc.FindReschedulableExecutions(ctx, r.config.BatchSize)
    // ...
    for i := range executions {
        err = r.runner.Reschedule(ctx, executions[i])  // ← 注意：调用 Reschedule 而非 Retry
        // ...
    }
    return nil
}
```

### 5.4 DAO 层查询

```go
func (g *GORMTaskExecutionDAO) FindReschedulableExecutions(ctx context.Context, limit int) ([]TaskExecution, error) {
    var executions []TaskExecution
    err := g.db.WithContext(ctx).
        Where("status = ? AND (sharding_parent_id IS NULL OR sharding_parent_id > 0)",
            TaskExecutionStatusFailedRescheduled).
        Order("utime ASC").
        Limit(limit).
        Find(&executions).Error
    return executions, err
}
```

**关键条件 `sharding_parent_id IS NULL OR sharding_parent_id > 0`**：

这排除了 `sharding_parent_id = 0` 的记录——即分片父任务本身。分片父任务不需要被重调度，只有普通任务（`NULL`）和分片子任务（`> 0`）才可能需要重调度。

### 5.5 状态机中的重调度处理

```go
// internal/service/task/execution_service.go — UpdateState 中

case state.Status.IsFailedRescheduled():
    if state.RequestReschedule {
        // 合并执行器请求的重调度参数（如新的分片范围）
        execution.MergeTaskScheduleParams(state.RescheduleParams)
    }
    err = s.updateState(ctx, execution, state)
```

执行器上报 `FAILED_RESCHEDULED` 时可以携带 `RescheduleParams`，这些参数会被合并到任务的 `ScheduleParams` 中。典型场景：执行器检测到本地磁盘满了，请求重调度时携带新的数据存储路径。

---

## 6. 补偿器三：中断补偿器（InterruptCompensator）

### 6.1 职责

扫描已超时但仍在运行的执行记录（`RUNNING + deadline ≤ now`），通过 gRPC 向执行器发送中断信号。

### 6.2 Deadline 机制

超时检测的核心是 **Deadline（截止时间）**，在执行记录创建时计算：

```go
// internal/repository/dao/task_execution.go — Create
execution.Deadline = now + execution.TaskMaxExecutionSeconds * 1000  // 毫秒

// SetRunningState 时重新计算（重试/重调度后 deadline 需要刷新）
newDeadline := now + execution.TaskMaxExecutionSeconds * 1000
```

```
时间线：
────────────────────────────────────────────────────────────────
                 create            setRunning           deadline
                   │                   │                   │
                   ▼                   ▼                   ▼
  ───────┬─────────┬───────────────────┬───────────────────┬──────►
         │ PREPARE │     RUNNING       │    TIMEOUT!       │
         │         │                   │                   │
         │         │  MaxExecSec=3600  │                   │
         │         │  (1小时)           │                   │
         │         │                   │                   │
                   └─── deadline = now + 3600*1000 ────────┘

中断补偿器查询条件：deadline <= now AND status = RUNNING
```

### 6.3 完整中断流程

```go
// internal/compensator/interrupt.go

type InterruptCompensator struct {
    execSvc     task.ExecutionService
    config      InterruptConfig
    logger      *elog.Component
    grpcClients *grpc.ClientsV2[executorv1.ExecutorServiceClient]  // ← 直接持有 gRPC 客户端
}
```

**注意**：与 Retry/Reschedule 不同，InterruptCompensator 没有依赖 `Runner`，而是**直接持有 gRPC 客户端池**。原因：中断操作不需要"抢占 → 创建执行记录 → 调用"的完整 Runner 流程，它只需要给执行器发一个信号。

中断单个任务的逻辑：

```go
func (t *InterruptCompensator) interruptTaskExecution(
    ctx context.Context, execution domain.TaskExecution,
) error {
    // 1. 检查 gRPC 配置（本地执行的任务无法中断）
    if execution.Task.GrpcConfig == nil {
        return fmt.Errorf("未找到GRPC配置，无法执行中断任务")
    }

    // 2. 获取 gRPC 客户端（按服务名路由）
    client := t.grpcClients.Get(execution.Task.GrpcConfig.ServiceName)

    // 3. 发送中断请求
    resp, err := client.Interrupt(ctx, &executorv1.InterruptRequest{
        Eid: execution.ID,
    })
    if err != nil {
        return fmt.Errorf("发送中断请求失败：%w", err)
    }

    // 4. 检查中断结果
    if !resp.GetSuccess() {
        return errs.ErrInterruptTaskExecutionFailed
    }

    // 5. 用执行器返回的状态更新数据库
    return t.execSvc.UpdateState(ctx, domain.ExecutionStateFromProto(resp.GetExecutionState()))
}
```

### 6.4 中断协议设计

```protobuf
// api/proto/executor/v1/executor.proto

message InterruptRequest {
    int64 eid = 1;  // 执行记录 ID
}

message InterruptResponse {
    bool success = 1;                  // 中断是否成功
    ExecutionState execution_state = 2; // 中断后的执行状态
}
```

**执行器侧的响应策略**：

| 中断结果 | `success` | `execution_state` | 后续处理 |
|---------|-----------|-------------------|---------|
| 成功中断 | `true` | `FAILED` / `CANCELLED` | 调度器更新为终态 |
| 任务已完成 | `true` | `SUCCESS` | 调度器更新为 SUCCESS |
| 中断失败 | `false` | — | 调度器忽略，下轮继续尝试 |

### 6.5 DAO 层查询

```go
func (g *GORMTaskExecutionDAO) FindTimeoutExecutions(ctx context.Context, limit int) ([]TaskExecution, error) {
    var executions []TaskExecution
    now := time.Now().UnixMilli()

    err := g.db.WithContext(ctx).
        Where("deadline <= ? AND status = ?", now, TaskExecutionStatusRunning).
        Order("deadline ASC").  // 优先处理超时最久的
        Limit(limit).
        Find(&executions).Error
    return executions, err
}
```

`Order("deadline ASC")` 确保超时最久的任务优先被中断，这是一个合理的**公平性策略**——超时越久意味着资源浪费越大。

---

## 7. 补偿器四：分片聚合补偿器（ShardingCompensator）

### 7.1 职责

定期扫描分片父任务（`sharding_parent_id = 0 AND status = RUNNING`），检查所有子任务是否已完成，汇总最终结果并释放任务锁。

### 7.2 分片任务的生命周期

```
                 Runner.handleShardingTask
                         │
          ┌──────────────┼──────────────┐
          │              │              │
          ▼              ▼              ▼
   ┌───────────┐  ┌───────────┐  ┌───────────┐
   │ Parent    │  │ Child #1  │  │ Child #2  │
   │ RUNNING   │  │ PREPARE   │  │ PREPARE   │
   │ SPid=0    │  │ SPid=P.ID │  │ SPid=P.ID │
   └─────┬─────┘  └─────┬─────┘  └─────┬─────┘
         │              │              │
         │         gRPC Execute   gRPC Execute
         │              │              │
         │              ▼              ▼
         │         ┌─────────┐   ┌─────────┐
         │  等待    │ RUNNING │   │ RUNNING │
         │  补偿器  │         │   │         │
         │  扫描    └────┬────┘   └────┬────┘
         │              │              │
         │              ▼              ▼
         │         ┌─────────┐   ┌─────────┐
         │         │ SUCCESS │   │ FAILED  │
         │         └────┬────┘   └────┬────┘
         │              │              │
         ▼              ▼              ▼
  ShardingCompensator.handle()
         │
         ├── 发现 Child #2 FAILED → anyFailed = true
         ├── progress = (1 SUCCESS / 2 total) × 100 = 50%
         ├── Parent → FAILED (任一子任务失败即失败)
         ├── UpdateNextTime (更新下次调度时间)
         └── releaseTask (释放 CAS 抢占锁)
```

### 7.3 核心实现：Offset 分页遍历模式

与其他补偿器不同，ShardingCompensator 使用 **offset 分页**遍历所有分片父任务：

```go
func (r *ShardingCompensator) Start(ctx context.Context) {
    offset := 0
    for {
        select {
        case <-ctx.Done():
            return
        default:
        }

        executions, err := r.execSvc.FindShardingParents(ctx, offset, r.config.BatchSize)
        if err != nil {
            offset += r.config.BatchSize  // 出错跳过本批，避免卡住
            continue
        }

        if len(executions) == 0 {
            time.Sleep(r.config.MinDuration)  // 到头了，休眠后重新开始
            offset = 0
            continue
        }

        for i := range executions {
            _ = r.handle(ctx, executions[i])
        }
        offset += len(executions)  // 推进到下一批
    }
}
```

**为什么用 offset 分页而不是 MinDuration 模式？**

分片父任务可能积累大量记录（每个都处于 RUNNING 状态等待子任务完成），一次 `batchSize=100` 可能处理不完。offset 模式允许补偿器**持续推进**直到扫描完所有记录，然后才休眠。

### 7.4 汇总算法

```go
func (r *ShardingCompensator) handle(ctx context.Context, parent domain.TaskExecution) error {
    children, err := r.execSvc.FindShardingChildren(ctx, parent.ID)
    if err != nil {
        return fmt.Errorf("查找分片子任务失败: %w", err)
    }

    // 边界：无子任务 → 创建异常，强制标记失败
    if len(children) == 0 {
        defer r.releaseTask(ctx, parent.Task)
        // 更新下次执行时间 + 标记父任务 FAILED
        // ...
        return errs.Join(...)
    }

    // 遍历所有子任务
    anyFailed := false
    successCount := 0
    for i := range children {
        if !children[i].Status.IsSuccess() && !children[i].Status.IsFailed() {
            // 发现非终态子任务 → 中止本轮，等下次补偿
            return nil
        }
        if children[i].Status.IsFailed() {
            anyFailed = true
        }
        if children[i].Status.IsSuccess() {
            successCount++
        }
    }

    // 所有子任务都已终止
    defer r.releaseTask(ctx, parent.Task)

    var finalStatus domain.TaskExecutionStatus
    if anyFailed {
        finalStatus = domain.TaskExecutionStatusFailed
    } else {
        finalStatus = domain.TaskExecutionStatusSuccess
    }

    progress := int32((successCount * 100) / len(children))

    // 更新父任务终态 + 更新下次执行时间
    r.execSvc.UpdateScheduleResult(ctx, parent.ID, finalStatus, progress, ...)
    r.taskSvc.UpdateNextTime(ctx, parent.Task.ID)
    return errs
}
```

### 7.5 汇总策略的设计决策

| 策略 | 逻辑 | 适用场景 |
|------|------|---------|
| **Any-Failed** ✅（本项目） | 任一子任务失败 → 父任务失败 | 数据一致性要求高（ETL 管线） |
| **All-Failed** | 全部失败 → 父任务失败 | 容忍部分失败（数据采集） |
| **Threshold** | 失败率超阈值 → 父任务失败 | 弹性处理（推荐系统预计算） |

当前采用 **Any-Failed** 策略，这是最保守也最安全的选择。生产环境可以考虑在 Task 模型上增加 `shardingAggregateStrategy` 字段支持灵活配置。

### 7.6 锁释放的重要性

```go
func (r *ShardingCompensator) releaseTask(ctx context.Context, task domain.Task) {
    if err := r.taskAcquirer.Release(ctx, task.ID, r.nodeID); err != nil {
        r.logger.Error("释放任务失败", ...)
    }
}
```

分片任务的 CAS 锁由**父任务**持有，从 `handleShardingTask`（创建时抢占）一直持有到 `ShardingCompensator.handle`（汇总完释放）。如果不释放，这个任务的下一次 Cron 调度将永远无法执行。

```
锁的生命周期：
─────────────────────────────────────────────────────────────
Runner.Run ─► acquireTask ─── 持有锁 ──── ShardingCompensator.releaseTask
    │                                              │
    │    这段期间子任务并行执行                        │
    │    调度器不会再次调度此任务                       │
    └──────────────────────────────────────────────┘
```

---

## 8. Runner 层：Retry 与 Reschedule 的实现

补偿器通过 `Runner` 接口发起重试/重调度，Runner 层是补偿器和执行基础设施之间的桥梁。

### 8.1 Runner 接口

```go
// internal/service/runner/types.go
type Runner interface {
    Run(ctx context.Context, task domain.Task) error
    Retry(ctx context.Context, execution domain.TaskExecution) error
    Reschedule(ctx context.Context, execution domain.TaskExecution) error
}
```

### 8.2 Retry 实现——排除故障节点

```go
// internal/service/runner/normal_task_runner.go

func (s *NormalTaskRunner) Retry(ctx context.Context, execution domain.TaskExecution) error {
    // 1. 分片子任务无需重新抢占（锁由父任务持有）
    if !s.isShardedTaskExecution(execution) {
        acquiredTask, err := s.acquireTask(ctx, execution.Task)
        if err != nil {
            return err
        }
        execution.Task = acquiredTask
    }

    // 2. 异步执行，不阻塞补偿器循环
    go func() {
        // 关键：WithExcludedNodeID 排除上次失败的执行节点
        state, err := s.invoker.Run(
            s.WithExcludedNodeIDContext(ctx, execution.ExecutorNodeID),
            execution,
        )
        if err != nil {
            s.logger.Error("执行器执行任务失败", elog.FieldErr(err))
            return
        }
        // 更新状态，触发状态机
        s.execSvc.UpdateState(ctx, state)
    }()
    return nil
}
```

### 8.3 Reschedule 实现——指定目标节点

```go
func (s *NormalTaskRunner) Reschedule(ctx context.Context, execution domain.TaskExecution) error {
    if !s.isShardedTaskExecution(execution) {
        acquiredTask, err := s.acquireTask(ctx, execution.Task)
        if err != nil {
            return err
        }
        execution.Task = acquiredTask
    }

    go func() {
        // 关键：WithSpecificNodeID 指定到原执行节点
        state, err := s.invoker.Run(
            s.WithSpecificNodeIDContext(ctx, execution.ExecutorNodeID),
            execution,
        )
        if err != nil {
            s.logger.Error("执行器执行任务失败", elog.FieldErr(err))
            return
        }
        s.execSvc.UpdateState(ctx, state)
    }()
    return nil
}
```

### 8.4 节点路由的 Context 注入机制

```go
// 排除节点
func (s *NormalTaskRunner) WithExcludedNodeIDContext(ctx context.Context, executorNodeID string) context.Context {
    if executorNodeID != "" {
        return balancer.WithExcludedNodeID(ctx, executorNodeID)
    }
    return ctx
}

// 指定节点
func (s *NormalTaskRunner) WithSpecificNodeIDContext(ctx context.Context, executorNodeID string) context.Context {
    if executorNodeID != "" {
        return balancer.WithSpecificNodeID(ctx, executorNodeID)
    }
    return ctx
}
```

这里巧妙地利用了 **gRPC Context 元数据传递**：

```
补偿器 → Runner → Context 注入 → gRPC Call → 负载均衡器 Picker → 读取 Context → 路由决策
```

不需要修改 gRPC 接口定义或 Invoker 接口，只需要在 Context 中携带路由信息，由自定义的 gRPC Balancer 消费。这是一个非常优雅的**非侵入式**设计。

### 8.5 分片子任务的特殊处理

```go
func (s *NormalTaskRunner) isShardedTaskExecution(execution domain.TaskExecution) bool {
    return execution.ShardingParentID != nil && *execution.ShardingParentID > 0
}
```

分片子任务重试/重调度时**不需要重新抢占任务**，因为：

1. 锁由父任务在 `Runner.Run → acquireTask` 时获取
2. 锁一直持有到 `ShardingCompensator.releaseTask`
3. 如果子任务重试时还要抢占，会因为任务已经被抢占（版本号不匹配）而必定失败

---

## 9. 状态机集成：updateRetryState

重试补偿器的核心状态转换由 `ExecutionService.updateRetryState` 驱动：

```go
// internal/service/task/execution_service.go

func (s *executionService) updateRetryState(
    ctx context.Context,
    execution domain.TaskExecution,
    state domain.ExecutionState,
) error {
    // 1. 根据任务配置创建重试策略实例
    retryStrategy, _ := retry.NewRetry(execution.Task.RetryConfig.ToRetryComponentConfig())

    // 2. 无状态计算：传入 retryCount+1（下一次重试的序号）
    duration, shouldRetry := retryStrategy.NextWithRetries(int32(execution.RetryCount + 1))

    if shouldRetry {
        // 3a. 未达到最大重试次数
        execution.NextRetryTime = time.Now().Add(duration).UnixMilli()
        execution.RetryCount++
    } else if !state.Status.IsTerminalStatus() {
        // 3b. 达到最大重试次数，强制设为 FAILED
        state.Status = domain.TaskExecutionStatusFailed
    }

    // 4. 更新数据库（不管是否达到上限都更新 retryCount）
    err := s.UpdateRetryResult(ctx,
        state.ID,
        execution.RetryCount,
        execution.NextRetryTime,
        state.Status,
        state.RunningProgress,
        time.Now().UnixMilli(),
        execution.Task.ScheduleParams,
        state.ExecutorNodeID,
    )
    if err != nil {
        if !shouldRetry {
            return fmt.Errorf("%w: %w", errs.ErrExecutionMaxRetriesExceeded, err)
        }
        return err
    }
    if !shouldRetry {
        return errs.ErrExecutionMaxRetriesExceeded
    }
    return nil
}
```

### 9.1 状态流转详解

```
执行器上报 FAILED_RETRYABLE
         │
         ▼
  ExecutionService.UpdateState
         │
    ┌────┴────────────────────────┐
    │ case state.Status.IsFailedRetryable():
    │     updateRetryState()
    │         │
    │    ┌────┴────────────────────────┐
    │    │ retryCount+1 ≤ maxRetries   │
    │    │  → 计算 NextRetryTime       │
    │    │  → 状态保持 FAILED_RETRYABLE │
    │    │  → DB: UPDATE retry_count,  │
    │    │         next_retry_time     │
    │    │  → return nil               │
    │    └─────────────────────────────┘
    │         │
    │    ┌────┴────────────────────────┐
    │    │ retryCount+1 > maxRetries   │
    │    │  → 状态强制改为 FAILED      │
    │    │  → DB: UPDATE status=FAILED │
    │    │  → sendCompletedEvent       │
    │    │  → return ErrMaxRetries     │
    │    └─────────────────────────────┘
    └─────────────────────────────────────
```

### 9.2 关键设计：不管是否达到上限都更新数据库

```go
// 不管是否达到最大重试次数，都要更新状态（主要是重试次数）
err := s.UpdateRetryResult(ctx, ...)
```

即使已达到最大重试次数（`shouldRetry = false`），仍然执行 `UpdateRetryResult`。原因：

1. 需要将 `status` 更新为 `FAILED`
2. 需要记录最终的 `retryCount`，供运维排查
3. 如果只更新状态不更新 `retryCount`，可能出现状态和计数不一致

---

## 10. IOC 注册与配置

### 10.1 后台任务注册

```go
// ioc/task.go
func InitTasks(
    t1 *compensator.RetryCompensator,       // 重试补偿器
    t2 *compensator.RescheduleCompensator,  // 重调度补偿器
    t3 *compensator.ShardingCompensator,    // 分片聚合补偿器
    t4 *compensator.InterruptCompensator,   // 中断补偿器
    t5 *reportevt.BatchReportEventConsumer, // 批量上报消费者（Step 3）
    t6 *reportevt.ReportEventConsumer,      // 单条上报消费者（Step 3）
) []Task {
    return []Task{t1, t2, t3, t4, t5, t6}
}
```

6 个后台任务通过 Wire 依赖注入注册，在 `SchedulerApp.StartTasks` 中以独立 goroutine 启动：

```
SchedulerApp.Start()
    │
    ├── go t1.Start(ctx)   // RetryCompensator
    ├── go t2.Start(ctx)   // RescheduleCompensator
    ├── go t3.Start(ctx)   // ShardingCompensator
    ├── go t4.Start(ctx)   // InterruptCompensator
    ├── go t5.Start(ctx)   // BatchReportEventConsumer
    └── go t6.Start(ctx)   // ReportEventConsumer
```

### 10.2 配置项

```yaml
# config/config.yaml
compensator:
  retry:
    maxRetryCount: 3             # 最大重试次数
    prepareTimeoutWindowMs: 10s  # PREPARE 超时窗口
    batchSize: 100               # 每轮批量处理上限
    minDuration: 1s              # 最小轮次间隔（防空转）
  reschedule:
    batchSize: 100
    minDuration: 1s
  sharding:
    batchSize: 100
    minDuration: 1s
  interrupt:
    batchSize: 100
    minDuration: 1s
```

---

## 11. 数据流全景：一个任务从失败到重试成功的完整链路

```
时间线 T=0：Task#42 首次执行
──────────────────────────────────────────────────────────
1. Scheduler 抢占 Task#42 (CAS: version 1→2)
2. 创建 TaskExecution#100 (status=PREPARE, deadline=T+3600s)
3. Invoker.Run → gRPC Execute → Executor Node-A 开始执行
4. Executor 上报 Running → SetRunningState → deadline 刷新为 T+3600s

时间线 T=30s：执行失败
──────────────────────────────────────────────────────────
5. Executor 上报 FAILED_RETRYABLE
6. ExecutionService.UpdateState → updateRetryState
7. retryCount: 0→1, NextRetryTime = T+30s + 1s (第1次退避) = T+31s
8. DB: UPDATE status=FAILED_RETRYABLE, retry_count=1, next_retry_time=T+31s
9. 释放任务锁 (version 2→3, status→ACTIVE)

时间线 T=31s：重试补偿器介入
──────────────────────────────────────────────────────────
10. RetryCompensator.retry() 扫描到 Execution#100
    (status=FAILED_RETRYABLE, next_retry_time ≤ now)
11. Runner.Retry → acquireTask (CAS: version 3→4)
12. ctx = WithExcludedNodeID(ctx, "Node-A")  // 排除故障节点
13. Invoker.Run → gRPC Execute → Executor Node-B (Node-A 被排除)

时间线 T=35s：重试成功
──────────────────────────────────────────────────────────
14. Executor Node-B 上报 SUCCESS
15. ExecutionService.UpdateState → 终态处理
16. DB: UPDATE status=SUCCESS, etime=T+35s
17. 释放任务锁, UpdateNextTime → 设置下次 Cron 调度时间
18. sendCompletedEvent → Kafka
```

---

## 12. V1 版本的已知不足与优化方向

### 12.1 并发安全问题

**问题**：多个调度节点同时运行同一个补偿器，可能扫描到相同的执行记录并发起重复补偿。

```
Node-1: RetryCompensator 扫描到 Exec#100 → Runner.Retry
Node-2: RetryCompensator 扫描到 Exec#100 → Runner.Retry  ← 重复！
```

**V2 解决方案**：引入分布式锁（`dlock`），每个补偿器在处理前先获取锁，确保同一时刻只有一个节点在运行该补偿器。

### 12.2 扫描效率问题

**问题**：4 个补偿器各自扫描全表，在分库分表场景下需要扫描 `2库 × 4表 = 8` 个分表。

**V2 解决方案**：`ShardingLoopJob` 框架，将分表分配给不同节点并发扫描，通过信号量控制并发度。

### 12.3 惊群效应

**问题**：所有补偿器以相同的 `MinDuration` 间隔轮询，可能同时发起大量数据库查询。

**优化方向**：
1. 在 MinDuration 上叠加随机抖动
2. 不同补偿器设置不同的 MinDuration
3. V2 的 ShardingLoopJob 天然分散了扫描时间点

### 12.4 重试风暴

**问题**：大量任务同时失败并进入重试队列，指数退避没有抖动，所有任务在相同时间点发起重试。

**优化方向**：`interval = baseInterval + random(0, baseInterval × jitterFactor)`

### 12.5 中断补偿器的局限

**问题**：中断请求依赖 gRPC 连接可达。如果执行器已经宕机（不是超时而是宕机），中断请求会失败。

**优化方向**：
1. 中断失败后降级为强制更新状态（不等执行器确认）
2. 结合心跳检测判断节点存活状态
3. 引入"死亡检测"机制：连续 N 次中断失败 → 判定节点死亡 → 直接回收任务

### 12.6 Offset 分页的一致性

**问题**：ShardingCompensator 使用 offset 分页，如果在遍历过程中有新的分片父任务被创建或旧的被完成，offset 可能导致遗漏或重复。

**V2 解决方案**：`ShardingCompensatorV2` 使用 `syncx.Map[string, int]` 为每个分表维护独立的 offset。

---

## 13. 部署验证

### 13.1 验证重试补偿器

```bash
# 1. 启动全部中间件
make e2e_up

# 2. 启动调度器
make run_scheduler_only

# 3. 启动执行器（配置为第 1-2 次执行返回 FAILED_RETRYABLE）
cd example/longrunning && go run main.go

# 4. 观察日志
# [scheduler] 重试补偿器启动
# [scheduler] 找到可重试任务 count=1
# [scheduler] 更新重试状态成功 retryCount=1 nextRetryTime=xxx
# [scheduler] 找到可重试任务 count=1  (第二次重试)
# [scheduler] 更新重试状态成功 retryCount=2 nextRetryTime=xxx
# [scheduler] 找到可重试任务 count=1  (第三次重试)
# [scheduler] 更新调度状态成功 status=SUCCESS
```

### 13.2 验证中断补偿器

```bash
# 配置任务 MaxExecutionSeconds=10（10秒超时）
# 启动执行器（模拟永不结束的任务）

# 观察日志
# [scheduler] 中断补偿器启动
# ... 10秒后 ...
# [scheduler] 找到可中断任务 count=1
# [scheduler] 成功中断超时任务 executionId=xxx taskName=xxx
```

### 13.3 验证分片聚合补偿器

```bash
# 启动执行器（分片模式）
cd example/sharding && go run main.go

# 观察日志
# [scheduler] 分片任务补偿器启动
# [scheduler] 找到分片父任务 count=1
# [scheduler] 子任务未全部完成，等待下次补偿... parentId=xxx
# ... 一段时间后 ...
# [scheduler] 所有子任务成功，父任务标记为SUCCESS parentId=xxx
```

---

## 14. 验收 Checklist

| # | 检查项 | 状态 |
|---|--------|------|
| 1 | 重试补偿器能发现 `FAILED_RETRYABLE` 状态的执行记录 | ✅ |
| 2 | 指数退避计算正确（1s → 2s → 4s） | ✅ |
| 3 | 达到最大重试次数后状态变为 `FAILED` | ✅ |
| 4 | 重试时排除了上次失败的执行节点 | ✅ |
| 5 | 重调度补偿器能发现 `FAILED_RESCHEDULED` 状态的记录 | ✅ |
| 6 | 重调度时指定到原执行节点 | ✅ |
| 7 | 中断补偿器能检测到超过 deadline 的 RUNNING 记录 | ✅ |
| 8 | gRPC Interrupt 请求发送成功，状态正确更新 | ✅ |
| 9 | 分片聚合补偿器能汇总子任务状态 | ✅ |
| 10 | 父任务终态后释放 CAS 锁 | ✅ |
| 11 | 防空转机制工作正常（MinDuration） | ✅ |
| 12 | 优雅停机时补偿器能正确退出 | ✅ |
| 13 | 分片子任务重试时不重新抢占 | ✅ |
| 14 | 4 个补偿器在 ioc/task.go 中正确注册 | ✅ |

---

## 15. 面试高频问题与参考回答

### Q1：为什么需要四种补偿器，而不是一个通用的补偿器？

**参考回答**：

> 四种补偿器虽然框架结构类似（循环扫描 + 批量处理 + 防空转），但它们的**触发条件、处理逻辑、依赖组件**完全不同：
>
> - 重试需要计算退避时间、排除故障节点
> - 重调度需要指定目标节点、合并调度参数
> - 中断需要直接操作 gRPC 客户端发送信号
> - 分片聚合需要跨记录汇总状态、释放锁
>
> 如果硬塞成一个通用补偿器，会导致大量 `if-else` 分支，违反单一职责原则。分离后每个补偿器可以独立调整 batchSize、MinDuration 等参数，也可以独立扩缩容（V2 中通过分布式锁实现）。

### Q2：指数退避重试策略为什么要设计 `NextWithRetries`（无状态）和 `Next`（有状态）两种模式？

**参考回答**：

> 这是因为重试发生在两个截然不同的场景：
>
> 1. **补偿器场景（无状态）**：补偿器从数据库读取 `retryCount`，需要传入次数计算间隔。多个调度节点可能轮流执行同一个补偿器，内存中的计数器没有意义。
>
> 2. **单次 RPC 调用链（有状态）**：比如 Invoker 调用失败后立即重试 3 次，这种场景在单个 goroutine 中完成，用内部 atomic 计数器更方便。
>
> 本质上是**分布式状态 vs 进程内状态**的区别。分布式场景下状态必须持久化到数据库，不能依赖内存。

### Q3：重试和重调度在节点路由上有什么区别？怎么实现的？

**参考回答**：

> - 重试（Retry）通过 `WithExcludedNodeID` 在 gRPC Context 中注入要**排除**的节点 ID。自定义的负载均衡器在选择节点时会跳过该节点。
>
> - 重调度（Reschedule）通过 `WithSpecificNodeID` 在 Context 中注入要**指定**的节点 ID。负载均衡器会优先选择该节点。
>
> 这个设计的巧妙之处在于**完全非侵入**：不需要修改 Invoker 接口或 gRPC 协议，只通过 Context 元数据和自定义 Balancer 实现。补偿器甚至不感知路由细节，它只知道调用 `Runner.Retry` 或 `Runner.Reschedule`。

### Q4：中断补偿器为什么不通过 Runner 发起，而是直接持有 gRPC 客户端？

**参考回答**：

> Runner 的职责是"抢占 → 创建执行记录 → 触发执行"的完整调度流程。而中断是一个**信号操作**，不需要抢占任务，也不需要创建新的执行记录。如果硬套 Runner 接口，需要在 Runner 中添加一个 `Interrupt` 方法，但这个方法的流程与 `Run/Retry/Reschedule` 完全不同，反而破坏了 Runner 接口的一致性。
>
> 所以中断补偿器直接持有 `grpc.ClientsV2`，按服务名获取客户端，发送 `InterruptRequest`。这是一个更清晰的职责边界。

### Q5：分片任务重试时为什么不需要重新抢占？

**参考回答**：

> 分片任务的锁（CAS 抢占）在 `Runner.Run` 时由**父任务**获取，一直持有到所有子任务完成后由 `ShardingCompensator.releaseTask` 释放。如果子任务重试时还要抢占，有两个问题：
>
> 1. **必定失败**：任务已经处于 `PREEMPTED` 状态（version 已经递增），`WHERE version=?` 条件不满足
> 2. **逻辑矛盾**：如果子任务自行释放锁再抢占，其他调度节点可能在中间窗口抢走这个任务
>
> 所以 `isShardedTaskExecution` 检查 `ShardingParentID > 0`，分片子任务直接跳过抢占步骤。

### Q6：V1 补偿器在多节点部署下有什么问题？怎么解决？

**参考回答**：

> V1 的核心问题是**缺乏互斥**。多个调度节点各自运行独立的补偿器实例，会出现：
>
> 1. **重复处理**：两个节点同时扫描到同一条记录并发起重试
> 2. **资源浪费**：所有节点都在全量扫描，数据库压力线性增长
>
> V2 通过 `ShardingLoopJob` 框架解决：
> - **分布式锁**：每个分表一把锁，同一时刻只有一个节点处理该分表
> - **资源信号量**：控制单节点的并发补偿 goroutine 数量
> - **分片策略**：2 库 × 4 表 = 8 个分片，天然支持 8 个节点并行工作

---

## 16. 总结

Step 4 实现了分布式任务调度系统中最关键的**容错层**：

| 补偿器 | 对抗的故障 | 核心机制 |
|--------|----------|---------|
| RetryCompensator | 网络抖动、临时错误 | 指数退避 + 排除故障节点 |
| RescheduleCompensator | 节点恢复后续执行 | 指定原节点 + 参数合并 |
| InterruptCompensator | 任务超时 / 死锁 | Deadline + gRPC 中断信号 |
| ShardingCompensator | 分片子任务完成汇总 | Offset 分页 + Any-Failed 聚合 |

V1 版本采用简单轮询模式，优先保证**可靠性**和**实现简洁性**。已识别的不足（并发安全、扫描效率、惊群效应）将在 Step 7（补偿机制 V2 + ShardingLoopJob 框架）中通过分布式锁和分片并发扫描彻底解决。

---

> **下一步（Step 5：DAG 工作流引擎）** 将基于 ANTLR4 构建声明式 DSL 解析器，支持任务依赖编排、并行/串行/条件分支执行，以及基于完成事件的后继任务驱动。
