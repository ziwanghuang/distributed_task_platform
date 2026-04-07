# Step 11：并发安全 — 状态机 CAS 与资源控制

> **目标**：消除 ExecutionService 状态更新中的 TOCTOU 竞态，用 CAS 统一所有状态迁移路径；用 Worker Pool 替换 NormalTaskRunner 中不受控的 goroutine；修复 ResourceSemaphore 的下溢 Bug。
>
> **完成后你能看到**：UpdateState 全面使用 `WHERE status = ? AND version = ?` 原子更新，并发状态变更不再丢失；Runner 的 goroutine 数量被 ants pool 严格限制，`GracefulStop()` 能等待所有运行中的 goroutine 退出；信号量 Release 不会因重复调用导致 curCount 变负数。

---

## 1. 架构总览

Step 11 聚焦于**并发安全**层面的三个核心改进，都是将隐性的运行时风险转化为编译期或运行时可检测的保护机制。

```
┌──────────────────────────────────────────────────────────────────────────────┐
│                         并发安全改进全景                                      │
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │                    ExecutionService.UpdateState                      │    │
│  │                                                                     │    │
│  │  Before: FindByID → Check → Update         (TOCTOU 竞态窗口)       │    │
│  │                                                                     │    │
│  │  After:  UPDATE ... SET status=?, version=version+1                 │    │
│  │          WHERE id=? AND status=? AND version=?   (CAS 原子操作)     │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │                    NormalTaskRunner goroutine 管理                    │    │
│  │                                                                     │    │
│  │  Before: go func() { ... }    (无限制、无追踪、无停机感知)          │    │
│  │                                                                     │    │
│  │  After:  ┌────────────────┐                                         │    │
│  │          │   ants Pool     │ ← maxWorkers = CPU * 2                 │    │
│  │          │   + WaitGroup   │ ← GracefulStop 等待所有任务完成         │    │
│  │          │   + ctx 感知    │ ← context 取消时拒绝新提交              │    │
│  │          └────────────────┘                                         │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │                    ResourceSemaphore 下溢防护                        │    │
│  │                                                                     │    │
│  │  Before: curCount--                    (无下限检查，可变负)          │    │
│  │                                                                     │    │
│  │  After:  if curCount <= 0 { warn; return }                          │    │
│  │          curCount--                                                  │    │
│  │          + RWMutex → Mutex  (从未用 RLock，简化)                     │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
└──────────────────────────────────────────────────────────────────────────────┘
```

**设计原则**：与项目已有的乐观锁风格保持一致——任务抢占用 `WHERE version = ?`，状态更新也统一用 CAS；资源控制用 Pool + WaitGroup，和 Scheduler 的优雅停机模式保持一致。

---

## 2. 变更对比

| 维度 | 优化前 | Step 11 优化后 |
|------|--------|----------------|
| **状态更新** | FindByID → 校验 → Update（TOCTOU） | `UpdateWithCAS`: WHERE id=? AND status=? AND version=? |
| **并发控制** | `go func()` 无限制 | ants Worker Pool + maxWorkers 配置化 |
| **停机感知** | goroutine 无追踪 | WaitGroup + 带超时等待 |
| **信号量安全** | `curCount--` 无下限 | `curCount <= 0` 检查 + 告警日志 |
| **锁类型** | `sync.RWMutex`（只用 Lock） | `sync.Mutex`（简化） |
| **影响文件** | — | `execution_service.go`, `normal_task_runner.go`, `resource.go`, `task_execution_dao.go` |

---

## 3. 详细设计

### 3.1 ExecutionService.UpdateState 的 TOCTOU 竞态 → CAS 统一

#### 3.1.1 问题分析

**现状代码**（`internal/service/task/execution_service.go:254`）：

```go
func (s *executionService) UpdateState(ctx context.Context, state domain.ExecutionState) error {
    // Step 1: 查询当前状态
    execution, err := s.FindByID(ctx, state.ID)
    if err != nil {
        return errs.ErrExecutionNotFound
    }

    // Step 2: 校验状态转换合法性
    if execution.Status.IsTerminalStatus() {
        return errs.ErrInvalidTaskExecutionStatus
    }

    // ⚠️ 竞态窗口：Step 1 到 Step 3 之间，其他 goroutine/节点可能已修改状态
    
    // Step 3: 执行更新
    switch {
    case state.Status.IsRunning():
        return s.setRunningState(ctx, state)
    // ...
    }
}
```

**TOCTOU（Time-of-Check-Time-of-Use）竞态场景**：

```
时间线     Goroutine A (MQ Consumer)           Goroutine B (Retry Compensator)
───────────────────────────────────────────────────────────────────────────
  t0       FindByID → status=RUNNING           
  t1       检查: !IsTerminal ✅                 FindByID → status=RUNNING
  t2                                            检查: !IsTerminal ✅
  t3       Update → status=FAILED ✅             
  t4                                            Update → status=SUCCESS ✅  ← 错误！
                                                (应该被拒绝，FAILED 不应转 SUCCESS)
```

**核心问题**：查询和更新不是原子的。在高并发场景下，MQ Consumer 和补偿器可能同时处理同一条执行记录的状态上报，导致非法状态转换。

#### 3.1.2 项目已有的 CAS 模式

项目中任务抢占已经使用了标准的 CAS 模式（`internal/repository/dao/task.go:131`）：

```go
// Acquire — 已有的 CAS 实现
result := tx.Model(&Task{}).
    Where("id = ? AND version = ?", id, version).
    Updates(map[string]any{
        "status":           StatusPreempted,
        "schedule_node_id": scheduleNodeID,
        "version":          gorm.Expr("version + 1"),
    })
if result.RowsAffected == 0 {
    return errs.ErrTaskPreemptFailed  // CAS 失败 → 被其他节点抢先
}
```

这个模式经过了生产验证，状态更新应该复用同样的思路。

#### 3.1.3 优化方案：UpdateWithCAS

**DAO 层新增 CAS 更新方法**：

```go
// internal/repository/dao/task_execution.go

// UpdateStatusWithCAS 通过 CAS 原子更新执行记录状态。
// SQL: UPDATE task_executions 
//      SET status=?, version=version+1, utime=? 
//      WHERE id=? AND status=? AND version=?
// 返回受影响行数，0 表示 CAS 失败（状态已被其他 goroutine 修改）。
func (dao *GORMTaskExecutionDAO) UpdateStatusWithCAS(
    ctx context.Context, 
    id int64, 
    oldStatus, newStatus string, 
    version int64,
    updates map[string]any,
) (int64, error) {
    updates["status"] = newStatus
    updates["version"] = gorm.Expr("version + 1")
    updates["utime"] = time.Now().UnixMilli()

    result := dao.db.WithContext(ctx).
        Model(&TaskExecution{}).
        Where("id = ? AND status = ? AND version = ?", id, oldStatus, version).
        Updates(updates)
    return result.RowsAffected, result.Error
}
```

**Service 层改造**：

```go
// internal/service/task/execution_service.go

func (s *executionService) UpdateState(ctx context.Context, state domain.ExecutionState) error {
    // 仍然需要 FindByID 获取完整的执行记录信息（用于后续业务逻辑）
    execution, err := s.FindByID(ctx, state.ID)
    if err != nil {
        return errs.ErrExecutionNotFound
    }

    // 快速拒绝：已终态的记录不允许再迁移（这步检查保留，减少无效 CAS 尝试）
    if execution.Status.IsTerminalStatus() {
        return errs.ErrInvalidTaskExecutionStatus
    }

    switch {
    case state.Status.IsRunning():
        if execution.Status.IsRunning() {
            return s.updateRunningProgress(ctx, state)
        }
        // CAS：只有当前状态仍为 PREPARE 时才能转为 RUNNING
        affected, casErr := s.repo.UpdateStatusWithCAS(ctx, 
            state.ID, 
            execution.Status,   // oldStatus
            state.Status,       // newStatus
            execution.Version,  // version
        )
        if casErr != nil {
            return casErr
        }
        if affected == 0 {
            return errs.ErrConcurrentStateUpdate
        }
        return nil

    case state.Status.IsTerminalStatus():
        // CAS：只有当前状态仍为 execution.Status 时才能转为终态
        affected, casErr := s.repo.UpdateStatusWithCAS(ctx, 
            state.ID, 
            execution.Status, 
            state.Status, 
            execution.Version,
        )
        if casErr != nil {
            return casErr
        }
        if affected == 0 {
            return errs.ErrConcurrentStateUpdate
        }
        
        // 后续业务逻辑保持不变
        isShardedTask := execution.ShardingParentID != nil && *execution.ShardingParentID > 0
        if !isShardedTask {
            s.releaseTask(ctx, execution.Task)
            if _, err3 := s.taskSvc.UpdateNextTime(ctx, execution.Task.ID); err3 != nil {
                // ...
            }
        }
        s.sendCompletedEvent(ctx, state, execution)
        return nil
    // ... 其他 case 同理
    }
}
```

#### 3.1.4 状态机合法转换矩阵

```
                    ┌─── PREPARE ──────────────────────────────────┐
                    │         │                                     │
                    │         ▼                                     │
                    │     RUNNING ──────────────────┐              │
                    │      │    │                    │              │
                    │      │    ▼                    ▼              │
                    │      │  FAILED_RETRYABLE   SUCCESS           │
                    │      │    │                   (终态)          │
                    │      │    ▼                                   │
                    │      │  FAILED (终态, 超过最大重试次数)        │
                    │      │                                       │
                    │      ▼                                       │
                    │  FAILED_RESCHEDULED                          │
                    └──────────────────────────────────────────────┘

CAS WHERE 条件矩阵:
┌──────────────────────┬──────────────────────┬──────────┐
│ oldStatus (WHERE)     │ newStatus (SET)       │ 合法?    │
├──────────────────────┼──────────────────────┼──────────┤
│ PREPARE              │ RUNNING              │ ✅       │
│ RUNNING              │ SUCCESS              │ ✅       │
│ RUNNING              │ FAILED               │ ✅       │
│ RUNNING              │ FAILED_RETRYABLE     │ ✅       │
│ RUNNING              │ FAILED_RESCHEDULED   │ ✅       │
│ SUCCESS              │ *                    │ ❌       │
│ FAILED               │ *                    │ ❌       │
│ PREPARE              │ SUCCESS              │ ❌       │
└──────────────────────┴──────────────────────┴──────────┘
```

**CAS 如何自动保证状态机合法性**：WHERE 条件中带了 `status = ?`，如果状态已经被其他 goroutine 改为终态，`RowsAffected = 0`，更新自然失败——无需在应用层做二次校验。

#### 3.1.5 关键设计决策

| 方案 | 优点 | 缺点 | 决策 |
|------|------|------|------|
| CAS（乐观锁） | 无锁高性能；与项目现有风格一致 | 失败需要调用方处理 | ✅ 采用 |
| SELECT FOR UPDATE（悲观锁） | 强一致性 | 持锁时间长，死锁风险，与现有风格不一致 | ❌ |
| 分布式锁（Redis/etcd） | 跨进程安全 | 引入额外依赖，延迟高 | ❌ |
| 数据库唯一索引约束 | 自动去重 | 无法表达状态转换逻辑 | ❌ |

---

### 3.2 NormalTaskRunner goroutine 泄漏 → Worker Pool

#### 3.2.1 问题分析

**现状代码**（`internal/service/runner/normal_task_runner.go`）：

```go
// handleNormalTask — 无限制地启动 goroutine
func (s *NormalTaskRunner) handleNormalTask(ctx context.Context, task domain.Task) error {
    execution, err := s.execSvc.Create(ctx, ...)
    if err != nil { ... }

    go func() {  // ← 问题 1：没有并发数限制
        state, runErr := s.invoker.Run(ctx, execution)
        // ...
    }()  // ← 问题 2：没有 WaitGroup 追踪
    return nil
}

// handleShardingTask — 同样的问题
func (s *NormalTaskRunner) handleShardingTask(ctx context.Context, task domain.Task) error {
    for i := range executions {
        execution := executions[i]
        go func() {  // ← 每个分片一个 goroutine，N 个分片 = N 个不受控的 goroutine
            // ...
        }()
    }
    return nil
}
```

**两个核心问题**：

**问题 1 — 并发数不可控（潜在 OOM）**：

```
场景：100 个任务同时到达调度窗口
├── handleNormalTask 被调用 100 次
├── 启动 100 个 goroutine
│   ├── 每个 goroutine 持有一个 gRPC 连接
│   ├── 每个 gRPC 调用可能耗时数分钟（长时间任务）
│   └── 100 个并发 gRPC 连接 + 每个连接的内存开销
└── 如果执行节点响应慢 → goroutine 堆积 → 内存持续增长 → OOM
```

**问题 2 — 优雅停机感知不到**：

```
停机流程：
  1. Scheduler.GracefulStop() 调用 cancel()
  2. scheduleLoop 退出
  3. ❌ 但 handleNormalTask 启动的 goroutine 还在运行
  4. 进程 exit → 运行中的 Invoker.Run() 被强制中断
  5. 执行记录卡在 RUNNING 状态 → 需要 InterruptCompensator 兜底
```

#### 3.2.2 优化方案：ants Worker Pool + WaitGroup

```go
// internal/service/runner/normal_task_runner.go

import (
    "github.com/panjf2000/ants/v2"
    "runtime"
    "sync"
    "time"
)

type NormalTaskRunner struct {
    nodeID       string
    taskSvc      task.Service
    execSvc      task.ExecutionService
    taskAcquirer acquirer.TaskAcquirer
    invoker      invoker.Invoker
    producer     event.CompleteProducer

    pool   *ants.Pool     // worker pool，限制并发 goroutine 数
    wg     sync.WaitGroup // 追踪所有运行中的任务 goroutine
    logger *elog.Component
}

func NewNormalTaskRunner(
    nodeID string,
    taskSvc task.Service,
    execSvc task.ExecutionService,
    taskAcquirer acquirer.TaskAcquirer,
    invoker invoker.Invoker,
    producer event.CompleteProducer,
    maxWorkers int,  // 新增参数：最大并发数
) *NormalTaskRunner {
    if maxWorkers <= 0 {
        maxWorkers = runtime.NumCPU() * 2
    }
    pool, _ := ants.NewPool(maxWorkers, 
        ants.WithPreAlloc(true),           // 预分配 goroutine
        ants.WithNonblocking(false),       // 阻塞等待可用 worker
        ants.WithExpiryDuration(time.Minute), // 空闲 worker 1 分钟回收
    )
    return &NormalTaskRunner{
        nodeID:       nodeID,
        taskSvc:      taskSvc,
        execSvc:      execSvc,
        taskAcquirer: taskAcquirer,
        invoker:      invoker,
        producer:     producer,
        pool:         pool,
        logger:       elog.DefaultLogger.With(elog.FieldComponentName("runner.NormalTaskRunner")),
    }
}
```

**handleNormalTask 改造**：

```go
func (s *NormalTaskRunner) handleNormalTask(ctx context.Context, task domain.Task) error {
    execution, err := s.execSvc.Create(ctx, domain.TaskExecution{
        Task:      task,
        StartTime: time.Now().UnixMilli(),
        Status:    domain.TaskExecutionStatusPrepare,
    })
    if err != nil {
        s.releaseTask(ctx, task)
        return err
    }

    // 通过 worker pool 提交，而非直接 go func()
    s.wg.Add(1)
    err = s.pool.Submit(func() {
        defer s.wg.Done()
        
        state, runErr := s.invoker.Run(ctx, execution)
        if runErr != nil {
            s.logger.Error("执行器执行任务失败", elog.FieldErr(runErr))
            return
        }
        if updateErr := s.execSvc.UpdateState(ctx, state); updateErr != nil {
            s.logger.Error("正常调度任务失败",
                elog.Any("execution", execution),
                elog.Any("state", state),
                elog.FieldErr(updateErr))
        }
    })
    if err != nil {
        s.wg.Done()
        s.logger.Error("提交任务到 worker pool 失败", elog.FieldErr(err))
        return err
    }
    return nil
}
```

**handleShardingTask 改造**：

```go
func (s *NormalTaskRunner) handleShardingTask(ctx context.Context, task domain.Task) error {
    executions, err := s.execSvc.CreateShardingChildren(ctx, domain.TaskExecution{
        Task:      task,
        StartTime: time.Now().UnixMilli(),
        Status:    domain.TaskExecutionStatusRunning,
    })
    if err != nil {
        s.releaseTask(ctx, task)
        return err
    }

    for i := range executions {
        execution := executions[i]
        s.wg.Add(1)
        submitErr := s.pool.Submit(func() {
            defer s.wg.Done()
            
            state, runErr := s.invoker.Run(
                s.WithSpecificNodeIDContext(ctx, execution.ExecutorNodeID), execution)
            if runErr != nil {
                s.logger.Error("执行器执行任务失败", elog.FieldErr(runErr))
                return
            }
            if updateErr := s.execSvc.UpdateState(ctx, state); updateErr != nil {
                s.logger.Error("正常调度子任务失败",
                    elog.Any("execution", execution),
                    elog.Any("state", state),
                    elog.FieldErr(updateErr))
            }
        })
        if submitErr != nil {
            s.wg.Done()
            s.logger.Error("提交分片任务到 worker pool 失败",
                elog.Int64("executionID", execution.ID),
                elog.FieldErr(submitErr))
        }
    }
    return nil
}
```

**GracefulStop 方法**：

```go
// GracefulStop 优雅停止 Runner，等待所有运行中的任务完成。
// 带 30 秒超时保护，避免无限等待卡死进程。
func (s *NormalTaskRunner) GracefulStop() {
    // 拒绝新任务提交
    s.pool.Release()

    // 带超时等待所有运行中的任务
    done := make(chan struct{})
    go func() {
        s.wg.Wait()
        close(done)
    }()

    select {
    case <-done:
        s.logger.Info("all task goroutines stopped gracefully")
    case <-time.After(30 * time.Second):
        s.logger.Warn("graceful stop timed out, some tasks may be interrupted")
    }
}
```

#### 3.2.3 maxWorkers 配置化

```yaml
# config/config.yaml
runner:
  maxWorkers: 16  # CPU * 2，可根据执行节点承载能力调整
```

**配置指南**：

| 场景 | 推荐值 | 理由 |
|------|--------|------|
| 轻量级任务（HTTP 回调） | CPU × 4 | I/O 密集，goroutine 大部分时间在等待网络 |
| 重量级任务（gRPC 长连接） | CPU × 2 | 每个连接占用一定内存和 CPU |
| 混合场景 | CPU × 2 | 保守值，避免执行节点过载 |

#### 3.2.4 Retry / Reschedule 同步改造

`Retry()` 和 `Reschedule()` 方法同样存在裸 `go func()` 的问题，需要同步改造：

```go
func (s *NormalTaskRunner) Retry(ctx context.Context, execution domain.TaskExecution) error {
    if !s.isShardedTaskExecution(execution) {
        acquiredTask, err := s.acquireTask(ctx, execution.Task)
        if err != nil { return err }
        execution.Task = acquiredTask
    }

    s.wg.Add(1)
    err := s.pool.Submit(func() {
        defer s.wg.Done()
        state, err1 := s.invoker.Run(
            s.WithExcludedNodeIDContext(ctx, execution.ExecutorNodeID), execution)
        if err1 != nil {
            s.logger.Error("执行器执行任务失败", elog.FieldErr(err1))
            return
        }
        if err1 = s.execSvc.UpdateState(ctx, state); err1 != nil {
            s.logger.Error("重试任务失败",
                elog.Any("execution", execution),
                elog.Any("state", state),
                elog.FieldErr(err1))
        }
    })
    if err != nil {
        s.wg.Done()
        return err
    }
    return nil
}
```

`Reschedule()` 同理，不再赘述。

---

### 3.3 ResourceSemaphore 下溢防护

#### 3.3.1 问题分析

**现状代码**（`pkg/loopjob/resource.go:66`）：

```go
func (r *MaxCntResourceSemaphore) Release(context.Context) error {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.curCount--  // ← 没有检查 curCount 是否已经 <= 0
    return nil
}
```

**下溢场景**：

```
时间线     调用序列                   curCount    后果
──────────────────────────────────────────────────────
  t0       Acquire() ✅              0 → 1       正常
  t1       Release() ✅              1 → 0       正常
  t2       Release() ← 编码 Bug     0 → -1      ⚠️ 下溢
  t3       Release()                 -1 → -2     ⚠️ 继续下溢
  t4       Acquire() ✅              -2 → -1     -1 < maxCount → 放行
  t5       Acquire() ✅              -1 → 0      0 < maxCount → 放行
  t6       Acquire() ✅              0 → 1       1 < maxCount → 放行
  t7       Acquire() ✅              1 → 2       maxCount=3, 2 < 3 → 放行
  t8       Acquire() ✅              2 → 3       3 >= 3 → 终于被限制
          → 实际并发数 = 3 + 2（下溢量）= 5，超出限制
```

**根本问题**：一次 double-release 导致信号量永久多出额度，后续所有 Acquire 检查都被污染。

#### 3.3.2 额外问题：RWMutex 的冗余

```go
type MaxCntResourceSemaphore struct {
    maxCount int
    curCount int
    mu       *sync.RWMutex  // ← 使用 RWMutex
}

func (r *MaxCntResourceSemaphore) Acquire(context.Context) error {
    r.mu.Lock()       // ← 只用 Lock()，从未使用 RLock()
    defer r.mu.Unlock()
    // ...
}

func (r *MaxCntResourceSemaphore) Release(context.Context) error {
    r.mu.Lock()       // ← 只用 Lock()
    defer r.mu.Unlock()
    // ...
}

func (r *MaxCntResourceSemaphore) UpdateMaxCount(maxCount int) {
    r.mu.Lock()       // ← 只用 Lock()
    defer r.mu.Unlock()
    // ...
}
```

三个方法全部使用 `Lock()`，从未使用 `RLock()`。`RWMutex` 比 `Mutex` 有额外的 reader-count 管理开销，且语义上暗示"有读写分离需求"，实际并没有。

#### 3.3.3 优化方案

```go
// pkg/loopjob/resource.go

type MaxCntResourceSemaphore struct {
    maxCount int
    curCount int
    mu       sync.Mutex  // ← 简化为 Mutex，不再用指针（零值即可用）
    logger   *elog.Component
}

// Acquire 尝试获取一个资源许可。
func (r *MaxCntResourceSemaphore) Acquire(context.Context) error {
    r.mu.Lock()
    defer r.mu.Unlock()
    if r.curCount >= r.maxCount {
        return errs.ErrExceedLimit
    }
    r.curCount++
    return nil
}

// Release 释放一个资源许可，加下溢防护。
func (r *MaxCntResourceSemaphore) Release(context.Context) error {
    r.mu.Lock()
    defer r.mu.Unlock()
    if r.curCount <= 0 {
        // 下溢检测：说明 Release 被多次调用，属于调用方的编码 Bug
        r.logger.Warn("ResourceSemaphore double-release detected",
            elog.Int("curCount", r.curCount),
            elog.Int("maxCount", r.maxCount))
        return nil  // 静默吞掉，不让调用方 panic
    }
    r.curCount--
    return nil
}

// UpdateMaxCount 动态更新最大许可数。
func (r *MaxCntResourceSemaphore) UpdateMaxCount(maxCount int) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.maxCount = maxCount
}

func NewResourceSemaphore(maxCount int) *MaxCntResourceSemaphore {
    return &MaxCntResourceSemaphore{
        maxCount: maxCount,
        curCount: 0,
        logger:   elog.DefaultLogger.With(elog.FieldComponentName("loopjob.semaphore")),
    }
}
```

#### 3.3.4 变更要点

| 变更 | Before | After | 理由 |
|------|--------|-------|------|
| 下溢检查 | 无 | `curCount <= 0 → warn + return` | 防止 double-release 污染信号量 |
| 锁类型 | `*sync.RWMutex`（指针，仅用 Lock） | `sync.Mutex`（值类型） | 简化，去除虚假的读写分离暗示 |
| 日志告警 | 无 | double-release 打 WARN | 帮助排查调用方 Bug |
| 返回值 | `r.curCount--` 无条件 | 下溢时返回 nil 不减 | 保持信号量语义正确 |

---

## 4. 设计决策分析

### 4.1 CAS vs SELECT FOR UPDATE

| 维度 | CAS（乐观锁） | SELECT FOR UPDATE（悲观锁） |
|------|---------------|---------------------------|
| 锁持有时间 | 无锁，瞬间完成 | 持锁直到事务结束 |
| 冲突概率低时 | 高效（一次 UPDATE） | 低效（多一次 SELECT + 行锁） |
| 冲突概率高时 | 需要重试 | 天然串行化 |
| 死锁风险 | 无 | 有（多表操作时） |
| 代码复杂度 | 低（单条 SQL） | 中（需要事务管理） |
| **项目适配** | ✅ 与抢占逻辑一致 | ❌ 风格不统一 |

**决策**：选择 CAS。原因：
1. 状态更新的冲突概率较低（同一执行记录不会频繁被多方同时更新）
2. 与项目已有的乐观锁风格完全一致
3. 无锁，不存在死锁风险

### 4.2 ants Worker Pool vs 自实现 Pool

| 方案 | 优点 | 缺点 |
|------|------|------|
| ants Pool | 成熟稳定、Star 12K+、支持预分配/非阻塞/空闲回收 | 外部依赖 |
| 自实现 channel Pool | 零依赖 | 需要自行处理 panic recovery、调优参数 |
| errgroup | 标准库、支持错误传播 | 不支持复用、每次需要新建 |

**决策**：选择 ants。理由：
1. 项目已经有大量外部依赖（ego、gorm、kafka），多一个 ants 不增加复杂度
2. ants 的 `WithNonblocking(false)` 可以在 Pool 满时阻塞，实现天然的反压
3. 支持运行时动态调整 Pool 大小

### 4.3 信号量下溢：返回 error vs 静默处理

| 方案 | 优点 | 缺点 |
|------|------|------|
| 返回 error | 强制调用方处理 | 现有调用方可能未检查 Release 错误 |
| 静默 + 日志 | 不破坏现有代码 | 问题可能被忽略 |
| panic | 立即暴露 Bug | 太激进，生产环境不合适 |

**决策**：静默 + 日志。理由：
1. Release 的现有调用方没有检查返回值，返回 error 不会被处理
2. 下溢本身是调用方的 Bug，最重要的是**让问题可见**（日志告警）
3. 不应该因为信号量的防御性检查导致整个分片处理逻辑失败

---

## 5. 面试话术

### Q1: 为什么状态更新要从先查后改改成 CAS？

*"原来的实现是典型的 TOCTOU 竞态——先 FindByID 查当前状态，校验合法性后再 Update。问题在于查和改之间有时间窗口，MQ Consumer 和补偿器可能同时处理同一条执行记录。解法是 CAS：`WHERE id=? AND status=? AND version=?`，把状态检查和更新合并为一条原子 SQL。项目里任务抢占已经用了同样的模式（`WHERE version = ?`），状态变更也应该统一。好处是无锁、高性能、无死锁风险。"*

### Q2: CAS 失败怎么处理？需要重试吗？

*"不重试。状态更新的 CAS 失败说明另一个 goroutine 已经把状态改了，这不是错误——而是正常的并发竞争结果。比如 MQ Consumer 把状态改成了 FAILED，补偿器的 CAS 自然会失败，这恰好是我们期望的行为。返回 `ErrConcurrentStateUpdate` 让调用方知道发生了什么就行。这和任务抢占的 CAS 失败一样——抢不到就不抢，不需要重试。"*

### Q3: Worker Pool 的 maxWorkers 设成多少合适？

*"取决于执行模式。如果任务是 I/O 密集型（等 gRPC 响应），可以设高一点（CPU × 4），因为 goroutine 大部分时间在 epoll wait。如果是 CPU 密集型或者执行节点承载能力有限，设 CPU × 2 就够。关键是要**配置化**——不同部署环境的机器规格不同，写死不合适。ants 还支持运行时动态调整 Pool 大小，可以结合 LoadChecker 做自适应缩放。"*

### Q4: 为什么用 ants 而不用标准库的 errgroup？

*"errgroup 每次创建就要用一次，不支持 goroutine 复用。ants 是 goroutine pool，预分配的 goroutine 可以反复使用，减少 goroutine 创建/销毁的开销。更重要的是 ants 的 `WithNonblocking(false)` 模式——Pool 满时新的 Submit 会阻塞等待，天然实现了反压。如果 100 个任务同时到达但 Pool 只有 16 个 worker，剩下的 84 个会排队等待，不会瞬间涌入执行节点造成过载。"*

### Q5: 信号量下溢是什么问题？现实中会遇到吗？

*"下溢就是 Release 被调用的次数多于 Acquire。比如 ShardingLoopJob 的分片处理代码里，如果有一条 error path 忘了 return 就 Release，就会 double-release。这会导致 curCount 变负数，后续 Acquire 检查永远不会触发 ErrExceedLimit——相当于信号量失效。防御方案很简单：Release 时检查 `curCount <= 0`，如果已经到底了就打日志不减。另外我把 RWMutex 简化成了 Mutex，因为三个方法全用 Lock()，从没 RLock()，RWMutex 只增加了认知负担和一点点性能开销。"*

### Q6: 这三个改进的优先级怎么排？

*"P1 是 TOCTOU 竞态 → CAS，因为它影响数据一致性——非法状态转换会导致任务状态混乱，补偿器也救不了。P1 是 goroutine 泄漏 → Worker Pool，因为它影响系统稳定性——极端情况下会 OOM。P2 是信号量下溢，因为它只在有编码 Bug 时才会触发，防御性修复，优先级相对较低。"*

---

## 6. 实现局限与改进方向

| # | 现状 | 改进方案 | 优先级 |
|---|------|---------|--------|
| 1 | CAS 失败直接返回错误 | 对于幂等操作可以有限重试（指数退避 + 最大 3 次） | 低 |
| 2 | ants Pool 大小静态配置 | 结合 LoadChecker 做自适应缩放（高负载缩小、低负载扩大） | 中 |
| 3 | Worker Pool 仅限 NormalTaskRunner | PlanTaskRunner 同样有裸 `go func()`，需要同步改造 | 中 |
| 4 | 状态机转换规则硬编码在 switch-case | 提取为独立的 `StateMachine` 对象，支持可视化和单元测试 | 中 |
| 5 | Retry/Reschedule 的 Pool.Submit 失败无补偿 | 提交失败的执行记录会卡在中间态，需要补偿器兜底 | 低 |
| 6 | 信号量 Release 返回 nil 掩盖了 Bug | 后续可加 Prometheus 指标 `semaphore_double_release_total` 监控 | 低 |
| 7 | TaskExecution 缺少 version 字段 | 需要在 domain + DAO + 表结构中新增 version 列支持 CAS | 高（前置条件） |
