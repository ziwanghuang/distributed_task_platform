# 项目技术难点与 Bug 分析

## 最难的两个技术点

---

### 难点一：ShardingLoopJob 分片循环框架中的时间约束与锁生命周期管理

**文件**：`pkg/loopjob/shardingloopjob.go`

这是整个 V2 架构的基座，也是最容易写出 bug 的地方。它的难度不在于某一行代码很复杂，而在于**多个异步组件之间的时间约束必须严格成立**，少一个条件整个系统就会出现脑裂。

#### 核心架构

```
Run() → 外层无限循环
  └─ 遍历所有分片（Broadcast: dbSharding × tableSharding 的笛卡尔积）
       ├─ Acquire 信号量（控制并发上限，防止单节点吃太多分片）
       ├─ NewLock + Lock（Redis 分布式锁，分片级互斥）
       └─ go tableLoop（启动独立 goroutine）
            └─ bizLoop（在锁保护下持续执行业务）
                 ├─ biz(ctx)（带 50s 超时）
                 └─ lock.Refresh（续约分布式锁）
```

#### 为什么难

**1）时间约束是隐式的，但必须严格成立**

```go
const bizTimeout = 50 * time.Second  // 业务超时
// retryInterval = 1 * time.Minute   // 锁 TTL
```

必须保证 `bizTimeout(50s) < retryInterval/锁TTL(60s)`。如果业务执行超过 60s，锁过期了但 goroutine 还在跑，另一个节点拿到锁也开始执行同一个分片——脑裂。这个约束没有编译期保护，完全靠开发者自己确保。

**2）锁释放必须用 Background Context**

```go
// tableLoop 第 185 行
unCtx, cancel := context.WithTimeout(context.Background(), l.defaultTimeout)
unErr := lock.Unlock(unCtx)
```

这里用 `context.Background()` 而不是传入的 `ctx`。原因：`bizLoop` 退出可能是因为 `ctx` 被取消（优雅停机），此时 `ctx` 已经 Done，如果用它去 Unlock 会直接失败，导致锁泄漏——其他节点要等整整 60s 的 TTL 才能拿到这个分片。这是一个**极其容易写错**的点。

**3）信号量泄漏的防御性编程**

```go
func (l *ShardingLoopJob) Run(ctx context.Context) {
    // ...
    err := l.resourceSemaphore.Acquire(ctx)  // 获取信号量
    lock, err := l.dclient.NewLock(...)
    if err != nil {
        err = l.resourceSemaphore.Release(ctx)  // 锁初始化失败，必须释放信号量
        continue
    }
    err = lock.Lock(lockCtx)
    if err != nil {
        err = l.resourceSemaphore.Release(ctx)  // 加锁失败，必须释放信号量
        continue
    }
    go l.tableLoop(...)  // 成功时，信号量在 tableLoop 的 defer 里释放
}
```

三条路径都需要保证信号量最终被释放：
- 锁初始化失败 → 当场释放
- 加锁失败 → 当场释放
- 加锁成功 → defer 在 goroutine 结束时释放

漏掉任何一条路径，信号量就会被耗尽，导致该节点再也无法处理新的分片。

**4）三层生命周期嵌套**

```
Run（进程级）
  └─ tableLoop（分片级，一个 goroutine）
       └─ bizLoop（业务级，在锁保护下循环）
```

退出时需要反向清理：bizLoop 退出 → tableLoop 释放锁 + 释放信号量 → Run 遍历下一个分片。任何一层的清理遗漏都会导致资源泄漏。

---

### 难点二：执行状态机中 FailedRetryable 的多路径状态转换

**文件**：`internal/service/task/execution_service.go`（第 254-410 行）

#### 核心状态流

```
PREPARE → RUNNING → SUCCESS（终态）
                  → FAILED（终态）
                  → FAILED_RETRYABLE ← → RUNNING（重试循环）
                  → FAILED_RESCHEDULED → RUNNING（重调度）
```

#### 为什么难

**1）终态保护 + 非终态的多路径分支**

```go
func (s *executionService) UpdateState(ctx context.Context, state domain.ExecutionState) error {
    execution, err := s.FindByID(ctx, state.ID)
    // ...
    if execution.Status.IsTerminalStatus() {
        return errs.ErrInvalidTaskExecutionStatus  // 终态保护
    }
    switch {
    case state.Status.IsRunning():
        if execution.Status.IsRunning() {
            return s.updateRunningProgress(ctx, state)  // 路径A：Running→Running，仅更新进度
        }
        return s.setRunningState(ctx, state)             // 路径B：Prepare/FailedRetryable→Running
    case state.Status.IsFailedRetryable():
        err = s.updateRetryState(ctx, execution, state)  // 路径C：计算重试 or 强制终止
        // ...
    case state.Status.IsFailedRescheduled():
        // 路径D
    case state.Status.IsTerminalStatus():
        // 路径E：终态处理 + 释放锁 + 更新下次时间 + 发送事件
    }
}
```

这里的难点是 **同一个状态（Running）对应两种完全不同的处理路径**（更新进度 vs 首次进入运行态），而且 **FailedRetryable 既可能留在非终态也可能被强制转为终态**。

**2）FailedRetryable 的"达到最大重试"逻辑**

```go
func (s *executionService) updateRetryState(ctx context.Context, execution domain.TaskExecution, state domain.ExecutionState) error {
    retryStrategy, _ := retry.NewRetry(execution.Task.RetryConfig.ToRetryComponentConfig())
    duration, shouldRetry := retryStrategy.NextWithRetries(int32(execution.RetryCount + 1))
    if shouldRetry {
        execution.NextRetryTime = time.Now().Add(duration).UnixMilli()
        execution.RetryCount++
    } else if !state.Status.IsTerminalStatus() {
        state.Status = domain.TaskExecutionStatusFailed  // 关键：强制改状态
    }
    // 不管是否达到最大重试次数，都要更新数据库（主要是重试次数）
    err := s.UpdateRetryResult(ctx, state.ID, execution.RetryCount, ...)
    if !shouldRetry {
        return errs.ErrExecutionMaxRetriesExceeded  // 返回特殊错误
    }
    return nil
}
```

**无论是否超限都要更新数据库**——这是一个违反直觉但必须的设计。如果超限时不更新 `RetryCount`，补偿器下次扫描时还会认为可以重试，造成无限重试。

**3）终态处理中分片任务的特殊路径**

```go
case state.Status.IsTerminalStatus():
    s.updateState(ctx, execution, state)
    isShardedTask := execution.ShardingParentID != nil && *execution.ShardingParentID > 0
    if !isShardedTask {
        s.releaseTask(ctx, execution.Task)          // 普通任务：释放锁
        s.taskSvc.UpdateNextTime(ctx, execution.Task.ID)  // 更新下次执行时间
    }
    // 分片任务不释放不更新——由 ShardingCompensator 统一处理
    s.sendCompletedEvent(ctx, state, execution)      // 但事件要发
```

分片子任务到了终态**不能释放锁也不能更新下次时间**，因为还有其他子任务在跑。锁由父任务持有，直到 ShardingCompensator 汇总所有子任务后统一释放。但**完成事件必须发**，否则 DAG 后继任务不会被触发。

---

## 最难的两个 Bug

---

### Bug 一：续约与抢占的时间窗口竞态——任务"被执行两次"

#### 问题场景

```
时间线：
t0: 节点A 抢占 Task#1，version=5 → 变为 version=6
t1: 节点A 续约 Task#1，version=6 → 变为 version=7（utime 刷新）
t2: 节点A 的续约因为网络抖动失败了两轮
t3: Task#1 的 utime 超过了 preemptedTimeoutMs 阈值
t4: 调度循环扫描到 Task#1（PREEMPTED 且超时），判定为"僵尸任务"
t5: 节点B 用 version=7 成功抢占 → version=8，开始执行
t6: 节点A 网络恢复，续约成功（WHERE schedule_node_id=A AND status=PREEMPTED）
    ⚠️ 但此时 schedule_node_id 已经是B了，续约实际上影响行数=0
t7: 但节点A 的执行 goroutine 还在跑！它不知道自己的锁已经丢了
```

#### 根因分析

```go
// scheduler.go 第 123-127 行
go func() {
    state, runErr := s.invoker.Run(ctx, execution)  // 异步执行
    // 这个 goroutine 不知道续约是否失败
    // 也不知道 Task 是否已经被其他节点抢占
    s.execSvc.UpdateState(ctx, state)  // 可能覆盖节点B的执行结果
}()
```

异步执行的 goroutine **没有感知续约失败的能力**。续约失败只在 `renewLoop` 里打了一条 Error 日志：

```go
func (s *Scheduler) renewLoop() {
    // ...
    err := s.acquirer.Renew(s.ctx, s.nodeID)
    if err != nil {
        s.logger.Error("批量续约失败", elog.FieldErr(err))
        // 就完了，没有通知正在执行的 goroutine
    }
}
```

而且续约是**批量续约**（WHERE schedule_node_id=? AND status=PREEMPTED），不会逐个检查每个任务是否还属于自己。

#### 实际后果

1. 同一个任务被两个节点同时执行（脑裂）
2. 两个执行结果可能互相覆盖（状态机混乱）
3. 最终状态不确定：可能一个写 SUCCESS 一个写 FAILED

#### 修复方向

```go
// 方案：在 UpdateState 时加上 schedule_node_id 校验
func (s *executionService) UpdateState(ctx context.Context, state domain.ExecutionState) error {
    execution, err := s.FindByID(ctx, state.ID)
    // 检查当前 Task 是否仍被本节点持有
    task, _ := s.taskSvc.GetByID(ctx, execution.Task.ID)
    if task.ScheduleNodeID != s.nodeID {
        // 任务已被其他节点接管，丢弃本次状态更新
        return errs.ErrTaskOwnershipLost
    }
    // ... 正常处理
}
```

---

### Bug 二：ShardingCompensator 的 offset 分页遍历 + 数据变化 = 跳过/重复处理

#### 问题场景

```go
// compensator/sharding.go 第 67-113 行
func (r *ShardingCompensator) Start(ctx context.Context) {
    offset := 0
    for {
        executions, _ := r.execSvc.FindShardingParents(ctx, offset, r.config.BatchSize)
        if len(executions) == 0 {
            offset = 0  // 到头了，从头开始
            continue
        }
        for i := range executions {
            r.handle(ctx, executions[i])  // handle 可能把父任务从 RUNNING 改成 SUCCESS/FAILED
        }
        offset += len(executions)
    }
}
```

**问题**：`handle` 方法会把已完成的父任务状态改为 SUCCESS/FAILED，使其不再满足查询条件（`status = RUNNING`）。但 offset 还在递增。

#### 具体竞态过程

```
假设有 5 个 Running 的分片父任务：[P1, P2, P3, P4, P5]，batchSize=2

第1轮：offset=0，查到 [P1, P2]
  - handle(P1) → 子任务全部完成，P1 变为 SUCCESS ✅
  - handle(P2) → 还有子任务在跑，跳过
  - offset = 0 + 2 = 2

第2轮：offset=2
  ⚠️ 此时 RUNNING 列表变成了 [P2, P3, P4, P5]（P1 已移除）
  - SELECT ... WHERE status='RUNNING' ORDER BY utime OFFSET 2 LIMIT 2
  - 实际查到的是 [P4, P5]，P3 被跳过了！

第3轮：offset=4
  - 查到空列表，重置 offset=0
  - 下一轮才能处理到 P3
```

#### 根因

使用 **offset 分页 + 可变数据集** 的经典问题。每次 handle 成功会改变结果集大小，但 offset 是线性递增的，导致某些记录被跳过。

**V2 版本**（shardingv2.go）也有同样的问题，只是用了 `syncx.Map` 存储每个分片表的 offset，本质没变。

#### 实际后果

1. 某些分片父任务的汇总被延迟处理（下一个完整遍历周期才能覆盖到）
2. 如果任务量大，延迟可能达到分钟级
3. 不会造成数据丢失（最终会处理到），但会影响任务完成的时效性

#### 修复方向

```go
// 方案A：不用 offset，用游标（基于 ID 或 utime）
func (r *ShardingCompensator) Start(ctx context.Context) {
    var lastID int64 = 0
    for {
        executions, _ := r.execSvc.FindShardingParentsAfterID(ctx, lastID, r.config.BatchSize)
        if len(executions) == 0 {
            lastID = 0  // 回到起点
            time.Sleep(r.config.MinDuration)
            continue
        }
        for i := range executions {
            r.handle(ctx, executions[i])
        }
        lastID = executions[len(executions)-1].ID
    }
}

// 方案B：处理成功后不递增 offset（因为数据已经从结果集移除了）
offset += len(executions) - successCount
```

---

## 总结

| 维度 | 难点一：ShardingLoopJob | 难点二：执行状态机 |
|------|----------------------|------------------|
| 核心挑战 | 分布式锁 + 信号量 + goroutine 三层资源的生命周期对齐 | 非线性状态转换 + 分片/非分片的路径分叉 |
| 容易出错的点 | 锁释放用错 context、信号量泄漏、时间约束不满足 | FailedRetryable 的双路径、分片子任务不释放锁 |
| 调试难度 | ⭐⭐⭐⭐⭐（分布式 + 异步，几乎无法本地复现） | ⭐⭐⭐⭐（状态组合多，边界情况需要覆盖） |

| 维度 | Bug一：续约时间窗口竞态 | Bug二：offset分页数据漂移 |
|------|-------------------|--------------------|
| 触发条件 | 网络抖动导致续约失败 2+ 轮 | 正常补偿流程就会触发 |
| 后果严重度 | 🔴 高（任务被执行两次，结果覆盖） | 🟡 中（延迟处理，不丢数据） |
| 修复难度 | 需要在异步 goroutine 中引入 ownership 校验 | 将 offset 改为基于 ID 的游标 |
| 是否会在面试中被问到 | 绝对会——"你的分布式锁/续约怎么保证一致性？" | 可能会——"你的补偿器怎么保证不遗漏？" |
