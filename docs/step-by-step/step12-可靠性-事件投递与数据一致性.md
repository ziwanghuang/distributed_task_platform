# Step 12：可靠性 — 事件投递与数据一致性

> **优化主线**：Outbox Pattern 解决 Kafka 事件丢失 → Offset 分页一致性 → Plan 变量名 Bug → V1/V2 策略模式去重  
> **关键词**：Outbox Pattern、Cursor Pagination、错误处理、策略模式

---

## 一、架构概览

```
┌───────────── Outbox Pattern ─────────────┐
│                                          │
│  业务逻辑                                │
│    │                                     │
│    ▼                                     │
│  ┌──── DB Transaction ────┐              │
│  │ 1. UPDATE execution    │              │
│  │ 2. INSERT outbox_event │              │
│  └────────────────────────┘              │
│                                          │
│  Outbox Poller (async)                   │
│    │                                     │
│    ├── SELECT * FROM outbox_events       │
│    │   WHERE status = 'PENDING'          │
│    │                                     │
│    ├── Kafka.Send(event)                 │
│    │                                     │
│    └── UPDATE outbox_events              │
│        SET status = 'SENT'               │
└──────────────────────────────────────────┘

┌───────── Cursor Pagination ──────────────┐
│                                          │
│  传统 OFFSET：                            │
│    Page1: SELECT ... OFFSET 0 LIMIT 10   │
│    handle() → status 变更 → 记录移出结果集 │
│    Page2: SELECT ... OFFSET 10 LIMIT 10  │
│           ↑ 跳过了一条！                  │
│                                          │
│  Cursor-based：                           │
│    Page1: SELECT ... WHERE id > 0        │
│    handle() → 记录 lastID=45             │
│    Page2: SELECT ... WHERE id > 45       │
│           ↑ 不受 status 变更影响          │
└──────────────────────────────────────────┘
```

---

## 二、优化点详解

### 2.1 Outbox Pattern — Kafka 事件投递可靠性

#### 现状分析

`internal/service/task/execution_service.go` 中的事件发送：

```go
func (s *ExecutionService) sendCompletedEvent(ctx context.Context, exec domain.TaskExecution) {
    evt := event.TaskCompleted{
        TaskID:      exec.TaskID,
        ExecutionID: exec.ID,
        Status:      string(exec.Status),
    }
    err := s.producer.SendCompletedEvent(ctx, evt)
    if err != nil {
        // ❌ 只是记日志，不重试，不补偿
        s.logger.Error("send completed event failed", zap.Error(err))
    }
}
```

#### 问题

```
任务执行完成 → DB 更新 status=SUCCESS ✅ → Kafka 发送 event ❌
                                              │
                                              ▼
                                    DAG PlanService 收不到事件
                                              │
                                              ▼
                                    DAG 工作流永久卡住！
                                    子节点永远不会被触发
```

核心矛盾：**DB 更新和 Kafka 发送不在同一个事务中**。两者之间的任何故障（网络抖动、Kafka 宕机、进程崩溃）都会导致状态不一致。

#### 优化设计：Outbox Pattern

**Step 1：Outbox 表**

```sql
CREATE TABLE outbox_events (
    id          BIGINT AUTO_INCREMENT PRIMARY KEY,
    event_type  VARCHAR(64) NOT NULL,          -- 'task_completed', 'dag_step_done'
    payload     TEXT NOT NULL,                  -- JSON 序列化
    status      VARCHAR(16) NOT NULL DEFAULT 'PENDING',  -- PENDING / SENT / FAILED
    retry_count INT NOT NULL DEFAULT 0,
    created_at  BIGINT NOT NULL,
    updated_at  BIGINT NOT NULL,
    INDEX idx_status_created (status, created_at)
);
```

**Step 2：业务逻辑改造**

```go
func (s *ExecutionService) CompleteExecution(ctx context.Context, 
    exec domain.TaskExecution) error {
    
    // 在同一个 DB 事务中完成状态更新 + outbox 写入
    return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
        // 1. CAS 更新执行状态
        result := tx.Model(&dao.TaskExecution{}).
            Where("id = ? AND status = ?", exec.ID, domain.ExecStatusRunning).
            Updates(map[string]any{
                "status": domain.ExecStatusSuccess,
                "utime":  time.Now().UnixMilli(),
            })
        if result.RowsAffected == 0 {
            return ErrCASConflict
        }

        // 2. 写入 outbox（同一事务，保证原子性）
        payload, _ := json.Marshal(event.TaskCompleted{
            TaskID:      exec.TaskID,
            ExecutionID: exec.ID,
            Status:      string(domain.ExecStatusSuccess),
        })
        return tx.Create(&dao.OutboxEvent{
            EventType: "task_completed",
            Payload:   string(payload),
            Status:    "PENDING",
            CreatedAt: time.Now().UnixMilli(),
            UpdatedAt: time.Now().UnixMilli(),
        }).Error
    })
}
```

**Step 3：Outbox Poller**

```go
type OutboxPoller struct {
    db       *gorm.DB
    producer event.Producer
    interval time.Duration
    logger   *zap.Logger
}

func (p *OutboxPoller) Start(ctx context.Context) {
    ticker := time.NewTicker(p.interval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            p.pollAndSend(ctx)
        }
    }
}

func (p *OutboxPoller) pollAndSend(ctx context.Context) {
    var events []dao.OutboxEvent
    p.db.WithContext(ctx).
        Where("status = ? AND retry_count < ?", "PENDING", 5).
        Order("id ASC").
        Limit(100).
        Find(&events)

    for _, evt := range events {
        if err := p.sendEvent(ctx, evt); err != nil {
            p.db.Model(&evt).Updates(map[string]any{
                "retry_count": gorm.Expr("retry_count + 1"),
                "updated_at":  time.Now().UnixMilli(),
            })
            continue
        }
        p.db.Model(&evt).Updates(map[string]any{
            "status":     "SENT",
            "updated_at": time.Now().UnixMilli(),
        })
    }
}
```

**消费端幂等性**：outbox 保证 at-least-once，消费端需要幂等：

```go
func (s *PlanService) HandleTaskCompleted(ctx context.Context, evt event.TaskCompleted) error {
    // 幂等检查：如果后续节点已经在运行，跳过
    if s.isAlreadyTriggered(ctx, evt.ExecutionID) {
        return nil
    }
    return s.triggerNextNodes(ctx, evt)
}
```

---

### 2.2 Offset 分页一致性

#### 现状分析

`internal/compensator/sharding.go` 的补偿循环：

```go
func (c *ShardingCompensator) Start(ctx context.Context) {
    offset := 0
    for {
        parents, err := c.repo.FindShardingParents(ctx, offset, c.batchSize)
        if len(parents) == 0 {
            offset = 0  // 重头来
            continue
        }
        
        for _, p := range parents {
            c.handle(ctx, p)  // handle 内部会改变 parent 的 status
        }
        
        offset += c.batchSize  // ❌ offset 偏移了！
    }
}
```

#### 问题

```
假设 batchSize=10，当前有 25 条 RUNNING 的 parent

第一轮：SELECT ... WHERE status='RUNNING' OFFSET 0 LIMIT 10
  → 返回 record 1-10
  → handle() 将 record 3, 7, 9 更新为 SUCCESS（它们不再是 RUNNING）
  → offset += 10

数据库此时 RUNNING 记录：1,2,4,5,6,8,10,11,12,...,25（共 22 条）

第二轮：SELECT ... WHERE status='RUNNING' OFFSET 10 LIMIT 10
  → 返回 record 18-25（因为前面少了 3 条，整体左移）
  → record 11-17 被跳过！永远不会被补偿！
```

#### 优化设计：Cursor-based Pagination

```go
func (c *ShardingCompensator) Start(ctx context.Context) {
    lastID := int64(0)
    for {
        parents, err := c.repo.FindShardingParentsAfter(ctx, lastID, c.batchSize)
        if err != nil {
            c.logger.Error("find sharding parents", zap.Error(err))
            time.Sleep(time.Second)
            continue
        }
        if len(parents) == 0 {
            lastID = 0  // 从头扫描
            time.Sleep(c.interval)
            continue
        }

        for _, p := range parents {
            c.handle(ctx, p)
        }
        
        lastID = parents[len(parents)-1].ID  // ✅ 基于 ID 游标
    }
}

// Repository
func (r *ExecutionRepo) FindShardingParentsAfter(ctx context.Context, 
    lastID int64, limit int) ([]domain.TaskExecution, error) {
    
    var execs []dao.TaskExecution
    err := r.db.WithContext(ctx).
        Where("id > ? AND type = ? AND status = ?", 
            lastID, domain.ExecTypeShardingParent, domain.ExecStatusRunning).
        Order("id ASC").
        Limit(limit).
        Find(&execs).Error
    return toDomain(execs), err
}
```

**为什么 cursor-based 不受 handle() 影响？** 因为 `WHERE id > lastID` 是基于主键的绝对位置，无论前面的记录状态怎么变，下一批查询从上次最后一个 ID 之后开始，不会跳过任何记录。

---

### 2.3 Plan 变量名 Bug

#### 现状分析

`internal/service/task/plan.go` 第 64-65 行：

```go
func (s *PlanService) getPlanData(ctx context.Context, taskID int64) (*PlanTask, error) {
    // ... 其他代码
    
    eerr := s.parseDSL(plan.DSL)
    if eerr != nil {
        return err  // ❌ 应该是 return eerr
    }
    // ...
}
```

#### 问题

这是个变量名拼写错误。`err` 是上面某个操作返回的 error（可能为 nil），而 `eerr` 才是 DSL 解析的错误。结果是：DSL 解析失败时，函数可能返回 nil error，调用方以为成功了，实际上 PlanTask 数据不完整。

#### 修复

```go
eerr := s.parseDSL(plan.DSL)
if eerr != nil {
    return nil, eerr  // ✅ 返回正确的错误变量
}
```

**教训**：Go 的 `:=` 允许在同一作用域声明多个 error 变量（err, eerr, err2…），这种模式容易出错。更好的做法：

```go
// 方案一：重用 err 变量
if err = s.parseDSL(plan.DSL); err != nil {
    return nil, fmt.Errorf("parse DSL: %w", err)
}

// 方案二：用 errcheck / golangci-lint 的 nolint 检测
```

---

### 2.4 V1/V2 补偿器策略模式去重

#### 现状分析

`internal/compensator/sharding.go`（V1）和 `internal/compensator/shardingv2.go`（V2）代码高度重复：

```
sharding.go (200 行)            shardingv2.go (197 行)
├── Start()                     ├── Start()
│   ├── 分页查询 parent         │   ├── 按表分页查询 parent  ← 唯一区别
│   └── 调用 handle()           │   └── 调用 handle()
├── handle()                    ├── handle()          ← 完全相同
│   ├── 查子任务状态             │   ├── 查子任务状态
│   ├── 判断是否全完成           │   ├── 判断是否全完成
│   └── 更新 parent 状态        │   └── 更新 parent 状态
└── 其他辅助方法                └── 其他辅助方法      ← 完全相同
```

核心差异只在 **数据获取方式**：V1 从单表查，V2 需要遍历分库分表。

#### 优化设计：策略模式

```go
// 补偿逻辑接口
type CompensationLogic interface {
    // FetchBatch 获取一批待补偿的 parent execution
    FetchBatch(ctx context.Context, lastID int64, batchSize int) ([]domain.TaskExecution, error)
    // Name 用于日志标识
    Name() string
}

// V1 实现
type SingleTableCompensation struct {
    repo repository.TaskExecutionRepo
}

func (c *SingleTableCompensation) FetchBatch(ctx context.Context, 
    lastID int64, batchSize int) ([]domain.TaskExecution, error) {
    return c.repo.FindShardingParentsAfter(ctx, lastID, batchSize)
}

// V2 实现
type ShardingTableCompensation struct {
    repos []repository.TaskExecutionRepo  // 多个分表 repo
}

func (c *ShardingTableCompensation) FetchBatch(ctx context.Context, 
    lastID int64, batchSize int) ([]domain.TaskExecution, error) {
    var all []domain.TaskExecution
    for _, repo := range c.repos {
        batch, err := repo.FindShardingParentsAfter(ctx, lastID, batchSize)
        if err != nil {
            return nil, err
        }
        all = append(all, batch...)
    }
    sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
    return all, nil
}

// 统一补偿器 — 消除重复代码
type ShardingCompensator struct {
    logic    CompensationLogic
    handler  *ShardingHandler  // 提取出的公共 handle 逻辑
    interval time.Duration
    batch    int
}

func (c *ShardingCompensator) Start(ctx context.Context) {
    lastID := int64(0)
    for {
        select {
        case <-ctx.Done():
            return
        default:
        }
        parents, err := c.logic.FetchBatch(ctx, lastID, c.batch)
        if err != nil || len(parents) == 0 {
            lastID = 0
            time.Sleep(c.interval)
            continue
        }
        for _, p := range parents {
            c.handler.Handle(ctx, p)
        }
        lastID = parents[len(parents)-1].ID
    }
}
```

**效果**：~400 行重复代码（V1 200 + V2 197）→ ~200 行统一实现 + 两个小的策略实现（各 ~20 行）。

---

## 三、对比总结

| 问题 | 现状风险 | 方案 | 关键技术 |
|------|---------|------|----------|
| Kafka 事件丢失 | DAG 永久卡住 | Outbox Pattern | 同事务写入 + 异步轮询 |
| Offset 分页偏移 | 记录被跳过 | Cursor Pagination | `WHERE id > lastID` |
| Plan 变量名 Bug | 静默失败 | 修正 + lint 规则 | errcheck |
| V1/V2 代码重复 | 维护成本翻倍 | 策略模式 | interface 抽象数据获取 |

---

## 四、面试话术

### Q: DAG 工作流怎么保证步骤间的可靠传递？

> "原来的实现是任务完成后直接发 Kafka 事件通知下游节点，但 DB 更新和 Kafka 发送不在同一个事务里，网络故障时会丢事件导致 DAG 卡住。我用 Outbox Pattern 解决——状态更新和事件写入在同一个 DB 事务中，然后由独立的 Poller 异步轮询 outbox 表发送到 Kafka。这样 DB 事务成功就保证了事件不丢，Kafka 发送失败会自动重试。消费端做幂等，整体是 at-least-once 语义。"

### Q: 分页查询有什么坑？

> "补偿器用 OFFSET 分页扫描 RUNNING 状态的 parent execution，但 handle() 处理后会把部分记录改为 SUCCESS，导致结果集左移。下一页的 OFFSET 就会跳过中间的记录，永远不被补偿。改用 cursor-based 分页——`WHERE id > lastID ORDER BY id ASC`，基于主键的绝对位置翻页，不受状态变更影响。"

### Q: 项目中有代码重复的问题吗？怎么处理？

> "V1 和 V2 的分片补偿器有 ~400 行近乎一样的代码，唯一区别是数据获取方式——V1 单表查，V2 遍历分表。我用策略模式抽取了 `CompensationLogic` 接口，V1/V2 各自实现 `FetchBatch`，公共的补偿循环和 handle 逻辑统一复用。代码量减少了一半，而且新增分表策略只需实现一个 20 行的 struct。"
