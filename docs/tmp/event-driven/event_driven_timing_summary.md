# 事件驱动定时调度解决方案总结

## 🎯 核心问题

**事件驱动模式下，如何确保任务在指定时间（NextTime）被调度？**

### 问题场景

```go
// 场景 1：定时任务（每天凌晨1点执行）
task := Task{
    CronExpr: "0 0 1 * * *",
    NextTime: 1732665600000,  // 2025-11-27 01:00:00
}
// 问题：任务创建时间是 2025-11-26 10:00:00
// 如果立即发送事件并调度，会导致任务提前 15 小时执行！❌

// 场景 2：延迟任务（30分钟后执行）
task := Task{
    NextTime: time.Now().Add(30 * time.Minute).UnixMilli(),
}
// 问题：任务创建后立即调度，无法实现延迟效果！❌
```

---

## 🏗️ 解决方案：混合模式

### 架构图

```
任务创建
    ↓
检查 NextTime
    ↓
    ├─→ 立即执行（≤30秒）─→ 发送 Kafka 事件 ─→ 立即调度
    │
    └─→ 延迟执行（>30秒）─→ 存储数据库
                              ↓
                         时间轮调度器
                              ↓
                         到达时间后发送事件 ─→ 调度
                              ↑
                         轮询兜底（补偿）
```

### 核心思想

| 延迟时间 | 处理策略 | 精度 |
|---------|---------|------|
| **0-30 秒** | 立即发送事件 | 毫秒级 |
| **30 秒-1 小时** | 时间轮调度 | 秒级 |
| **1 小时以上** | 数据库 + 定期扫描 | 分钟级 |

---

## 💻 核心实现

### 1. 任务创建时的智能分发

```go
// internal/service/task/service.go
func (s *service) Create(ctx context.Context, task domain.Task) (domain.Task, error) {
    // 1. 计算下次执行时间
    nextTime, err := task.CalculateNextTime()
    task.NextTime = nextTime.UnixMilli()
    
    // 2. 创建任务
    createdTask, err := s.repo.Create(ctx, task)
    
    // 3. 智能分发
    now := time.Now().UnixMilli()
    delaySeconds := (createdTask.NextTime - now) / 1000
    
    if delaySeconds <= 30 {  // 30 秒阈值
        // 立即调度：发送事件
        go s.publishImmediateScheduleEvent(createdTask)
    } else {
        // 延迟调度：由时间轮或轮询兜底处理
        s.logger.Info("任务将延迟调度", elog.Int64("delaySeconds", delaySeconds))
    }
    
    return createdTask, nil
}
```

**关键点**：
- ✅ 30 秒内的任务立即发送事件
- ✅ 超过 30 秒的任务不发送事件，由时间轮处理
- ✅ 异步发送事件，不阻塞主流程

### 2. 时间轮调度器

```go
// internal/service/scheduler/timewheel_scheduler.go
type TimeWheelScheduler struct {
    taskSvc       task.Service
    eventProducer event.TaskEventProducer
    wheel         *TimeWheel  // 时间轮
}

func (s *TimeWheelScheduler) Start() error {
    // 1. 启动时间轮（每秒前进一格）
    s.wheel.Start(s.ctx, s.onTaskReady)
    
    // 2. 定期扫描数据库，加载延迟任务到时间轮
    go s.scanAndLoadTasks()
    
    return nil
}

// 扫描数据库，加载未来 1 小时内的任务
func (s *TimeWheelScheduler) loadDelayedTasks() {
    now := time.Now().UnixMilli()
    lookAheadTime := now + 1*time.Hour.Milliseconds()
    
    // 查询未来 1 小时内需要执行的任务
    tasks, _ := s.taskSvc.FindTasksInTimeRange(ctx, now, lookAheadTime, 1000)
    
    // 添加到时间轮
    for _, task := range tasks {
        delay := time.Duration(task.NextTime - now) * time.Millisecond
        s.wheel.AddTask(task, delay)
    }
}

// 任务到达执行时间的回调
func (s *TimeWheelScheduler) onTaskReady(task domain.Task) {
    // 发送调度事件
    event := TaskCreatedEvent{
        TaskID: task.ID,
        Task:   task,
        Source: "timewheel",
    }
    s.eventProducer.PublishTaskCreated(ctx, event)
}
```

**关键点**：
- ✅ 时间轮每秒前进一格，精度为秒级
- ✅ 每分钟扫描数据库，加载未来 1 小时的任务
- ✅ 任务到期时发送调度事件

### 3. 时间轮实现

```go
type TimeWheel struct {
    tickDuration time.Duration  // 刻度：1 秒
    wheelSize    int             // 大小：3600 格（1 小时）
    slots        []map[int64]domain.Task  // 槽位
    currentPos   int             // 当前位置
}

// 时间轮前进一格
func (tw *TimeWheel) tick() {
    tw.mu.Lock()
    
    // 获取当前槽位的任务
    tasks := tw.slots[tw.currentPos]
    
    // 清空当前槽位
    tw.slots[tw.currentPos] = make(map[int64]domain.Task)
    
    // 前进到下一个槽位
    tw.currentPos = (tw.currentPos + 1) % tw.wheelSize
    
    tw.mu.Unlock()
    
    // 触发任务回调
    for _, task := range tasks {
        go tw.taskCallback(task)
    }
}

// 添加任务到时间轮
func (tw *TimeWheel) AddTask(task domain.Task, delay time.Duration) {
    // 计算槽位
    ticks := int(delay / tw.tickDuration)
    slotIndex := (tw.currentPos + ticks) % tw.wheelSize
    
    tw.slots[slotIndex][task.ID] = task
}
```

**关键点**：
- ✅ 3600 个槽位，每个槽位代表 1 秒
- ✅ 每秒前进一格，触发当前槽位的任务
- ✅ 时间复杂度 O(1)

### 4. 事件驱动调度器增强

```go
// internal/service/scheduler/event_driven_scheduler.go
func (s *EventDrivenScheduler) handleEvent(ctx context.Context, taskEvent event.TaskCreatedEvent) error {
    // 1. 检查任务是否到达执行时间
    now := time.Now().UnixMilli()
    if taskEvent.Task.NextTime > now {
        delayMs := taskEvent.Task.NextTime - now
        
        // 如果延迟时间较短（< 1 分钟），等待后再调度
        if delayMs < 60000 {
            time.Sleep(time.Duration(delayMs) * time.Millisecond)
        } else {
            // 延迟时间过长，跳过调度
            return fmt.Errorf("任务未到执行时间")
        }
    }
    
    // 2. 调度任务
    err := s.runner.Run(ctx, taskEvent.Task)
    
    return err
}
```

**关键点**：
- ✅ 检查任务是否到达执行时间
- ✅ 短延迟（< 1 分钟）等待后调度
- ✅ 长延迟跳过，由时间轮处理

### 5. 轮询兜底机制

```go
func (s *EventDrivenScheduler) pollingFallback() {
    ticker := time.NewTicker(5 * time.Minute)
    
    for range ticker.C {
        // 查询已到执行时间但未被调度的任务
        now := time.Now().UnixMilli()
        tasks, _ := s.taskSvc.SchedulableTasks(ctx, 60000, 100)
        
        // 过滤：只处理已到执行时间的任务
        readyTasks := make([]domain.Task, 0)
        for _, t := range tasks {
            if t.NextTime <= now {
                readyTasks = append(readyTasks, t)
            }
        }
        
        // 发送补偿事件
        for _, t := range readyTasks {
            s.eventChan <- TaskCreatedEvent{Task: t, Source: "polling_fallback"}
        }
    }
}
```

**关键点**：
- ✅ 每 5 分钟扫描一次
- ✅ 只处理已到执行时间的任务
- ✅ 发送补偿事件

---

## 🔄 完整流程

### 立即任务流程

```
1. 用户创建任务（NextTime = now + 10s）
2. 任务服务计算 delaySeconds = 10s
3. delaySeconds <= 30s，立即发送 Kafka 事件
4. 事件驱动调度器消费事件
5. 检查 NextTime，等待 10s 后调度
6. 调度执行
```

### 延迟任务流程

```
1. 用户创建任务（NextTime = now + 30min）
2. 任务服务计算 delaySeconds = 1800s
3. delaySeconds > 30s，不发送事件
4. 时间轮调度器每分钟扫描数据库
5. 发现该任务，加载到时间轮
6. 30 分钟后，时间轮触发回调
7. 发送 Kafka 事件
8. 事件驱动调度器消费事件并调度
```

### 轮询兜底流程

```
1. 每 5 分钟扫描数据库
2. 查询已到执行时间但未调度的任务
3. 发现遗漏任务，记录告警指标
4. 发送补偿事件
5. 事件驱动调度器消费事件并调度
```

---

## ⚙️ 配置文件

```yaml
scheduler:
  mode: event_driven
  
  # 任务服务配置
  task:
    immediateScheduleThreshold: 30  # 立即调度阈值（秒）
  
  # 事件驱动配置
  eventDriven:
    enabled: true
    workerCount: 20
    enablePolling: true
    pollingInterval: 5m
  
  # 时间轮配置
  timeWheel:
    enabled: true
    tickDuration: 1s        # 时间轮刻度：1 秒
    wheelSize: 3600         # 时间轮大小：3600 格（1 小时）
    scanInterval: 1m        # 扫描数据库间隔：1 分钟
    scanBatchSize: 1000     # 每次扫描数量：1000
    scanLookAhead: 1h       # 提前加载时间窗口：1 小时
```

---

## 📊 性能对比

| 指标 | 纯轮询模式 | 纯事件驱动 | 混合模式（推荐） |
|------|-----------|-----------|----------------|
| **立即任务延迟** | 15 秒 | < 1 秒 | **< 1 秒** ✅ |
| **延迟任务精度** | 30 秒 | ❌ 不支持 | **1 秒** ✅ |
| **数据库 QPS** | 持续查询 | 仅创建时 | **定期扫描** ✅ |
| **内存占用** | 低 | 低 | 中 |
| **可靠性** | 高 | 中 | **高** ✅ |

---

## 🎯 关键设计

### 1. 智能分发策略

```go
if delaySeconds <= 30 {
    // 立即调度：发送事件
    publishEvent(task)
} else {
    // 延迟调度：时间轮处理
    // 不发送事件
}
```

### 2. 时间轮参数

```yaml
tickDuration: 1s      # 刻度：1 秒
wheelSize: 3600       # 大小：3600 格（1 小时）
scanInterval: 1m      # 扫描间隔：1 分钟
scanLookAhead: 1h     # 提前加载：1 小时
```

### 3. 三层保障

```
第一层：立即任务 → 事件驱动（< 1 秒）
第二层：延迟任务 → 时间轮（秒级精度）
第三层：遗漏任务 → 轮询兜底（5 分钟）
```

---

## 🔍 监控告警

### 关键指标

```yaml
# 调度延迟
scheduler_schedule_latency_seconds{source="immediate"}
scheduler_schedule_latency_seconds{source="timewheel"}
scheduler_schedule_latency_seconds{source="polling"}

# 时间轮状态
timewheel_tasks_total          # 时间轮中的任务数
timewheel_slot_utilization     # 槽位利用率

# 轮询兜底
polling_fallback_tasks_total   # 轮询发现的遗漏任务数
```

### 告警规则

```yaml
# 延迟任务调度超时
- alert: DelayedTaskScheduleLate
  expr: (time() - task_next_time_seconds) > 60
  
# 时间轮任务积压
- alert: TimeWheelBacklog
  expr: timewheel_tasks_total > 10000
  
# 轮询兜底频繁触发
- alert: PollingFallbackHigh
  expr: rate(polling_fallback_tasks_total[5m]) > 10
```

---

## 📚 总结

### 核心优势

1. ✅ **高效**：立即任务 < 1 秒调度
2. ✅ **精确**：延迟任务秒级精度
3. ✅ **可靠**：轮询兜底保证不丢任务
4. ✅ **低成本**：数据库压力小
5. ✅ **可扩展**：时间轮可水平扩展

### 实施步骤

1. 修改任务服务，增加智能分发逻辑
2. 实现时间轮调度器
3. 增强事件驱动调度器的时间检查
4. 保留轮询兜底机制
5. 完善监控和告警

### 适用场景

| 场景 | 是否适用 |
|------|---------|
| 立即执行任务 | ✅ 完美支持 |
| 短延迟任务（< 1 小时） | ✅ 完美支持 |
| 长延迟任务（> 1 小时） | ✅ 支持（分钟级精度） |
| 定时任务（Cron） | ✅ 完美支持 |
| 高并发场景 | ✅ 完美支持 |

---

**详细文档**：[event_driven_timing_solution.md](./event_driven_timing_solution.md)  
**版本**：v1.0  
**日期**：2025-11-27
