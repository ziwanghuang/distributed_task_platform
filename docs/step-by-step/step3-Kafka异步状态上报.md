# Step 3：Kafka 异步状态上报

> **目标**：引入 Kafka 消息队列，实现执行器状态的**异步上报**和**完成事件驱动**，将 Step 2 的同步 gRPC 回调升级为高可靠的异步事件系统。
>
> **完成后你能看到**：启动 MySQL + etcd + Kafka → 启动调度器（自动创建 Topic）→ 启动执行器 → 调度器通过 gRPC 下发任务 → 执行器通过 gRPC Report 实时上报进度 → 任务完成后调度器发送 Kafka 完成事件 → 消费者消费事件更新状态 → 数据库中执行记录最终为 SUCCESS。

---

## 1. 架构总览

Step 3 在 Step 2 的基础上引入 Kafka，构建了**双链路上报 + 事件驱动**的异步架构：

```
                         ┌─────────────────────────────────────────────────┐
                         │              Scheduler（调度器进程）               │
                         │                                                 │
                         │  ┌──────────────────────────────────────────┐  │
                         │  │          ExecutionService                 │  │
                         │  │    ┌─────────────────────────────┐      │  │
                         │  │    │     UpdateState（状态机）     │      │  │
                         │  │    │                             │      │  │
                         │  │    │  PREPARE ──► RUNNING ─┬───► SUCCESS│  │
                         │  │    │                       │            │  │
                         │  │    │                       ├───► FAILED │  │
                         │  │    │                       │            │  │
                         │  │    │                       └───► RETRY  │  │
                         │  │    └──────────┬──────────────────┘      │  │
                         │  │               │ 终态到达时               │  │
                         │  │               ▼                         │  │
                         │  │    ┌──────────────────┐                 │  │
                         │  │    │ CompleteProducer  │──► Kafka Topic │  │
                         │  │    │  (完成事件)        │   "complete"   │  │
                         │  │    └──────────────────┘                 │  │
                         │  └──────────────────────────────────────────┘  │
                         │                                                 │
 ┌────────────┐ gRPC     │  ReporterServer ─────────────────┐             │
 │  Executor  │──Report──┤                                   │             │
 │  (执行器)   │          │                                   ▼             │
 └────────────┘          │  ReportEvent ──► Kafka Topic ──► Consumer ───► │
               Kafka可选  │  Consumer       "report_event"   HandleReports│
                         │                                                 │
                         └─────────────────────────────────────────────────┘

                              数据流方向：
                              ──────────────────────────────────────────
                              1. Executor ──gRPC Report──► ReporterServer
                                                            │
                                                  ExecutionService.UpdateState
                                                            │
                                              ┌─────────────┼──────────────┐
                                              │ 终态         │ RUNNING      │
                                              ▼             ▼              
                                    CompleteProducer   UpdateProgress    
                                         │                              
                                    Kafka "complete"                    
                                         │                              
                                    ┌────┴─────┐                       
                                    │ Consumer  │                       
                                    │ (DAG驱动)  │                       
                                    └──────────┘                       

                              2. Executor ──Kafka Report──► ReportEventConsumer
                                                            │
                                                  ExecutionService.HandleReports
                                                    (同上，共享状态机)
```

### Step 2 vs Step 3 对比

| 维度 | Step 2 | Step 3 |
|------|--------|--------|
| **状态上报** | 仅 gRPC 同步上报 | 双链路：gRPC 同步 + Kafka 异步 |
| **完成事件** | 无事件系统 | Kafka CompleteEvent 驱动后续流程 |
| **进度更新** | Invoker.Run 同步返回状态 | ExecutionService.UpdateState 状态机 |
| **中间件** | MySQL + etcd | MySQL + etcd + **Kafka** |
| **MQ 抽象** | 无 | ecodeclub/mq-api 抽象层 |
| **消费者** | 无 | ReportEventConsumer + BatchReportEventConsumer |
| **核心新增文件** | — | 10+ 个文件 |

### 新增 / 修改文件清单

| 目录 | 文件 | 变更 | 说明 |
|------|------|------|------|
| `internal/event/` | `complete.go` | 新增 | 完成事件数据结构 |
| `internal/event/` | `complete_producer.go` | 新增 | 完成事件 Kafka 生产者 |
| `internal/event/complete/` | `consumer.go` | 新增 | 完成事件消费者（驱动 DAG） |
| `internal/event/reportevt/` | `report_event_producer.go` | 新增 | 单条上报事件 Kafka 生产者 |
| `internal/event/reportevt/` | `report_event_consumer.go` | 新增 | 单条上报事件消费者 |
| `internal/event/reportevt/` | `batch_report_event_consumer.go` | 新增 | 批量上报事件消费者 |
| `internal/domain/` | `report.go` | 新增 | Report 上报领域模型 |
| `internal/service/task/` | `execution_service.go` | **重构** | 状态机核心 + HandleReports |
| `internal/grpc/` | `server.go` | 修改 | ReporterServer 接入状态机 |
| `pkg/mqx/` | `consumer.go` | 新增 | MQ 消费者通用封装 |
| `ioc/` | `mq.go` / `mq_kafka.go` / `mq_producer.go` / `mq_consumer.go` | 新增 | MQ 初始化 + 消费者/生产者注入 |

---

## 2. 设计决策对比

### 2.1 状态上报方式：gRPC 同步 vs Kafka 异步 vs 双链路

| 方案 | 优点 | 缺点 | 适用场景 |
|------|------|------|---------|
| **纯 gRPC 同步** | 延迟低（<1ms）、实现简单、强一致 | 调度器宕机时上报丢失；高并发下调度器成为瓶颈 | 实时性要求极高、任务量小 |
| **纯 Kafka 异步** | 削峰填谷、消息持久化、消费者横向扩展 | 延迟增加（10-100ms）、依赖 Kafka 可用性 | 高吞吐、跨网络 |
| **双链路（本项目选择）** | 两条路径互补；gRPC 保实时性，Kafka 保可靠性 | 实现复杂度高、需保证两条链路共享同一状态机 | 生产级系统 |

**决策理由**：

```
双链路设计的核心洞察：gRPC Report 和 Kafka Report 的消费端最终
都汇入同一个 ExecutionService.HandleReports 方法。状态机逻辑
只有一份，两条链路只是"投递通道"不同。

gRPC 链路：Executor → ReporterServer.Report → HandleReports
Kafka 链路：Executor → Kafka → ReportEventConsumer → HandleReports
```

### 2.2 MQ 选型：Kafka vs RabbitMQ vs RocketMQ vs Pulsar

| 维度 | Kafka | RabbitMQ | RocketMQ | Pulsar |
|------|-------|----------|----------|--------|
| **吞吐量** | 极高（百万/s） | 中等（万/s） | 高（十万/s） | 极高 |
| **延迟** | ms~十ms | 微秒~ms | ms | ms |
| **消息持久化** | ✅ 日志追加 | ✅ 磁盘 | ✅ 多副本 | ✅ BookKeeper |
| **消费模型** | Pull + Consumer Group | Push + Ack | Pull/Push | Pull/Push |
| **KRaft** | ✅ 3.x 去 ZK | N/A | N/A | N/A |
| **Go 生态** | sarama/confluent | amqp | official Go SDK | official Go SDK |
| **运维复杂度** | 中（KRaft 后简化） | 低 | 中 | 高 |

**选择 Kafka 的理由**：
1. **吞吐量与任务量匹配**：大量任务执行状态上报天然是高频事件
2. **KRaft 模式**：3.9.0 已完全去掉 Zookeeper 依赖，docker-compose 单容器即可运行
3. **Consumer Group**：多调度器实例自然地通过 Consumer Group 负载均衡消费
4. **消息持久化**：即使调度器临时宕机，状态上报消息不会丢失
5. **生态成熟**：Go 的 sarama 库 + ecodeclub/mq-api 抽象层，开箱即用

### 2.3 MQ 抽象层：直接用 sarama vs ecodeclub/mq-api

| 方案 | 优点 | 缺点 |
|------|------|------|
| **直接 sarama** | 功能最全、社区大 | 与 Kafka 强绑定，换 MQ 需全量重写 |
| **ecodeclub/mq-api 抽象** | 接口统一、可替换实现（Kafka/内存/Redis Stream） | 功能受限于接口抽象（如 partition 控制） |

**选择 mq-api 的理由**：
1. **可测试性**：集成测试使用内存 MQ 实现，无需启动 Kafka 容器
2. **接口简洁**：只暴露 `MQ.Producer()`、`MQ.Consumer()`、`Producer.Produce()`、`Consumer.ConsumeChan()` 四个核心方法
3. **底层可替换**：如果未来需要迁移到 Pulsar，只需替换 `kafka.NewMQ()` 为 `pulsar.NewMQ()`

### 2.4 完成事件消费模式：事件驱动 vs 轮询聚合

| 方案 | 说明 | 优点 | 缺点 |
|------|------|------|------|
| **事件驱动（本项目）** | 任务到达终态时发 Kafka 事件，消费者驱动后续逻辑 | 实时性好、解耦 | 需要 MQ 保证不丢消息 |
| **轮询聚合** | 定时扫描执行记录，检查终态 | 实现简单 | 延迟高、扫描开销大 |

**决策理由**：事件驱动是 DAG 工作流（Step 5）的基础——当一个节点任务完成时，立即触发后继节点。轮询方式无法满足 DAG 的实时驱动需求。

---

## 3. 核心代码实现

### 3.1 Kafka 基础设施

#### docker-compose 加入 Kafka

```yaml
# docker-compose.yml（新增 Kafka 服务）
kafka:
  image: "bitnami/kafka:3.9.0"
  container_name: task-kafka
  ports:
    - "9092:9092"   # EXTERNAL: 宿主机访问
    - "9094:9094"   # INTERNAL: Docker 内部服务通信
  environment:
    - KAFKA_CFG_NODE_ID=0
    - KAFKA_CFG_AUTO_CREATE_TOPICS_ENABLE=true
    - KAFKA_CFG_PROCESS_ROLES=controller,broker          # KRaft 模式，同时是 controller 和 broker
    - KAFKA_CFG_LISTENERS=EXTERNAL://:9092,INTERNAL://:9094,CONTROLLER://:9093
    - KAFKA_CFG_ADVERTISED_LISTENERS=EXTERNAL://localhost:9092,INTERNAL://kafka:9094
    - KAFKA_CFG_INTER_BROKER_LISTENER_NAME=INTERNAL
    - KAFKA_CFG_LISTENER_SECURITY_PROTOCOL_MAP=EXTERNAL:PLAINTEXT,INTERNAL:PLAINTEXT,CONTROLLER:PLAINTEXT
    - KAFKA_CFG_CONTROLLER_QUORUM_VOTERS=0@kafka:9093
    - KAFKA_CFG_CONTROLLER_LISTENER_NAMES=CONTROLLER
  volumes:
    - kafka-data:/bitnami/kafka
  healthcheck:
    test: ["CMD-SHELL", "kafka-broker-api-versions.sh --bootstrap-server localhost:9092 || exit 1"]
    interval: 10s
    timeout: 10s
    retries: 5
    start_period: 30s
```

**关键设计点**：
- **KRaft 模式**：`PROCESS_ROLES=controller,broker` 让单节点同时承担元数据管理和消息存储，无需 Zookeeper
- **双 Listener**：EXTERNAL 供宿主机开发访问，INTERNAL 供 Docker 内部服务通信
- **healthcheck**：30s start_period 给足 Kafka 初始化时间

#### 配置文件新增

```yaml
# config/config.yaml（Step 3 新增部分）
mq:
  kafka:
    network: "tcp"
    addr: "localhost:9092"

executionReportEvent:
  topic: "execution_report"
  partitions: 1

executionBatchReportEvent:
  topic: "execution_batch_report"
  partitions: 1
```

### 3.2 MQ 初始化（IOC 层）

#### mq.go — 带指数退避重试的 MQ 连接

```go
// ioc/mq.go
package ioc

import (
    "sync"
    "time"
    "github.com/ecodeclub/ekit/retry"
    "github.com/ecodeclub/mq-api"
)

var (
    q          mq.MQ
    mqInitOnce sync.Once
)

// InitMQ 初始化 Kafka 连接。
// 使用 sync.Once 保证全局单例 + 指数退避重试（1s~10s，最多 10 次）。
// 设计理由：Kafka 容器启动通常需要 20-30s，重试保证调度器不会因为
// Kafka 启动慢而 panic。
func InitMQ() mq.MQ {
    mqInitOnce.Do(func() {
        const maxInterval = 10 * time.Second
        const maxRetries = 10
        strategy, err := retry.NewExponentialBackoffRetryStrategy(
            time.Second, maxInterval, maxRetries)
        if err != nil {
            panic(err)
        }
        for {
            q, err = initMQ()
            if err == nil {
                break
            }
            next, ok := strategy.Next()
            if !ok {
                panic("InitMQ 重试失败......")
            }
            time.Sleep(next)
        }
    })
    return q
}
```

#### mq_kafka.go — Kafka 连接 + Topic 创建

```go
// ioc/mq_kafka.go
func initMQ() (mq.MQ, error) {
    network := econf.GetString("mq.kafka.network")
    addresses := econf.GetStringSlice("mq.kafka.addr")
    queue, err := kafka.NewMQ(network, addresses)
    if err != nil {
        return nil, err
    }
    // 自动创建上报事件 Topic
    err = createTopic(queue, "executionReportEvent.topic", "executionReportEvent.partitions")
    if err != nil {
        return nil, err
    }
    err = createTopic(queue, "executionBatchReportEvent.topic", "executionBatchReportEvent.partitions")
    if err != nil {
        return nil, err
    }
    return queue, nil
}

func createTopic(queue mq.MQ, topicKey, partitionsKey string) error {
    topic := econf.GetString(topicKey)
    partitions := econf.GetInt(partitionsKey)
    return queue.CreateTopic(context.Background(), topic, partitions)
}
```

### 3.3 领域模型：Report 上报结构

```go
// internal/domain/report.go
package domain

// Report 执行器上报的状态消息。
// 这个结构同时用于 gRPC 上报（ReporterServer 转换后）和 Kafka 上报（JSON 序列化后直接作为消息体）。
type Report struct {
    ExecutionState ExecutionState `json:"executionState"`
}

type ExecutionState struct {
    ID              int64               `json:"id"`              // 执行实例ID
    TaskID          int64               `json:"taskId"`          // 任务ID
    TaskName        string              `json:"taskName"`        // 任务名称
    Status          TaskExecutionStatus `json:"status"`          // 执行状态
    RunningProgress int32               `json:"runningProgress"` // 进度 0-100
    // 执行节点请求调度节点执行重调度
    RequestReschedule bool              `json:"requestReschedule"`
    RescheduleParams  map[string]string `json:"rescheduleParams"`
    // 执行节点的 nodeID
    ExecutorNodeID string `json:"executorNodeId"`
}

// BatchReport 批量上报结构
type BatchReport struct {
    Reports []*Report `json:"reports"`
}
```

### 3.4 完成事件系统

#### Event 事件定义

```go
// internal/event/complete.go
package event

import "gitee.com/flycash/distributed_task_platform/internal/domain"

// Event 任务执行完成事件。
// 当一个 TaskExecution 到达终态（Success/Failed）时发布到 Kafka。
// 主要驱动两个消费场景：
//   1. DAG 工作流中，子任务完成后检查后继节点的前驱依赖
//   2. 分片任务中，子任务完成触发父任务状态汇总
type Event struct {
    PlanID         int64                      `json:"planId"`         // DAG 工作流 ID，0 表示非工作流
    ExecID         int64                      `json:"execId"`
    TaskID         int64                      `json:"taskId"`
    Version        int64                      `json:"version"`
    ScheduleNodeID string                     `json:"scheduleNodeId"`
    Type           domain.TaskType            `json:"type"`           // Normal/Sharding/Plan
    ExecStatus     domain.TaskExecutionStatus `json:"execStatus"`
    Name           string                     `json:"name"`           // DAG 节点名匹配用
}
```

#### CompleteProducer — 完成事件生产者

```go
// internal/event/complete_producer.go
package event

import (
    "context"
    "encoding/json"
    "github.com/ecodeclub/mq-api"
)

// CompleteProducer 完成事件生产者接口。
// 抽象为接口的理由：集成测试可以注入 mock 实现，不依赖 Kafka。
type CompleteProducer interface {
    Produce(ctx context.Context, evt Event) error
}

type completeProducer struct {
    producer mq.Producer
}

func NewCompleteProducer(producer mq.Producer) CompleteProducer {
    return &completeProducer{producer: producer}
}

func (c *completeProducer) Produce(ctx context.Context, evt Event) error {
    val, err := json.Marshal(evt)
    if err != nil {
        return err
    }
    _, err = c.producer.Produce(ctx, &mq.Message{Value: val})
    return err
}
```

#### complete.Consumer — 完成事件消费者

```go
// internal/event/complete/consumer.go
package complete

// Consumer 任务完成事件的消费者。
// 路由规则：
//   - NormalTaskType + PlanID > 0 → handlePlanTask：DAG 子任务完成，触发后继
//   - NormalTaskType + PlanID = 0 → handleTask：独立任务完成（当前空实现）
//   - PlanTaskType → handlePlan：Plan 整体完成，释放锁
type Consumer struct {
    planRunner *runner.PlanTaskRunner
    execSvc    tasksvc.ExecutionService
    taskSvc    tasksvc.Service
    acquire    acquirer.TaskAcquirer
}

func (c *Consumer) Consume(ctx context.Context, message *mq.Message) error {
    var evt event.Event
    err := json.Unmarshal(message.Value, &evt)
    if err != nil {
        return fmt.Errorf("序列化失败 %w", err)
    }
    return c.handle(ctx, evt)
}

func (c *Consumer) handle(ctx context.Context, evt event.Event) error {
    switch evt.Type {
    case domain.NormalTaskType:
        if evt.PlanID > 0 {
            // DAG 子任务完成 → 驱动后继节点
            return c.handlePlanTask(ctx, evt)
        }
        // 独立任务完成 → 预留扩展点
        return c.handleTask(ctx, evt)
    case domain.PlanTaskType:
        // DAG 整体完成 → 更新 Plan 状态 + 释放锁
        return c.handlePlan(ctx, evt)
    default:
        return errors.New("unknown event type")
    }
}

// handlePlan 处理 Plan（DAG 工作流）整体完成事件。
// 流程：判定终态 → 更新 Plan 执行记录 → 更新下次执行时间 → 释放抢占锁
func (c *Consumer) handlePlan(ctx context.Context, evt event.Event) error {
    var err error
    if evt.ExecStatus.IsSuccess() {
        err = c.execSvc.UpdateScheduleResult(ctx, evt.ExecID,
            domain.TaskExecutionStatusSuccess, 100, time.Now().UnixMilli(), nil, "")
    } else {
        err = c.execSvc.UpdateScheduleResult(ctx, evt.ExecID,
            domain.TaskExecutionStatusFailed, 0, time.Now().UnixMilli(), nil, "")
    }
    if err != nil {
        return err
    }
    _, err = c.taskSvc.UpdateNextTime(ctx, evt.TaskID)
    if err != nil {
        return err
    }
    return c.acquire.Release(ctx, evt.TaskID, evt.ScheduleNodeID)
}
```

### 3.5 上报事件系统（Report Event）

#### ReportEventProducer — 上报事件 Kafka 生产者

```go
// internal/event/reportevt/report_event_producer.go
package reportevt

// ReportEventProducer 除了 gRPC 直接上报之外，还支持通过 Kafka 异步上报。
// 适用场景：执行器与调度平台不在同一网络、高吞吐削峰、需要消息持久化保证。
type ReportEventProducer interface {
    Produce(ctx context.Context, evt domain.Report) error
}

type reportEventProducer struct {
    producer mq.Producer
}

func (c *reportEventProducer) Produce(ctx context.Context, evt domain.Report) error {
    val, err := json.Marshal(evt)
    if err != nil {
        return err
    }
    _, err = c.producer.Produce(ctx, &mq.Message{Value: val})
    return err
}
```

#### ReportEventConsumer — 单条上报事件消费者

```go
// internal/event/reportevt/report_event_consumer.go
package reportevt

type ReportEventConsumer struct {
    execSvc  task.ExecutionService
    consumer *mqx.Consumer
    logger   *elog.Component
}

func (c *ReportEventConsumer) Start(ctx context.Context) {
    if err := c.consumer.Start(ctx, c.consumeExecutionReportEvent); err != nil {
        panic(err)
    }
}

func (c *ReportEventConsumer) consumeExecutionReportEvent(
    ctx context.Context, message *mq.Message) error {
    report := &domain.Report{}
    err := json.Unmarshal(message.Value, report)
    if err != nil {
        c.logger.Error("反序列化MQ消息体失败", ...)
        return err
    }

    if !report.ExecutionState.Status.IsValid() {
        return errs.ErrInvalidTaskExecutionStatus
    }

    // 关键：与 gRPC 上报共享同一个 HandleReports 方法
    return c.execSvc.HandleReports(ctx, []*domain.Report{report})
}
```

#### BatchReportEventConsumer — 批量上报事件消费者

```go
// internal/event/reportevt/batch_report_event_consumer.go
package reportevt

type BatchReportEventConsumer struct {
    svc      task.ExecutionService
    consumer *mqx.Consumer
    logger   *elog.Component
}

func (c *BatchReportEventConsumer) consumeBatchExecutionReportEvent(
    ctx context.Context, message *mq.Message) error {
    batchReport := &domain.BatchReport{}
    err := json.Unmarshal(message.Value, batchReport)
    if err != nil {
        return err
    }

    // 校验：只要有一条非法就拒绝整个批次
    for i := range batchReport.Reports {
        if !batchReport.Reports[i].ExecutionState.Status.IsValid() {
            return errs.ErrInvalidTaskExecutionStatus
        }
    }

    return c.svc.HandleReports(ctx, batchReport.Reports)
}
```

### 3.6 MQ 消费者通用封装

```go
// pkg/mqx/consumer.go
package mqx

// Consumer 是 MQ 消费者的通用封装。
// 核心设计：
//   1. 单条消费模型（非批量拉取），保证每条消息独立处理
//   2. 双 context 退出机制：内部 ctx（Stop 调用）+ 外部 ctx（服务关闭）
//   3. 自动创建 Consumer Group（name 作为 group ID）
type Consumer struct {
    name   string             // 消费者名称 = consumer group ID
    mq     mq.MQ
    topic  string
    ctx    context.Context    // 内部 context
    cancel context.CancelFunc
    logger *elog.Component
}

func NewConsumer(name string, mq mq.MQ, topic string) *Consumer {
    ctx, cancelFunc := context.WithCancel(context.Background())
    return &Consumer{
        name:   name,
        mq:     mq,
        topic:  topic,
        ctx:    ctx,
        cancel: cancelFunc,
        logger: elog.DefaultLogger.With(elog.FieldComponent(name)),
    }
}

func (c *Consumer) Start(ctx context.Context, consumeFunc ConsumeFunc) error {
    consumer, err := c.mq.Consumer(c.topic, c.name)
    if err != nil {
        return err
    }
    ch, err := consumer.ConsumeChan(ctx)
    if err != nil {
        return err
    }
    go c.consume(ctx, ch, consumeFunc)
    return nil
}

func (c *Consumer) consume(ctx context.Context, mqChan <-chan *mq.Message,
    consumeFunc func(ctx context.Context, message *mq.Message) error) {
    for {
        select {
        case <-c.ctx.Done():   // 内部停止
            return
        case <-ctx.Done():     // 外部关闭
            return
        case message, ok := <-mqChan:
            if !ok {
                return         // channel 关闭
            }
            err := consumeFunc(ctx, message)
            if err != nil {
                c.logger.Error("消费消息失败", ...)
            }
        }
    }
}

func (c *Consumer) Stop() error {
    c.cancel()
    return nil
}
```

### 3.7 ExecutionService — 状态机核心

这是 Step 3 最核心的新增逻辑：`UpdateState` 方法实现了完整的执行状态机。

```go
// internal/service/task/execution_service.go

// HandleReports 批量处理上报。
// "尽力而为"策略：单条失败不影响其他记录的处理，最终返回聚合错误。
func (s *executionService) HandleReports(ctx context.Context, reports []*domain.Report) error {
    var err error
    for i := range reports {
        if err1 := s.UpdateState(ctx, reports[i].ExecutionState); err1 != nil {
            err = multierr.Append(err, fmt.Errorf(
                "处理失败: taskID=%d, executionID=%d: %w",
                reports[i].ExecutionState.TaskID,
                reports[i].ExecutionState.ID, err1))
        }
    }
    return err
}

// UpdateState 状态机核心方法。
//
// 状态迁移图：
//
//   ┌─────────┐  Running   ┌─────────┐  Success/Failed  ┌──────────┐
//   │ Prepare ├───────────>│ Running ├──────────────────>│ Terminal │
//   └─────────┘            └────┬────┘                   └──────────┘
//                               │ FailedRetryable
//                               v
//                       ┌───────────────┐  MaxRetry  ┌──────────┐
//                       │ WaitingRetry  ├───────────>│  Failed  │
//                       └───────────────┘            └──────────┘
//
// 约束：终止状态（Success/Failed）不允许再迁移。
func (s *executionService) UpdateState(ctx context.Context, state domain.ExecutionState) error {
    execution, err := s.FindByID(ctx, state.ID)
    if err != nil {
        return errs.ErrExecutionNotFound
    }

    // 已终止的执行记录不允许再迁移
    if execution.Status.IsTerminalStatus() {
        return errs.ErrInvalidTaskExecutionStatus
    }

    switch {
    case state.Status.IsRunning():
        if execution.Status.IsRunning() {
            return s.updateRunningProgress(ctx, state) // 仅更新进度
        }
        return s.setRunningState(ctx, state) // PREPARE → RUNNING

    case state.Status.IsFailedRetryable():
        err = s.updateRetryState(ctx, execution, state)
        if err != nil && errors.Is(err, errs.ErrExecutionMaxRetriesExceeded) {
            s.sendCompletedEvent(ctx, state, execution) // 达到上限，发完成事件
        }
        return err

    case state.Status.IsFailedRescheduled():
        if state.RequestReschedule {
            execution.MergeTaskScheduleParams(state.RescheduleParams)
        }
        return s.updateState(ctx, execution, state)

    case state.Status.IsTerminalStatus():
        // 更新 DB 状态
        if err := s.updateState(ctx, execution, state); err != nil {
            // 不阻断后续流程
        }
        // 非分片子任务：释放任务锁 + 更新下次执行时间
        isShardedTask := execution.ShardingParentID != nil && *execution.ShardingParentID > 0
        if !isShardedTask {
            s.releaseTask(ctx, execution.Task)
            s.taskSvc.UpdateNextTime(ctx, execution.Task.ID)
        }
        // 发送完成事件到 Kafka
        s.sendCompletedEvent(ctx, state, execution)
        return nil
    }
}

// sendCompletedEvent 发送完成事件到 Kafka。
// 只有终态才会发送，驱动 DAG 后继任务和独立任务的后处理。
func (s *executionService) sendCompletedEvent(ctx context.Context,
    state domain.ExecutionState, execution domain.TaskExecution) {
    if !state.Status.IsTerminalStatus() {
        return
    }
    err := s.producer.Produce(ctx, event.Event{
        PlanID: execution.Task.PlanID,
        TaskID: execution.Task.ID,
        Name:   execution.Task.Name,
        Type:   domain.NormalTaskType,
    })
    if err != nil {
        s.logger.Error("发送完成事件失败", ...)
    }
}
```

### 3.8 ReporterServer — gRPC 上报接入状态机

```go
// internal/grpc/server.go
type ReporterServer struct {
    reporterv1.UnimplementedReporterServiceServer
    execSvc task.ExecutionService
    logger  *elog.Component
}

func (s *ReporterServer) Report(ctx context.Context,
    req *reporterv1.ReportRequest) (*reporterv1.ReportResponse, error) {
    state := req.ExecutionState
    if state == nil {
        return &reporterv1.ReportResponse{}, nil
    }
    // 转换 protobuf → domain 并交给状态机处理
    err := s.handleReports(ctx, s.toDomainReports([]*reporterv1.ReportRequest{req}))
    if err != nil {
        return nil, status.Error(codes.Internal, "处理失败")
    }
    return &reporterv1.ReportResponse{}, nil
}

func (s *ReporterServer) handleReports(ctx context.Context, reports []*domain.Report) error {
    // 最终汇入 ExecutionService.HandleReports —— 与 Kafka 消费者共享同一入口
    return s.execSvc.HandleReports(ctx, reports)
}
```

### 3.9 IOC 注入层

#### 生产者注入

```go
// ioc/mq_producer.go
func InitCompleteProducer(q mq.MQ) event.CompleteProducer {
    producer, err := q.Producer("")
    if err != nil {
        panic(err)
    }
    return event.NewCompleteProducer(producer)
}
```

#### 消费者注入

```go
// ioc/mq_consumer.go
func InitExecutionReportEventConsumer(q mq.MQ, nodeID string,
    execSvc task.ExecutionService) *reportevt.ReportEventConsumer {
    topic := econf.GetString("executionReportEvent.topic")
    // Consumer Group 名 = "executionReportEvent-{nodeID}"
    // 多调度器实例天然负载均衡消费
    return reportevt.NewReportEventConsumer(
        name("executionReportEvent", nodeID), q, topic, execSvc)
}

func InitExecutionBatchReportEventConsumer(q mq.MQ, nodeID string,
    execSvc task.ExecutionService) *reportevt.BatchReportEventConsumer {
    topic := econf.GetString("executionBatchReportEvent.topic")
    return reportevt.NewBatchReportEventConsumer(
        name("executionBatchReportEvent", nodeID), q, topic, execSvc)
}

func name(eventName, nodeID string) string {
    return fmt.Sprintf("%s-%s", eventName, nodeID)
}
```

#### 长任务列表注入

```go
// ioc/task.go
func InitTasks(
    t1 *compensator.RetryCompensator,
    t2 *compensator.RescheduleCompensator,
    t3 *compensator.ShardingCompensator,
    t4 *compensator.InterruptCompensator,
    t5 *reportevt.BatchReportEventConsumer,
    t6 *reportevt.ReportEventConsumer,
) []Task {
    return []Task{t1, t2, t3, t4, t5, t6}
}
```

### 3.10 执行器侧实现（Example Executor）

执行器通过 gRPC 直接调用 ReporterServer 上报状态：

```go
// example/grpc/executor.go
func (s *Executor) runTask(ctx context.Context, eid int64, start, end int) {
    total := end
    progressUnits := start
    incTicker := time.NewTicker(100 * time.Millisecond)
    reportTicker := time.NewTicker(30 * time.Second)

    for {
        select {
        case <-ctx.Done():
            // 被中断，上报 FAILED_RESCHEDULABLE
            cur, _ := s.states.Load(eid)
            cur.Status = executorv1.ExecutionStatus_FAILED_RESCHEDULABLE
            s.client.Report(context.Background(), &reporterv1.ReportRequest{
                ExecutionState: &cur,
            })
            return

        case <-incTicker.C:
            if progressUnits < total {
                progressUnits++
                pct := int32(progressUnits * 100 / total)
                cur, _ := s.states.Load(eid)
                cur.RunningProgress = pct
                if progressUnits >= total {
                    // 任务完成，上报 SUCCESS
                    cur.Status = executorv1.ExecutionStatus_SUCCESS
                    cur.RunningProgress = 100
                    s.client.Report(context.Background(), &reporterv1.ReportRequest{
                        ExecutionState: &cur,
                    })
                    return
                }
                s.states.Store(eid, cur)
            }

        case <-reportTicker.C:
            // 定期上报进度（RUNNING 状态）
            cur, _ := s.states.Load(eid)
            s.client.Report(context.Background(), &reporterv1.ReportRequest{
                ExecutionState: &cur,
            })
        }
    }
}
```

**执行器上报时序**：
1. 每 100ms 推进进度
2. 每 30s 通过 gRPC `Report` 上报一次 RUNNING + 当前进度百分比
3. 完成时上报 SUCCESS + progress=100
4. 被中断时上报 FAILED_RESCHEDULABLE

---

## 4. 数据流全链路

### 4.1 正常执行链路

```
 Executor                   Scheduler                     Kafka            MySQL
    │                          │                             │                │
    │◄──── gRPC Execute ──────│                             │                │
    │   返回 RUNNING           │                             │                │
    │                          │ UpdateState(RUNNING)         │                │
    │                          │──────────────────────────────────────────────►│
    │                          │                             │   SetRunning   │
    │                          │                             │                │
    │──── gRPC Report ────────►│                             │                │
    │   RUNNING, progress=30   │ UpdateState(RUNNING)         │                │
    │                          │──────────────────────────────────────────────►│
    │                          │                             │ UpdateProgress │
    │                          │                             │                │
    │──── gRPC Report ────────►│                             │                │
    │   SUCCESS, progress=100  │ UpdateState(SUCCESS)         │                │
    │                          │──────────────────────────────────────────────►│
    │                          │                             │ UpdateResult   │
    │                          │                             │                │
    │                          │ sendCompletedEvent           │                │
    │                          │────────────────────────────►│                │
    │                          │                             │ CompleteEvent  │
    │                          │                             │                │
    │                          │◄───── Consume ──────────────│                │
    │                          │ (DAG: NextStep)             │                │
```

### 4.2 Kafka 异步上报链路

```
 Executor                  Kafka                    Scheduler              MySQL
    │                         │                         │                    │
    │── Kafka Produce ───────►│                         │                    │
    │  {Report: RUNNING}      │                         │                    │
    │                         │──── Consume ───────────►│                    │
    │                         │  ReportEventConsumer    │                    │
    │                         │                         │ HandleReports      │
    │                         │                         │──────────────────►│
    │                         │                         │  UpdateState      │
```

---

## 5. 现有实现不足与优化建议

### 5.1 Consumer Group 命名耦合 nodeID

**问题**：

```go
func name(eventName, nodeID string) string {
    return fmt.Sprintf("%s-%s", eventName, nodeID)
}
```

每个调度器实例使用 `eventName-nodeID` 作为 Consumer Group 名。如果 nodeID 每次重启变化（如基于进程 PID），则每次重启都会创建新的 Consumer Group，导致旧 Group 的 offset 无人消费。

**优化方案**：

```go
// 方案 A：固定 Consumer Group 名（推荐）
// 所有调度器实例共享一个 Group，Kafka 自动 rebalance
func InitExecutionReportEventConsumer(q mq.MQ, execSvc task.ExecutionService) {
    return reportevt.NewReportEventConsumer(
        "executionReportEvent-scheduler",  // 固定名，不含 nodeID
        q, topic, execSvc)
}

// 方案 B：nodeID 使用稳定标识（hostname + port）
func InitNodeID() string {
    hostname, _ := os.Hostname()
    return fmt.Sprintf("%s:%d", hostname, port)  // 重启后不变
}
```

### 5.2 MQ 消费者缺少错误重试和死信队列

**问题**：当前消费失败只是打日志，消息被"消费"但实际未处理成功。没有重试机制，也没有死信队列兜底。

```go
// 当前实现：失败只打日志
err := consumeFunc(ctx, message)
if err != nil {
    c.logger.Error("消费消息失败", ...)
    // 消息就这么丢了...
}
```

**优化方案**：

```go
// 方案 1：消费者内部重试
func (c *Consumer) consumeWithRetry(ctx context.Context, msg *mq.Message,
    consumeFunc ConsumeFunc, maxRetries int) {
    for i := 0; i <= maxRetries; i++ {
        err := consumeFunc(ctx, msg)
        if err == nil {
            return
        }
        if i == maxRetries {
            // 发送到死信 Topic
            c.sendToDeadLetter(ctx, msg, err)
            return
        }
        time.Sleep(time.Duration(1<<i) * time.Second) // 指数退避
    }
}

// 方案 2：Kafka 原生重试 Topic（更推荐）
// 消费失败 → 发送到 {topic}-retry-1 → 延迟后重试
// 再失败 → {topic}-retry-2 → 延迟更长 → 再失败 → {topic}-dlq
```

### 5.3 CompleteProducer 发送失败无补偿

**问题**：`sendCompletedEvent` 中 Kafka 发送失败仅打日志，完成事件丢失将导致 DAG 后继节点永远不会被触发。

```go
func (s *executionService) sendCompletedEvent(...) {
    err := s.producer.Produce(ctx, event.Event{...})
    if err != nil {
        s.logger.Error("发送完成事件失败", ...) // 仅日志！
    }
}
```

**优化方案**：

```go
// 方案 1：Outbox Pattern（事务发件箱）
// 在同一个 DB 事务中：更新执行状态 + 写入 outbox 表
// 后台 goroutine 扫描 outbox 表发送到 Kafka
func (s *executionService) UpdateState(ctx context.Context, state ...) error {
    return s.repo.Transaction(ctx, func(tx *gorm.DB) error {
        // 1. 更新执行状态
        err := s.repo.UpdateScheduleResult(tx, ...)
        if err != nil {
            return err
        }
        // 2. 写入 outbox（同一事务）
        return s.repo.InsertOutbox(tx, event.Event{...})
    })
}

// 方案 2：异步重试 + 幂等消费
func (s *executionService) sendCompletedEvent(...) {
    for i := 0; i < 3; i++ {
        err := s.producer.Produce(ctx, evt)
        if err == nil {
            return
        }
        time.Sleep(time.Duration(1<<i) * 100 * time.Millisecond)
    }
    // 兜底：写入本地文件/DB，由补偿任务后续处理
    s.logger.Error("完成事件发送彻底失败，写入兜底存储", ...)
}
```

### 5.4 BatchReport 一条非法拒绝整批次

**问题**：批量上报中只要有一条状态非法，整个批次被拒绝，导致其他合法的上报也被丢弃。

```go
for i := range batchReport.Reports {
    if !batchReport.Reports[i].ExecutionState.Status.IsValid() {
        return errs.ErrInvalidTaskExecutionStatus  // 整批拒绝
    }
}
```

**优化方案**：

```go
// 过滤非法记录，处理合法部分
validReports := make([]*domain.Report, 0, len(batchReport.Reports))
for i := range batchReport.Reports {
    if batchReport.Reports[i].ExecutionState.Status.IsValid() {
        validReports = append(validReports, batchReport.Reports[i])
    } else {
        c.logger.Warn("跳过非法状态上报",
            elog.Any("report", batchReport.Reports[i]))
    }
}
if len(validReports) > 0 {
    return c.svc.HandleReports(ctx, validReports)
}
return nil
```

### 5.5 消费者成功也打 Info 日志

**问题**：每条消费成功都打 Info 日志，在高吞吐场景下会产生大量日志，影响性能。

```go
c.logger.Info("消费消息成功",
    elog.String("消息体", string(message.Value)))  // 每条都打！
```

**优化方案**：

```go
// 成功日志降级为 Debug
c.logger.Debug("消费消息成功",
    elog.String("消息体", string(message.Value)))

// 或者：使用采样日志（每 100 条打一次）
atomic.AddInt64(&c.processedCount, 1)
if c.processedCount%100 == 0 {
    c.logger.Info("消费进度", elog.Int64("processed", c.processedCount))
}
```

### 5.6 Kafka 分区数固定为 1

**问题**：`partitions: 1` 限制了消费并行度。多个调度器实例只能有一个实际消费。

**优化方案**：

```yaml
# 分区数 = 调度器实例数（或更多）
executionReportEvent:
  topic: "execution_report"
  partitions: 4           # 支持最多 4 个实例并行消费

executionBatchReportEvent:
  topic: "execution_batch_report"
  partitions: 4
```

配合 Partition Key 策略：

```go
// 按 TaskID 路由到固定分区，保证同一任务的上报顺序
func (c *reportEventProducer) Produce(ctx context.Context, evt domain.Report) error {
    val, _ := json.Marshal(evt)
    _, err = c.producer.Produce(ctx, &mq.Message{
        Key:   []byte(strconv.FormatInt(evt.ExecutionState.TaskID, 10)),
        Value: val,
    })
    return err
}
```

### 5.7 缺少 Topic 存在性检查

**问题**：`createTopic` 每次启动都尝试创建 Topic，如果 Topic 已存在会返回错误（取决于 Kafka 配置）。

**优化方案**：

```go
func createTopicIfNotExists(queue mq.MQ, topicKey, partitionsKey string) error {
    topic := econf.GetString(topicKey)
    partitions := econf.GetInt(partitionsKey)
    err := queue.CreateTopic(context.Background(), topic, partitions)
    if err != nil {
        // Kafka 返回 "topic already exists" 不算错误
        if strings.Contains(err.Error(), "already exists") {
            log.Printf("Topic %s already exists, skip creation", topic)
            return nil
        }
        return err
    }
    return nil
}
```

---

## 6. 部署验证

### 6.1 启动中间件

```bash
# 启动 MySQL + etcd + Kafka
docker compose up -d mysql etcd kafka

# 等待 Kafka 就绪（约 30s）
docker compose logs -f kafka 2>&1 | grep -m1 "started"

# 验证 Kafka 就绪
docker compose exec kafka kafka-broker-api-versions.sh \
  --bootstrap-server localhost:9092
```

### 6.2 启动调度器

```bash
# 启动调度器（会自动创建 Kafka Topic）
make run_scheduler_only

# 观察日志，确认 Kafka Topic 创建成功：
# initMQ: Topic = "execution_report", Partitions = 1
# initMQ: Topic = "execution_batch_report", Partitions = 1
```

### 6.3 启动执行器

```bash
# 终端 2：启动长任务执行器
cd example/longrunning && go run main.go
```

### 6.4 触发任务执行

```bash
# 终端 3：创建测试任务
cd example/longrunning && go test -run TestStart -v
```

### 6.5 验证完整链路

```bash
# 观察调度器日志：
# [INFO] 收到执行状态上报请求 executionId=xxx status=RUNNING
# [INFO] 收到执行状态上报请求 executionId=xxx status=SUCCESS
# [INFO] 执行状态上报处理完成 total=1 processed=1

# 验证 Kafka 消息（完成事件）：
docker compose exec kafka kafka-console-consumer.sh \
  --bootstrap-server localhost:9092 \
  --topic execution_report --from-beginning --timeout-ms 5000

# 期望看到 JSON 格式的上报事件

# 验证数据库状态：
mysql -h 127.0.0.1 -P 13316 -u root -proot task -e \
  "SELECT id, status, running_progress FROM execution ORDER BY id DESC LIMIT 5;"

# 期望看到 status=SUCCESS, running_progress=100
```

### 6.6 验证 Kafka 异步上报（可选）

如果有执行器实现了 Kafka 上报（而非 gRPC），可以手动往 Topic 推送消息验证：

```bash
# 手动发送一条上报消息到 Kafka
echo '{"executionState":{"id":1,"taskId":1,"taskName":"test","status":"RUNNING","runningProgress":50}}' | \
docker compose exec -T kafka kafka-console-producer.sh \
  --bootstrap-server localhost:9092 \
  --topic execution_report

# 观察调度器日志：
# [INFO] 消费消息成功 ...
```

---

## 7. 验收标准

- [x] 执行器执行完成后，调度器端 execution 状态变为 SUCCESS
- [x] gRPC Report 上报能正确更新数据库中的进度和状态
- [x] Kafka Topic 在调度器启动时自动创建
- [x] 调度器日志中可以看到完成事件发送成功
- [x] 执行过程中进度实时更新（数据库 progress 字段变化）
- [x] 状态机约束生效：终止状态的记录不允许再迁移
- [x] 多条上报中单条失败不影响其他记录的处理

---

## 8. 面试话术

### Q1：为什么同时保留 gRPC 上报和 Kafka 异步上报两条链路？

> "这是双链路互补设计。gRPC 上报延迟低，适合实时进度反馈——执行器每 30 秒通过 gRPC Report 上报一次 RUNNING 状态和当前进度百分比，调度器侧实时更新数据库。Kafka 异步上报适合高吞吐场景和跨网络环境，消息有持久化保证。
>
> 关键点是两条链路共享同一个 `ExecutionService.HandleReports` 入口，状态机逻辑只有一份代码。这样不管上报从哪条链路来，状态流转的正确性和一致性都有保证。"

### Q2：完成事件为什么用 Kafka 而不是直接函数调用？

> "因为完成事件是后续 DAG 工作流（Step 5）的触发点。如果直接函数调用，UpdateState 方法就会和 DAG 逻辑强耦合。用 Kafka 做事件驱动，UpdateState 只负责发送事件，后续谁消费、做什么，由消费者自己决定。
>
> 而且 Kafka 的 Consumer Group 机制天然支持多调度器实例消费——不需要自己做分布式锁或者选主。再加上消息持久化，即使调度器临时宕机，恢复后也能继续消费未处理的完成事件。"

### Q3：HandleReports 为什么用"尽力而为"策略，而不是全部成功或全部失败？

> "批量上报中，每条记录的状态迁移是独立的——A 任务从 RUNNING 变 SUCCESS 和 B 任务从 RUNNING 变 FAILED 之间没有关联。如果因为 B 失败而回滚 A 的处理，A 的状态更新就被延迟了。
>
> 所以我用 `go.uber.org/multierr` 收集所有错误，单条失败不影响其他记录。失败的会在日志中记录，下一次上报（执行器会定期重报）或者补偿器扫描时自然会处理。"

### Q4：MQ 抽象层 ecodeclub/mq-api 带来了什么好处？

> "主要是可测试性和可替换性。集成测试里我用的是内存 MQ 实现，测试跑起来不需要启动 Kafka 容器，几秒钟就完成。生产环境用 `kafka.NewMQ()` 切换到真实 Kafka。
>
> 接口就四个方法：`Producer()`, `Consumer()`, `Produce()`, `ConsumeChan()`，足够覆盖消息队列的核心操作。如果未来要换成 Pulsar 或 Redis Stream，只需要换一行初始化代码。"

### Q5：状态机设计中最容易出问题的是什么？

> "并发状态迁移。比如执行器刚上报了 SUCCESS，但同时补偿器判断任务超时发了 INTERRUPT。如果没有保护，任务可能从 SUCCESS 被错误地改回 FAILED。
>
> 我的实现里有两层保护：第一，`UpdateState` 入口检查 `IsTerminalStatus()`，已到达终态的记录直接拒绝迁移；第二，数据库层面用乐观锁（version 字段），确保并发更新不会覆盖已完成的状态。"

---

## 9. 小结

Step 3 的核心贡献是引入了**消息驱动的异步架构**，将原来同步紧耦合的状态更新变成了异步事件驱动：

1. **双链路上报**：gRPC 保实时，Kafka 保可靠，两条链路共享一个状态机
2. **完成事件**：`CompleteProducer` + `complete.Consumer` 为 Step 5 的 DAG 工作流铺路
3. **MQ 抽象**：`ecodeclub/mq-api` + `pkg/mqx/Consumer` 提供可测试、可替换的 MQ 基础设施
4. **状态机**：`ExecutionService.UpdateState` 是整个系统状态流转的单一入口，7 种状态、5 条迁移路径、2 层并发保护

下一步（Step 4：补偿机制）将在这个事件系统之上，构建重试、重调度、超时中断三大补偿器。
