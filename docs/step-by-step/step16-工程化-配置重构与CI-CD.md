# Step 16：工程化 — 配置重构、代码去重、健康检查与 CI/CD

> **目标**：提升项目的工程化水平——配置从纳秒天文数字变成人类可读的 `"10s"`、V1/V2 补偿器代码去重消除 `//nolint:dupl`、Docker Compose 全服务健康检查、GitHub Actions CI/CD 流水线、关键模块单元测试全覆盖。
>
> **完成后你能看到**：`config.yaml` 里所有 Duration 都是 `"10s"` 格式 → 补偿器代码量减少 40% 且无 dupl 告警 → `docker compose up -d` 后所有 7 个服务都是 `healthy` → `git push` 自动触发 lint + test + build → codecov 报告覆盖率 > 60%。

---

## 1. 架构总览

Step 16 是横切面的工程化改进，不引入新的业务功能，而是对 Step 1–9 积累的技术债进行系统性治理。

```
┌────────────────────────────────────────────────────────────────────────────┐
│                        Step 16 工程化改进全景                               │
│                                                                            │
│  ┌──────────────┐  ┌──────────────────┐  ┌─────────────────────────────┐  │
│  │ 16.1 配置重构 │  │ 16.2 代码去重     │  │ 16.3 健康检查完善            │  │
│  │              │  │                  │  │                             │  │
│  │ Duration:    │  │ 策略模式:         │  │ etcd + redis:               │  │
│  │ 10000000000  │  │ CompensationLogic│  │ healthcheck + service_healthy│  │
│  │   → "10s"   │  │ V1: for-loop     │  │                             │  │
│  │              │  │ V2: ShardingLoop │  │                             │  │
│  └──────────────┘  └──────────────────┘  └─────────────────────────────┘  │
│                                                                            │
│  ┌──────────────────────────────┐  ┌──────────────────────────────────┐   │
│  │ 16.4 CI/CD Pipeline          │  │ 16.5 测试覆盖增强                 │   │
│  │                              │  │                                  │   │
│  │ GitHub Actions:              │  │ 优先补充:                         │   │
│  │ lint → unit → integration    │  │ 状态机 / 重试策略 / DAG 构建     │   │
│  │ → build → codecov            │  │ CompositeChecker / ShardingRule  │   │
│  └──────────────────────────────┘  └──────────────────────────────────┘   │
└────────────────────────────────────────────────────────────────────────────┘
```

---

## 2. Step 9 → Step 16 变更对比

| 维度 | Step 9（可观测性 + Docker 部署） | Step 16（工程化改进） |
|------|------|------|
| **关注点** | 部署 + 可观测性 | 代码质量 + 自动化 |
| **配置格式** | Duration 写纳秒（`10000000000`） | 人类可读（`"10s"`） |
| **补偿器代码** | V1/V2 各写一遍，`//nolint:dupl` | 策略模式提取，公共逻辑只写一份 |
| **健康检查** | etcd/redis 无 healthcheck | 全服务 healthcheck + `service_healthy` |
| **CI/CD** | 无 | GitHub Actions 完整流水线 |
| **测试** | 仅 DAO 集成测试 + E2E | 补充 domain/service 层单元测试 |
| **Lint** | 本地 `make lint` | CI 自动 lint + staticcheck SA4006 |

---

## 3. 配置值可读性差 → 自定义 Duration 类型

### 3.1 现状分析

Go 的 `time.Duration` 内部表示为 `int64` 纳秒值。当 YAML 配置文件直接存储纳秒值时，人类几乎无法阅读：

```yaml
# ❌ 现状：config/config.yaml
scheduler:
  batchTimeout: 3000000000        # 3s? 30s? 需要数零
  preemptedTimeout: 600000000000  # 是 10 分钟？还是 1 小时？
  scheduleInterval: 10000000000   # 10s

loadChecker:
  limiter:
    waitDuration: 5000000000      # 5s
  database:
    threshold: 100000000          # 100ms
    timeWindow: 300000000000      # 5min
    backoffDuration: 10000000000  # 10s
  cluster:
    timeWindow: 300000000000      # 5min
    minBackoffDuration: 5000000000 # 5s

compensator:
  retry:
    minDuration: 1000000000       # 1s
```

**问题**：
1. **可读性极差**：`600000000000` 需要数零才能知道是 10 分钟
2. **易出错**：多一个零或少一个零就是 10 倍差异
3. **Code Review 困难**：审查者无法直观判断配置值是否合理
4. **注释维护成本**：每个纳秒值旁边都需要注释说明真实含义

### 3.2 方案设计：自定义 Duration 类型

核心思路：定义一个包装类型，实现 `yaml.Unmarshaler` 接口，支持 `"10s"`、`"200ms"`、`"5m"` 等人类可读格式。

```go
// pkg/config/duration.go
package config

import (
    "time"

    "gopkg.in/yaml.v3"
)

// Duration 是 time.Duration 的包装类型，支持 YAML 中使用人类可读格式。
// 支持的格式："300ms"、"1.5s"、"2m30s"、"1h"、"10s" 等所有 time.ParseDuration 支持的格式。
type Duration struct {
    time.Duration
}

// UnmarshalYAML 实现 yaml.Unmarshaler 接口。
// 支持两种格式：
//   - 字符串格式："10s"、"200ms"、"5m"（推荐）
//   - 数字格式：10000000000（向后兼容，单位为纳秒）
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
    // 尝试字符串格式（推荐）
    if value.Kind == yaml.ScalarNode {
        duration, err := time.ParseDuration(value.Value)
        if err == nil {
            d.Duration = duration
            return nil
        }
    }
    
    // 兼容旧格式：纯数字（纳秒）
    var nanos int64
    if err := value.Decode(&nanos); err != nil {
        return fmt.Errorf("invalid duration format %q: use \"10s\" or nanoseconds", value.Value)
    }
    d.Duration = time.Duration(nanos)
    return nil
}

// MarshalYAML 实现 yaml.Marshaler 接口，输出人类可读格式。
func (d Duration) MarshalYAML() (interface{}, error) {
    return d.Duration.String(), nil
}
```

**关键设计决策**：
- **向后兼容**：同时支持字符串格式和纯数字格式，允许逐步迁移
- **零依赖**：直接利用 Go 标准库 `time.ParseDuration`，支持所有标准格式
- **序列化友好**：`MarshalYAML` 输出 `"10s"` 而非纳秒，保证配置文件可读性

### 3.3 配置结构体改造

```go
// 改造前
type SchedulerConfig struct {
    BatchTimeout     time.Duration `yaml:"batchTimeout"`
    ScheduleInterval time.Duration `yaml:"scheduleInterval"`
    RenewInterval    time.Duration `yaml:"renewInterval"`
    PreemptedTimeout time.Duration `yaml:"preemptedTimeout"`
}

// 改造后
type SchedulerConfig struct {
    BatchTimeout     config.Duration `yaml:"batchTimeout"`
    ScheduleInterval config.Duration `yaml:"scheduleInterval"`
    RenewInterval    config.Duration `yaml:"renewInterval"`
    PreemptedTimeout config.Duration `yaml:"preemptedTimeout"`
}

// 使用时通过 .Duration 字段访问底层 time.Duration
func (s *Scheduler) Start() {
    ticker := time.NewTicker(s.config.ScheduleInterval.Duration)
    // ...
}
```

### 3.4 配置文件变更前后对比

```yaml
# ✅ 改造后：config/config.yaml
scheduler:
  batchTimeout: "3s"
  batchSize: 100
  preemptedTimeout: "10m"
  scheduleInterval: "10s"
  renewInterval: "5s"
  maxConcurrentTasks: 1000
  tokenAcquireTimeout: "3s"
  pollInterval: "1s"
  reportTimeout: "10s"

intelligentScheduling:
  jobName: "executors"
  topNCandidates: 3
  timeWindow: "30s"
  queryTimeout: "5s"

loadChecker:
  limiter:
    rateLimit: 10.0
    burstSize: 20
    waitDuration: "5s"
  database:
    threshold: "100ms"
    timeWindow: "5m"
    backoffDuration: "10s"
  cluster:
    thresholdRatio: 1.2
    timeWindow: "5m"
    slowdownMultiplier: 2.0
    minBackoffDuration: "5s"

compensator:
  retry:
    maxRetryCount: 3
    prepareTimeoutWindowMs: "10s"
    batchSize: 100
    minDuration: "1s"
  reschedule:
    batchSize: 100
    minDuration: "1s"
  sharding:
    batchSize: 100
    minDuration: "1s"
  interrupt:
    batchSize: 100
    minDuration: "1s"

cache:
  local:
    capacity: 1000000
  requestTimeout: "3s"
  valueExpiration: "10m"
```

**改造效果**：
- `600000000000` → `"10m"`：从数 12 位数字变成 4 个字符
- `100000000` → `"100ms"`：直观知道是 100 毫秒
- 消除了所有 `# Xs` 注释的维护负担
- 向后兼容：旧配置文件的纳秒值仍可正常解析

---

## 4. V1/V2 代码重复 → 策略模式

### 4.1 现状分析

项目有 4 个补偿器，每个都有 V1 和 V2 两个版本，共 8 个文件：

```
internal/compensator/
├── interrupt.go        ← V1（for-loop）
├── interruptv2.go      ← V2（ShardingLoopJob）
├── retry.go            ← V1
├── retryv2.go          ← V2
├── reschedule.go       ← V1
├── reschedulev2.go     ← V2
├── sharding.go         ← V1
└── shardingv2.go       ← V2
```

以 InterruptCompensator 为例，V1 和 V2 的 `interruptTimeoutTasks` 方法**逐行相同**：

```go
// V1: interrupt.go:88 （标注 //nolint:dupl）
func (t *InterruptCompensator) interruptTimeoutTasks(ctx context.Context) error {
    executions, err := t.execSvc.FindTimeoutExecutions(ctx, t.config.BatchSize)
    if err != nil { return fmt.Errorf("查找可中断任务失败: %w", err) }
    if len(executions) == 0 { return nil }
    for i := range executions {
        err = t.interruptTaskExecution(ctx, executions[i])
        // ... 日志
    }
    return nil
}

// V2: interruptv2.go:101 （标注 //nolint:dupl）
func (t *InterruptCompensatorV2) interruptTimeoutTasks(ctx context.Context) error {
    executions, err := t.execSvc.FindTimeoutExecutions(ctx, t.config.BatchSize)
    if err != nil { return fmt.Errorf("查找可中断任务失败: %w", err) }
    if len(executions) == 0 { return nil }
    for i := range executions {
        err = t.interruptTaskExecution(ctx, executions[i])
        // ... 日志（完全相同）
    }
    return nil
}
```

同样的模式在 Retry、Reschedule、Sharding 中重复。**V1 和 V2 的唯一差异是循环驱动框架**：
- **V1**：`for { select ... default: scanAndHandle(); sleep(minDuration) }`
- **V2**：`ShardingLoopJob.Run(ctx)` → 内部获取分布式锁 + 按分片遍历 + 调用 `scanAndHandle()`

### 4.2 重复代码统计

| 补偿器 | 重复方法 | 重复行数 |
|--------|---------|---------|
| Interrupt | `interruptTimeoutTasks` + `interruptTaskExecution` | ~50 行 |
| Retry | `retry` | ~30 行 |
| Reschedule | `reschedule` | ~30 行 |
| Sharding | `handle` + `releaseTask` | ~70 行 |
| **合计** | | **~180 行重复代码** |

### 4.3 方案设计：提取 CompensationLogic 接口

```go
// internal/compensator/types.go

// CompensationLogic 补偿逻辑接口。
// 将"查找候选记录"和"处理单条记录"抽象出来，
// 让 V1 和 V2 共享同一份业务逻辑实现。
type CompensationLogic interface {
    // FindCandidates 查找待补偿的执行记录。
    // ctx 中可能包含分片信息（V2 场景下由 ShardingLoopJob 注入）。
    FindCandidates(ctx context.Context, batchSize int) ([]domain.TaskExecution, error)
    
    // Handle 处理单条执行记录的补偿逻辑。
    Handle(ctx context.Context, exec domain.TaskExecution) error
    
    // Name 返回补偿器名称，用于日志标识。
    Name() string
}
```

### 4.4 公共补偿执行器

```go
// internal/compensator/executor.go

// CompensationExecutor 补偿逻辑执行器。
// 封装"查找 + 遍历处理"的通用逻辑，V1 和 V2 共用。
type CompensationExecutor struct {
    logic     CompensationLogic
    batchSize int
    logger    *elog.Component
}

func NewCompensationExecutor(logic CompensationLogic, batchSize int) *CompensationExecutor {
    return &CompensationExecutor{
        logic:     logic,
        batchSize: batchSize,
        logger:    elog.DefaultLogger.With(elog.FieldComponentName("compensator." + logic.Name())),
    }
}

// Execute 执行一轮补偿扫描。
// 查找候选记录 → 逐条处理 → 单条失败不影响后续。
func (e *CompensationExecutor) Execute(ctx context.Context) error {
    executions, err := e.logic.FindCandidates(ctx, e.batchSize)
    if err != nil {
        return fmt.Errorf("[%s] 查找候选记录失败: %w", e.logic.Name(), err)
    }

    if len(executions) == 0 {
        e.logger.Info("没有找到待补偿记录")
        return nil
    }

    e.logger.Info("找到待补偿记录", elog.Int("count", len(executions)))

    for i := range executions {
        if handleErr := e.logic.Handle(ctx, executions[i]); handleErr != nil {
            e.logger.Error("补偿处理失败",
                elog.Int64("executionId", executions[i].ID),
                elog.String("taskName", executions[i].Task.Name),
                elog.FieldErr(handleErr))
            continue
        }
        e.logger.Info("补偿处理成功",
            elog.Int64("executionId", executions[i].ID),
            elog.String("taskName", executions[i].Task.Name))
    }
    return nil
}
```

### 4.5 具体补偿逻辑实现（以 Interrupt 为例）

```go
// internal/compensator/interrupt_logic.go

// InterruptLogic 中断补偿的具体业务逻辑。
// 实现 CompensationLogic 接口，V1 和 V2 共用此实现。
type InterruptLogic struct {
    execSvc     task.ExecutionService
    grpcClients *grpc.ClientsV2[executorv1.ExecutorServiceClient]
}

func NewInterruptLogic(
    execSvc task.ExecutionService,
    grpcClients *grpc.ClientsV2[executorv1.ExecutorServiceClient],
) *InterruptLogic {
    return &InterruptLogic{
        execSvc:     execSvc,
        grpcClients: grpcClients,
    }
}

func (l *InterruptLogic) Name() string { return "interrupt" }

func (l *InterruptLogic) FindCandidates(ctx context.Context, batchSize int) ([]domain.TaskExecution, error) {
    return l.execSvc.FindTimeoutExecutions(ctx, batchSize)
}

func (l *InterruptLogic) Handle(ctx context.Context, execution domain.TaskExecution) error {
    if execution.Task.GrpcConfig == nil {
        return fmt.Errorf("未找到GRPC配置，无法执行中断任务")
    }
    client := l.grpcClients.Get(execution.Task.GrpcConfig.ServiceName)
    resp, err := client.Interrupt(ctx, &executorv1.InterruptRequest{
        Eid: execution.ID,
    })
    if err != nil {
        return fmt.Errorf("发送中断请求失败：%w", err)
    }
    if !resp.GetSuccess() {
        return errs.ErrInterruptTaskExecutionFailed
    }
    return l.execSvc.UpdateState(ctx, domain.ExecutionStateFromProto(resp.GetExecutionState()))
}
```

### 4.6 V1/V2 补偿器简化

```go
// internal/compensator/interrupt.go（重构后）

// InterruptCompensator V1 版中断补偿器。
// 使用简单 for-loop + sleep 驱动 CompensationExecutor。
type InterruptCompensator struct {
    executor *CompensationExecutor
    config   InterruptConfig
    logger   *elog.Component
}

func NewInterruptCompensator(
    grpcClients *grpc.ClientsV2[executorv1.ExecutorServiceClient],
    execSvc task.ExecutionService,
    config InterruptConfig,
) *InterruptCompensator {
    logic := NewInterruptLogic(execSvc, grpcClients)
    return &InterruptCompensator{
        executor: NewCompensationExecutor(logic, config.BatchSize),
        config:   config,
        logger:   elog.DefaultLogger.With(elog.FieldComponentName("compensator.interrupt")),
    }
}

func (t *InterruptCompensator) Start(ctx context.Context) {
    t.logger.Info("中断补偿器启动")
    for {
        select {
        case <-ctx.Done():
            t.logger.Info("中断补偿器停止")
            return
        default:
            startTime := time.Now()
            if err := t.executor.Execute(ctx); err != nil {
                t.logger.Error("补偿执行失败", elog.FieldErr(err))
            }
            elapsed := time.Since(startTime)
            if elapsed < t.config.MinDuration {
                select {
                case <-ctx.Done():
                    return
                case <-time.After(t.config.MinDuration - elapsed):
                }
            }
        }
    }
}
```

```go
// internal/compensator/interruptv2.go（重构后）

// InterruptCompensatorV2 V2 版中断补偿器。
// 使用 ShardingLoopJob 驱动 CompensationExecutor。
type InterruptCompensatorV2 struct {
    executor     *CompensationExecutor
    dlockClient  dlock.Client
    sem          loopjob.ResourceSemaphore
    executionStr sharding.ShardingStrategy
}

func NewInterruptCompensatorV2(
    grpcClients *grpc.ClientsV2[executorv1.ExecutorServiceClient],
    execSvc task.ExecutionService,
    config InterruptConfig,
    dlockClient dlock.Client,
    sem loopjob.ResourceSemaphore,
    executionStr sharding.ShardingStrategy,
) *InterruptCompensatorV2 {
    logic := NewInterruptLogic(execSvc, grpcClients)
    return &InterruptCompensatorV2{
        executor:     NewCompensationExecutor(logic, config.BatchSize),
        dlockClient:  dlockClient,
        sem:          sem,
        executionStr: executionStr,
    }
}

func (t *InterruptCompensatorV2) Start(ctx context.Context) {
    const interruptKey = "interruptKey"
    loopjob.NewShardingLoopJob(
        t.dlockClient, interruptKey, t.executor.Execute, t.executionStr, t.sem,
    ).Run(ctx)
}
```

### 4.7 重构后的文件结构

```
internal/compensator/
├── types.go              ← CompensationLogic 接口定义
├── executor.go           ← CompensationExecutor 通用执行器
│
├── interrupt_logic.go    ← 中断补偿业务逻辑（V1/V2 共用）
├── interrupt.go          ← V1 驱动（for-loop）
├── interruptv2.go        ← V2 驱动（ShardingLoopJob）
│
├── retry_logic.go        ← 重试补偿业务逻辑
├── retry.go              ← V1
├── retryv2.go            ← V2
│
├── reschedule_logic.go   ← 重调度补偿业务逻辑
├── reschedule.go         ← V1
├── reschedulev2.go       ← V2
│
├── sharding_logic.go     ← 分片汇总补偿业务逻辑
├── sharding.go           ← V1
└── shardingv2.go         ← V2
```

### 4.8 重构效果

| 指标 | 重构前 | 重构后 |
|------|--------|--------|
| 重复代码行数 | ~180 行 | 0 行 |
| `//nolint:dupl` 标注数 | 6 处 | 0 处 |
| 补偿逻辑修改点 | 修改 V1 后必须同步修改 V2 | 只改一个 `*Logic` 文件 |
| 新增补偿器的成本 | 写完整的 V1 + V2 | 实现 `CompensationLogic` 接口，驱动框架复用 |
| 文件数 | 8 | 14（增加了接口定义和逻辑实现文件） |
| 总代码量 | ~900 行 | ~650 行（减少 ~28%） |

---

## 5. Docker Compose 健康检查不完整

### 5.1 现状分析

当前 `docker-compose.yml` 中：

| 服务 | healthcheck | depends_on 条件 | 问题 |
|------|-------------|----------------|------|
| mysql | ✅ `mysqladmin ping` | — | 正常 |
| kafka | ✅ `kafka-broker-api-versions.sh` | — | 正常 |
| redis | ❌ 无 | `service_started` | Redis 可能还在加载 RDB |
| etcd | ❌ 无 | `service_started` | etcd 可能还在选举 |
| prometheus | ❌ 无 | — | — |
| grafana | ❌ 无 | — | — |
| scheduler | Dockerfile HEALTHCHECK | `mysql: healthy`, **redis: started**, kafka: healthy, **etcd: started** | redis/etcd 用 started 不够安全 |

**问题**：
1. **redis** 启动后可能还在加载持久化数据（大 RDB 文件加载可能需要数秒），此时连接会被拒绝
2. **etcd** 单节点模式下启动很快，但多节点集群需要选主完成后才能处理请求
3. scheduler 依赖 redis/etcd 时用 `service_started`，如果这两个服务还未就绪，scheduler 的连接会走代码级重试，产生无谓的错误日志

### 5.2 完整 docker-compose.yml 修改

```yaml
services:
  # ---- 调度器主服务 ----
  scheduler:
    build:
      context: .
      dockerfile: Dockerfile
    container_name: task-scheduler
    volumes:
      - ./config/docker-config.yaml:/app/config/config.yaml:ro
    ports:
      - "9002:9002"
      - "9003:9003"
    depends_on:
      mysql:
        condition: service_healthy    # ← 保持不变
      redis:
        condition: service_healthy    # ← 从 service_started 改为 service_healthy
      kafka:
        condition: service_healthy    # ← 保持不变
      etcd:
        condition: service_healthy    # ← 从 service_started 改为 service_healthy
    restart: unless-stopped
    networks:
      - task-network

  # ---- MySQL 8.0 ----
  mysql:
    image: mysql:8.0.29
    container_name: task-mysql
    command: --default_authentication_plugin=mysql_native_password
    environment:
      MYSQL_ROOT_PASSWORD: root
    volumes:
      - ./scripts/mysql/init.sql:/docker-entrypoint-initdb.d/init.sql
      - mysql-data:/var/lib/mysql
    ports:
      - "13316:3306"
    healthcheck:
      test: ["CMD", "mysqladmin", "ping", "-h", "localhost", "-u", "root", "--password=root"]
      interval: 2s
      timeout: 5s
      retries: 15
      start_period: 10s
    networks:
      - task-network

  # ---- Redis ----
  redis:
    image: "redislabs/rebloom:latest"
    container_name: task-redis
    command: redis-server --notify-keyspace-events AKE --loadmodule /usr/lib/redis/modules/redisbloom.so
    environment:
      - ALLOW_EMPTY_PASSWORD=yes
    ports:
      - "6379:6379"
    volumes:
      - redis-data:/data
    healthcheck:                        # ← 新增
      test: ["CMD", "redis-cli", "ping"]
      interval: 5s
      timeout: 3s
      retries: 5
      start_period: 5s
    networks:
      - task-network

  # ---- Kafka 3.9.0 (KRaft 模式) ----
  kafka:
    image: "bitnami/kafka:3.9.0"
    container_name: task-kafka
    ports:
      - "9092:9092"
      - "9094:9094"
    environment:
      - KAFKA_CFG_NODE_ID=0
      - KAFKA_CFG_AUTO_CREATE_TOPICS_ENABLE=true
      - KAFKA_CFG_PROCESS_ROLES=controller,broker
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
    networks:
      - task-network

  # ---- etcd ----
  etcd:
    image: "bitnami/etcd:latest"
    container_name: task-etcd
    environment:
      - ALLOW_NONE_AUTHENTICATION=yes
      - ETCD_ADVERTISE_CLIENT_URLS=http://etcd:2379
    ports:
      - "2379:2379"
      - "2380:2380"
    volumes:
      - etcd-data:/bitnami/etcd
    healthcheck:                        # ← 新增
      test: ["CMD-SHELL", "etcdctl endpoint health || exit 1"]
      interval: 10s
      timeout: 5s
      retries: 5
      start_period: 10s
    networks:
      - task-network

  # ---- Prometheus ----
  prometheus:
    image: prom/prometheus:latest
    container_name: task-prometheus
    user: root
    volumes:
      - ./scripts/prometheus/prometheus.yml:/etc/prometheus/prometheus.yml
      - prometheus-data:/prometheus
    ports:
      - "9090:9090"
    command:
      - "--web.enable-remote-write-receiver"
      - "--config.file=/etc/prometheus/prometheus.yml"
    healthcheck:                        # ← 新增
      test: ["CMD-SHELL", "wget -qO- http://localhost:9090/-/healthy || exit 1"]
      interval: 15s
      timeout: 5s
      retries: 3
      start_period: 10s
    networks:
      - task-network

  # ---- Grafana ----
  grafana:
    image: grafana/grafana-enterprise:latest
    container_name: task-grafana
    ports:
      - "3000:3000"
    environment:
      - GF_SECURITY_ADMIN_PASSWORD=123
    volumes:
      - grafana-data:/var/lib/grafana
    depends_on:
      prometheus:
        condition: service_healthy     # ← 从隐式 service_started 改为 service_healthy
    healthcheck:                        # ← 新增
      test: ["CMD-SHELL", "wget -qO- http://localhost:3000/api/health || exit 1"]
      interval: 15s
      timeout: 5s
      retries: 3
      start_period: 15s
    networks:
      - task-network

volumes:
  mysql-data:
  redis-data:
  kafka-data:
  etcd-data:
  prometheus-data:
  grafana-data:

networks:
  task-network:
    driver: bridge
```

### 5.3 健康检查参数设计

| 服务 | 检查命令 | interval | timeout | retries | start_period | 设计理由 |
|------|---------|----------|---------|---------|-------------|---------|
| mysql | `mysqladmin ping` | 2s | 5s | 15 | 10s | MySQL 初始化慢，需要多次重试 |
| redis | `redis-cli ping` | 5s | 3s | 5 | 5s | Redis 启动快，5s 足够 |
| kafka | `kafka-broker-api-versions.sh` | 10s | 10s | 5 | 30s | KRaft 初始化需要较长时间 |
| etcd | `etcdctl endpoint health` | 10s | 5s | 5 | 10s | 单节点启动快，预留选举时间 |
| prometheus | `wget localhost:9090/-/healthy` | 15s | 5s | 3 | 10s | 无状态，启动快 |
| grafana | `wget localhost:3000/api/health` | 15s | 5s | 3 | 15s | 需要初始化数据库 |
| scheduler | Dockerfile `wget localhost:9003/debug/health` | 15s | 5s | 3 | 10s | 依赖全部就绪后启动 |

### 5.4 启动顺序变更

```
# 改造前：
mysql(healthy)  ──┐
redis(started)  ──┤         ← redis 可能还在加载
kafka(healthy)  ──┼──→ scheduler
etcd(started)   ──┘         ← etcd 可能还未就绪
prometheus ──→ grafana      ← 无健康检查保障

# 改造后：
mysql(healthy)       ──┐
redis(healthy)       ──┤    ← 确保 redis-cli PING 返回 PONG
kafka(healthy)       ──┼──→ scheduler
etcd(healthy)        ──┘    ← 确保 etcdctl endpoint health 通过
prometheus(healthy)  ──→ grafana(healthy)
```

---

## 6. CI/CD Pipeline

### 6.1 设计目标

```
┌──────────────────────────────────────────────────────────────────┐
│                    GitHub Actions CI Pipeline                     │
│                                                                  │
│   push/PR ──→ lint ──→ unit-test ──→ integration-test ──→ build  │
│                │           │              │               │       │
│                ▼           ▼              ▼               ▼       │
│           golangci-lint  coverage    docker services   docker     │
│           + staticcheck   report     mysql/redis/...    image    │
│                           │                                      │
│                           ▼                                      │
│                        codecov                                   │
└──────────────────────────────────────────────────────────────────┘
```

### 6.2 完整 GitHub Actions 配置

```yaml
# .github/workflows/ci.yml
name: CI

on:
  push:
    branches: [ master, main, develop ]
  pull_request:
    branches: [ master, main ]

env:
  GO_VERSION: '1.24'
  GOLANGCI_LINT_VERSION: 'v1.62'

jobs:
  # ============================================================
  # Job 1: 代码规范检查
  # ============================================================
  lint:
    name: Lint
    runs-on: ubuntu-latest
    steps:
      - name: Checkout code
        uses: actions/checkout@v4

      - name: Setup Go
        uses: actions/setup-go@v5
        with:
          go-version: ${{ env.GO_VERSION }}
          cache: true

      - name: golangci-lint
        uses: golangci/golangci-lint-action@v6
        with:
          version: ${{ env.GOLANGCI_LINT_VERSION }}
          args: --config=./scripts/lint/.golangci.yaml --timeout=5m
          # 仅在 PR 上评论 lint 结果
          only-new-issues: true

  # ============================================================
  # Job 2: 单元测试 + 覆盖率
  # ============================================================
  unit-test:
    name: Unit Test
    runs-on: ubuntu-latest
    steps:
      - name: Checkout code
        uses: actions/checkout@v4

      - name: Setup Go
        uses: actions/setup-go@v5
        with:
          go-version: ${{ env.GO_VERSION }}
          cache: true

      - name: Run unit tests
        run: |
          go test -race -shuffle=on -short -failfast \
            -tags=unit \
            -count=1 \
            -coverprofile=coverage-unit.out \
            -covermode=atomic \
            ./...

      - name: Upload coverage to Codecov
        uses: codecov/codecov-action@v4
        with:
          files: coverage-unit.out
          flags: unit
          fail_ci_if_error: false
          token: ${{ secrets.CODECOV_TOKEN }}

  # ============================================================
  # Job 3: 集成测试（需要中间件服务）
  # ============================================================
  integration-test:
    name: Integration Test
    runs-on: ubuntu-latest
    services:
      mysql:
        image: mysql:8.0.29
        env:
          MYSQL_ROOT_PASSWORD: root
        ports:
          - 13316:3306
        options: >-
          --health-cmd="mysqladmin ping -h localhost -u root --password=root"
          --health-interval=2s
          --health-timeout=5s
          --health-retries=15
          --health-start-period=10s
      redis:
        image: redis:7-alpine
        ports:
          - 6379:6379
        options: >-
          --health-cmd="redis-cli ping"
          --health-interval=5s
          --health-timeout=3s
          --health-retries=5
      kafka:
        image: bitnami/kafka:3.9.0
        env:
          KAFKA_CFG_NODE_ID: "0"
          KAFKA_CFG_PROCESS_ROLES: controller,broker
          KAFKA_CFG_LISTENERS: PLAINTEXT://:9092,CONTROLLER://:9093
          KAFKA_CFG_ADVERTISED_LISTENERS: PLAINTEXT://localhost:9092
          KAFKA_CFG_CONTROLLER_QUORUM_VOTERS: 0@localhost:9093
          KAFKA_CFG_CONTROLLER_LISTENER_NAMES: CONTROLLER
          KAFKA_CFG_AUTO_CREATE_TOPICS_ENABLE: "true"
        ports:
          - 9092:9092
        options: >-
          --health-cmd="kafka-broker-api-versions.sh --bootstrap-server localhost:9092 || exit 1"
          --health-interval=10s
          --health-timeout=10s
          --health-retries=5
          --health-start-period=30s
      etcd:
        image: bitnami/etcd:latest
        env:
          ALLOW_NONE_AUTHENTICATION: "yes"
        ports:
          - 2379:2379
    steps:
      - name: Checkout code
        uses: actions/checkout@v4

      - name: Setup Go
        uses: actions/setup-go@v5
        with:
          go-version: ${{ env.GO_VERSION }}
          cache: true

      - name: Initialize MySQL schema
        run: |
          # 等待 MySQL 完全就绪
          for i in $(seq 1 30); do
            if mysql -h 127.0.0.1 -P 13316 -u root -proot -e "SELECT 1" &>/dev/null; then
              break
            fi
            sleep 2
          done
          # 执行初始化脚本
          mysql -h 127.0.0.1 -P 13316 -u root -proot < scripts/mysql/init.sql

      - name: Run integration tests
        run: |
          go test -race -shuffle=on -failfast \
            -tags=e2e \
            -count=1 \
            -coverprofile=coverage-e2e.out \
            -covermode=atomic \
            ./...

      - name: Upload coverage to Codecov
        uses: codecov/codecov-action@v4
        with:
          files: coverage-e2e.out
          flags: integration
          fail_ci_if_error: false
          token: ${{ secrets.CODECOV_TOKEN }}

  # ============================================================
  # Job 4: Docker 镜像构建验证
  # ============================================================
  build:
    name: Build Docker Image
    runs-on: ubuntu-latest
    needs: [lint, unit-test]  # lint 和单元测试通过后才构建
    steps:
      - name: Checkout code
        uses: actions/checkout@v4

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v3

      - name: Build Docker image
        uses: docker/build-push-action@v6
        with:
          context: .
          push: false
          tags: task-scheduler:${{ github.sha }}
          cache-from: type=gha
          cache-to: type=gha,mode=max
```

### 6.3 golangci-lint 配置增强

在现有 `scripts/lint/.golangci.yaml` 基础上，确保关键检查已启用：

```yaml
# scripts/lint/.golangci.yaml（增量修改）

linters-settings:
  staticcheck:
    checks:
      - "-SA4008"
      # 确保 SA4006 启用（检测赋值后未使用的变量）
      # SA4006 默认已启用，只要不在此处排除即可
      # 这能捕获类似 "eerr 赋值后返回 err" 的 bug

  govet:
    shadow: true    # 检测变量遮蔽

  errcheck:
    check-type-assertions: true  # 检查类型断言的 error
```

**SA4006 能捕获的典型问题**：

```go
// 项目中的实际 Bug（Plan 错误处理）
func (p *PlanTask) getPlanData(...) error {
    eerr := p.buildDAG(dslExpression)
    if eerr != nil {
        return err   // ← SA4006 警告：eerr 赋值后未使用
    }
}
```

### 6.4 Codecov 配置

```yaml
# codecov.yml（项目根目录）
codecov:
  require_ci_to_pass: false

coverage:
  precision: 2
  round: down
  range: "40...80"

  status:
    project:
      default:
        target: auto
        threshold: 2%    # 允许覆盖率下降不超过 2%
    patch:
      default:
        target: 60%      # 新增代码至少 60% 覆盖率

flags:
  unit:
    paths:
      - "internal/"
      - "pkg/"
    carryforward: true
  integration:
    paths:
      - "internal/"
    carryforward: true

ignore:
  - "api/proto/gen/**"     # 生成代码
  - "ioc/**"               # Wire 注入（测试价值低）
  - "cmd/**"               # 入口文件
  - "example/**"           # 示例代码
  - "scripts/**"
```

### 6.5 Makefile 集成

```makefile
# Makefile 新增 CI 相关目标

# CI 全量检查（本地模拟 CI 流水线）
.PHONY: ci
ci:
	@$(MAKE) --no-print-directory lint
	@$(MAKE) --no-print-directory ut
	@echo "✅ CI checks passed"

# 覆盖率报告（本地查看）
.PHONY: coverage
coverage:
	@go test -race -tags=unit -coverprofile=coverage.out -covermode=atomic ./...
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"
```

---

## 7. 测试覆盖不足 → 关键模块单元测试

### 7.1 现状分析

| 测试类型 | 覆盖范围 | 文件位置 |
|---------|---------|---------|
| 单元测试（`//go:build unit`） | DSL Parser、ShardingRule、重试策略 | `internal/dsl/parser/`、`internal/domain/`、`pkg/retry/` |
| 集成测试（`//go:build e2e`） | DAO 层 CRUD、Picker、分片任务全流程 | `internal/test/integration/` |
| E2E 测试 | 模拟执行节点 | `example/` |

**缺失的关键测试**：

| 优先级 | 模块 | 缺失的测试场景 |
|--------|------|---------------|
| P0 | 状态机转换 | `TaskExecutionStatus.IsValid()` 边界、`CanTransitionTo()` 合法/非法路径 |
| P0 | 重试策略边界 | 指数退避溢出、`maxRetries=0`、`context.Cancel` 中断 |
| P1 | DAG 构建 | 环检测、孤立节点、复杂 DSL 表达式解析 |
| P1 | CompositeChecker | AND/OR 组合策略、空 checkers 列表、全通过/全失败 |
| P2 | ShardingRule | 权重分布均匀性、`totalNums=0` 边界 |

### 7.2 测试框架选型

```go
import (
    "testing"
    
    "github.com/stretchr/testify/assert"   // 断言
    "github.com/stretchr/testify/require"  // 致命断言（失败则停止）
    "github.com/stretchr/testify/suite"    // 测试套件（集成测试用）
    "go.uber.org/mock/gomock"              // Mock 生成
)
```

**选型理由**：
- **testify**：Go 生态最广泛使用的断言库，项目已在用
- **gomock**：官方维护的 Mock 框架，与 interface 配合好
- 不引入 ginkgo/gomega 等 BDD 框架——保持与项目现有风格一致

### 7.3 状态机转换测试

```go
//go:build unit

package domain_test

import (
    "testing"

    "gitee.com/flycash/distributed_task_platform/internal/domain"
    "github.com/stretchr/testify/assert"
)

func TestTaskExecutionStatus_IsValid(t *testing.T) {
    t.Parallel()
    tests := []struct {
        name   string
        status domain.TaskExecutionStatus
        want   bool
    }{
        {"PREPARE is valid", domain.TaskExecutionStatusPrepare, true},
        {"RUNNING is valid", domain.TaskExecutionStatusRunning, true},
        {"SUCCESS is valid", domain.TaskExecutionStatusSuccess, true},
        {"FAILED is valid", domain.TaskExecutionStatusFailed, true},
        {"FAILED_RETRYABLE is valid", domain.TaskExecutionStatusFailedRetryable, true},
        {"UNKNOWN is invalid", domain.TaskExecutionStatusUnknown, false},
        {"empty string is invalid", domain.TaskExecutionStatus(""), false},
        {"arbitrary string is invalid", domain.TaskExecutionStatus("CANCELLED"), false},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()
            assert.Equal(t, tt.want, tt.status.IsValid())
        })
    }
}

func TestTaskExecutionStatus_IsTerminalStatus(t *testing.T) {
    t.Parallel()
    tests := []struct {
        name   string
        status domain.TaskExecutionStatus
        want   bool
    }{
        {"SUCCESS is terminal", domain.TaskExecutionStatusSuccess, true},
        {"FAILED is terminal", domain.TaskExecutionStatusFailed, true},
        {"RUNNING is not terminal", domain.TaskExecutionStatusRunning, false},
        {"PREPARE is not terminal", domain.TaskExecutionStatusPrepare, false},
        {"FAILED_RETRYABLE is not terminal", domain.TaskExecutionStatusFailedRetryable, false},
        {"FAILED_RESCHEDULED is not terminal", domain.TaskExecutionStatusFailedRescheduled, false},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()
            assert.Equal(t, tt.want, tt.status.IsTerminalStatus())
        })
    }
}

func TestTaskExecutionStatus_StatusCheckers(t *testing.T) {
    t.Parallel()
    
    // 验证每个状态的判断方法互斥
    statuses := []struct {
        status      domain.TaskExecutionStatus
        isPrepare   bool
        isRunning   bool
        isSuccess   bool
        isFailed    bool
        isRetryable bool
        isResched   bool
    }{
        {domain.TaskExecutionStatusPrepare, true, false, false, false, false, false},
        {domain.TaskExecutionStatusRunning, false, true, false, false, false, false},
        {domain.TaskExecutionStatusSuccess, false, false, true, false, false, false},
        {domain.TaskExecutionStatusFailed, false, false, false, true, false, false},
        {domain.TaskExecutionStatusFailedRetryable, false, false, false, false, true, false},
        {domain.TaskExecutionStatusFailedRescheduled, false, false, false, false, false, true},
    }

    for _, tt := range statuses {
        t.Run(string(tt.status), func(t *testing.T) {
            t.Parallel()
            assert.Equal(t, tt.isPrepare, tt.status.IsPrepare())
            assert.Equal(t, tt.isRunning, tt.status.IsRunning())
            assert.Equal(t, tt.isSuccess, tt.status.IsSuccess())
            assert.Equal(t, tt.isFailed, tt.status.IsFailed())
            assert.Equal(t, tt.isRetryable, tt.status.IsFailedRetryable())
            assert.Equal(t, tt.isResched, tt.status.IsFailedRescheduled())
        })
    }
}
```

### 7.4 重试策略边界测试

```go
//go:build unit

package strategy_test

import (
    "context"
    "testing"
    "time"

    "gitee.com/flycash/distributed_task_platform/pkg/retry/strategy"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestExponentialBackoff_BoundaryConditions(t *testing.T) {
    t.Parallel()

    t.Run("max retries exhausted", func(t *testing.T) {
        t.Parallel()
        s := strategy.NewExponentialBackoffRetryStrategy(
            100*time.Millisecond, // initialInterval
            1*time.Second,        // maxInterval
            3,                    // maxRetries
        )

        // 应该能重试 3 次
        for i := 0; i < 3; i++ {
            interval, err := s.Next(context.Background())
            require.NoError(t, err)
            assert.True(t, interval > 0, "retry %d should have positive interval", i)
        }

        // 第 4 次应该失败
        _, err := s.Next(context.Background())
        assert.Error(t, err, "should fail after max retries")
    })

    t.Run("interval capped at maxInterval", func(t *testing.T) {
        t.Parallel()
        s := strategy.NewExponentialBackoffRetryStrategy(
            500*time.Millisecond,
            1*time.Second,  // maxInterval = 1s
            10,
        )

        // 指数增长应该在某个点被 maxInterval 截断
        for i := 0; i < 5; i++ {
            interval, err := s.Next(context.Background())
            require.NoError(t, err)
            assert.LessOrEqual(t, interval, 1*time.Second,
                "interval should never exceed maxInterval")
        }
    })

    t.Run("single retry allowed", func(t *testing.T) {
        t.Parallel()
        s := strategy.NewExponentialBackoffRetryStrategy(
            100*time.Millisecond,
            1*time.Second,
            1, // 只允许 1 次重试
        )

        _, err := s.Next(context.Background())
        require.NoError(t, err)

        _, err = s.Next(context.Background())
        assert.Error(t, err, "second retry should fail")
    })
}
```

### 7.5 CompositeChecker 组合测试

```go
//go:build unit

package loadchecker_test

import (
    "context"
    "testing"
    "time"

    "gitee.com/flycash/distributed_task_platform/pkg/loadchecker"
    "github.com/stretchr/testify/assert"
)

// mockChecker 模拟负载检查器
type mockChecker struct {
    duration time.Duration
    pass     bool
}

func (m *mockChecker) Check(_ context.Context) (time.Duration, bool) {
    return m.duration, m.pass
}

func TestCompositeChecker_AND(t *testing.T) {
    t.Parallel()

    t.Run("all pass", func(t *testing.T) {
        t.Parallel()
        checker := loadchecker.NewCompositeChecker(
            loadchecker.StrategyAND,
            &mockChecker{pass: true},
            &mockChecker{pass: true},
            &mockChecker{pass: true},
        )
        duration, shouldSchedule := checker.Check(context.Background())
        assert.True(t, shouldSchedule)
        assert.Equal(t, time.Duration(0), duration)
    })

    t.Run("one fails → all fail, return max duration", func(t *testing.T) {
        t.Parallel()
        checker := loadchecker.NewCompositeChecker(
            loadchecker.StrategyAND,
            &mockChecker{pass: true},
            &mockChecker{pass: false, duration: 5 * time.Second},
            &mockChecker{pass: false, duration: 10 * time.Second},
        )
        duration, shouldSchedule := checker.Check(context.Background())
        assert.False(t, shouldSchedule)
        assert.Equal(t, 10*time.Second, duration, "should return max sleep duration")
    })

    t.Run("empty checkers → pass", func(t *testing.T) {
        t.Parallel()
        checker := loadchecker.NewCompositeChecker(loadchecker.StrategyAND)
        duration, shouldSchedule := checker.Check(context.Background())
        assert.True(t, shouldSchedule)
        assert.Equal(t, time.Duration(0), duration)
    })
}

func TestCompositeChecker_OR(t *testing.T) {
    t.Parallel()

    t.Run("one pass → short circuit", func(t *testing.T) {
        t.Parallel()
        checker := loadchecker.NewCompositeChecker(
            loadchecker.StrategyOR,
            &mockChecker{pass: false, duration: 10 * time.Second},
            &mockChecker{pass: true}, // 短路
            &mockChecker{pass: false, duration: 5 * time.Second},
        )
        duration, shouldSchedule := checker.Check(context.Background())
        assert.True(t, shouldSchedule)
        assert.Equal(t, time.Duration(0), duration)
    })

    t.Run("all fail → return min duration", func(t *testing.T) {
        t.Parallel()
        checker := loadchecker.NewCompositeChecker(
            loadchecker.StrategyOR,
            &mockChecker{pass: false, duration: 10 * time.Second},
            &mockChecker{pass: false, duration: 3 * time.Second},
            &mockChecker{pass: false, duration: 7 * time.Second},
        )
        duration, shouldSchedule := checker.Check(context.Background())
        assert.False(t, shouldSchedule)
        assert.Equal(t, 3*time.Second, duration, "should return min sleep duration")
    })
}

func TestCompositeChecker_DefaultStrategy(t *testing.T) {
    t.Parallel()
    // 非法策略值应该降级为 AND
    checker := loadchecker.NewCompositeChecker(
        loadchecker.CompositeStrategy(999), // 非法值
        &mockChecker{pass: true},
    )
    _, shouldSchedule := checker.Check(context.Background())
    assert.True(t, shouldSchedule, "unknown strategy should fallback to AND")
}
```

### 7.6 DAG 构建关键测试

```go
//go:build unit

package parser_test

// 补充到 internal/dsl/parser/visitor_test.go

func TestTaskOrchestrationVisitor_EdgeCases(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        wantErr bool
        desc    string
    }{
        {
            name:    "single task",
            input:   "A;",
            wantErr: false,
            desc:    "单节点 DAG，A 就是起点也是终点",
        },
        {
            name:    "diamond dependency",
            input:   "A->(B&&C); B->D; C->D;",
            wantErr: false,
            desc:    "菱形依赖：A → B,C → D",
        },
        {
            name:    "parallel branches",
            input:   "A->B; A->C; A->D;",
            wantErr: false,
            desc:    "扇出：A 同时触发 B/C/D",
        },
        {
            name:    "empty input",
            input:   "",
            wantErr: true,
            desc:    "空输入应该报错",
        },
        {
            name:    "invalid syntax",
            input:   "A->->B;",
            wantErr: true,
            desc:    "语法错误应该报错",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()
            lexer := parser.NewTaskOrchestrationLexer(antlr.NewInputStream(tt.input))
            stream := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
            p := parser.NewTaskOrchestrationParser(stream)
            
            visitor := NewTaskOrchestrationVisitor()
            result := visitor.Visit(p.Program())
            
            if tt.wantErr {
                planRes, ok := result.(*planRes)
                require.True(t, ok)
                assert.Error(t, planRes.err, tt.desc)
            } else {
                planRes, ok := result.(*planRes)
                require.True(t, ok)
                assert.NoError(t, planRes.err, tt.desc)
            }
        })
    }
}
```

### 7.7 测试覆盖率目标

| 模块 | 当前覆盖率（估算） | 目标覆盖率 | 优先级 |
|------|-------------------|-----------|--------|
| `internal/domain/` | ~30% | 80% | P0 |
| `pkg/retry/strategy/` | ~50% | 90% | P0 |
| `pkg/loadchecker/` | 0% | 80% | P1 |
| `internal/dsl/parser/` | ~40% | 70% | P1 |
| `internal/compensator/` | 0%（仅集成测试） | 60% | P2 |
| `pkg/sharding/` | 0% | 70% | P2 |

---

## 8. 设计决策分析

### 8.1 自定义 Duration vs 第三方库

| 方案 | 优点 | 缺点 |
|------|------|------|
| **自定义 Duration**（当前选择） | 零依赖、代码量少（~30 行）、向后兼容 | 需要改所有配置结构体 |
| `spf13/viper` 内置 Duration | 功能丰富、社区维护 | 引入重量级依赖、与 ego 框架不兼容 |
| `envconfig` Duration | 支持环境变量 | 不支持 YAML 嵌套结构 |

**决策**：自定义实现。理由：
1. 代码量极小，不值得引入新依赖
2. 与 ego 框架的 YAML 配置加载机制完全兼容
3. 向后兼容纳秒格式，允许渐进式迁移

### 8.2 策略模式 vs 泛型模式消除重复

| 方案 | 优点 | 缺点 |
|------|------|------|
| **策略模式（接口）**（当前选择） | 符合 Go 惯用写法、清晰的职责分离 | 增加文件数 |
| 泛型函数 `RunCompensation[T]` | 代码更紧凑 | Go 泛型对 interface 方法集的支持有限 |
| 代码生成（go generate） | 零运行时开销 | 维护复杂、调试困难 |

**决策**：策略模式。理由：
1. Go 社区推崇"接口小而专"的设计哲学
2. `CompensationLogic` 接口只有 3 个方法，足够简洁
3. 新增补偿器只需实现接口，不需要理解框架内部

### 8.3 CI 中集成测试的服务编排

| 方案 | 优点 | 缺点 |
|------|------|------|
| **GitHub Actions services**（当前选择） | 原生支持、自动清理、声明式 | 服务配置选项有限 |
| Docker Compose in CI | 复用开发环境配置 | 需要手动管理生命周期 |
| Testcontainers-Go | 代码级控制、自动端口分配 | 需要 Docker-in-Docker 权限 |

**决策**：GitHub Actions services。理由：
1. 最简单、最可靠的 CI 集成方式
2. 健康检查直接在 `options` 中声明
3. 服务在 job 结束后自动清理，无泄漏风险

---

## 9. 面试高频 Q&A

### Q1: 为什么配置文件用纳秒值？Go 不是支持 "10s" 格式吗？

Go 的 `time.Duration` 确实支持 `time.ParseDuration("10s")`，但 YAML 反序列化时，`time.Duration` 的底层类型是 `int64`，YAML 库会直接解析为数字。要支持 `"10s"` 格式，需要自定义类型实现 `yaml.Unmarshaler`。这是 Go + YAML 配置的一个经典坑——很多项目都会踩到。解法是包装一个 `config.Duration` 类型，20 行代码解决问题，同时保持向后兼容。

### Q2: 为什么 V1 和 V2 补偿器不直接合并成一个？

V1 和 V2 的**驱动框架完全不同**：V1 是简单的 for-loop + context cancel，适合单节点；V2 是 ShardingLoopJob + 分布式锁 + 资源信号量，支持多节点协调。它们需要独立存在，因为通过 Feature Flag 控制使用哪个版本。但它们的**业务逻辑**（查找 + 处理）完全相同，所以用策略模式把逻辑提取出来共享。这是经典的"变化点隔离"——变化的是驱动框架，不变的是业务逻辑。

### Q3: Docker 健康检查和代码级重试是重复防护吗？有必要吗？

不是重复，是**分层防护**。健康检查解决**启动顺序**问题——确保依赖服务完全就绪后才启动 scheduler。代码级重试解决**运行时瞬断**问题——比如 MySQL 做 failover、Redis 主从切换时的短暂不可用。类比安全领域的纵深防御：门禁卡（healthcheck）保证你进入的是工作时间，但进去后电梯可能临时检修（运行时故障），这时候你需要等电梯恢复（指数退避）。少了任何一层，都会增加系统的脆弱性。

### Q4: GitHub Actions 的集成测试为什么不直接用项目的 docker-compose.yml？

两个原因。一是 GitHub Actions 的 `services` 声明方式比 `docker compose up` 更可靠——服务在 job 开始前就启动，在 job 结束后自动清理，不需要手动管理生命周期。二是 CI 环境的端口映射和网络配置与本地不同——`services` 中的容器直接在同一网络，用 `localhost` 即可访问，而 docker-compose 需要处理网络模式差异。保持 CI 配置独立也避免了开发环境的 docker-compose 变更意外影响 CI。

### Q5: 测试覆盖率目标为什么不是 100%？

100% 覆盖率是伪命题。追求 100% 会导致大量"为了覆盖而覆盖"的无意义测试（比如测试 getter/setter），维护成本高但 bug 发现率低。我的原则是**按 ROI 排序**：状态机和重试策略的边界测试 ROI 最高（bug 影响面大 + 逻辑复杂），所以目标 80-90%。DAO 层用集成测试覆盖比单元测试更有价值（能发现 SQL 问题）。Wire 注入代码和入口文件没有测试价值，直接排除。这也是 codecov.yml 里设置 `patch.target: 60%` 而不是 100% 的原因。

### Q6: golangci-lint 里为什么要禁用 SA4008 但保留 SA4006？

SA4008 检测的是 "dead code after unconditional return"，在某些代码生成场景（ANTLR4 生成的 parser）中会误报。SA4006 检测的是 "变量赋值后未使用"，这能捕获项目中实际存在的 bug——比如 `eerr := buildDAG(); if eerr != nil { return err }` 这种变量名拼写错误。禁用和启用 lint 规则不是一刀切的，要根据项目实际情况权衡误报率和漏报率。

---

## 10. 实现局限与改进方向

| # | 现状 | 改进方案 | 优先级 |
|---|------|---------|--------|
| 1 | CI 只有构建验证，未推送镜像到 Registry | 添加 Docker Hub / GHCR 推送步骤 + 版本标签 | 中 |
| 2 | 集成测试在 CI 中用 GitHub Services，与本地 docker-compose 不一致 | 统一测试容器配置，或引入 Testcontainers-Go | 低 |
| 3 | 无 CD（持续部署）流程 | 添加 ArgoCD/Flux 的 GitOps 部署流程 | 中 |
| 4 | 测试覆盖率还有提升空间 | 补充 service 层 Mock 测试、property-based testing | 中 |
| 5 | 无性能基准测试 CI | 添加 benchstat 对比，PR 中自动展示性能回归 | 低 |
| 6 | 配置重构需要逐个替换所有结构体字段类型 | 考虑代码生成自动迁移 | 低 |

---

## 11. 完整改动文件清单

| 文件 | 变更类型 | 说明 |
|------|---------|------|
| `pkg/config/duration.go` | 新增 | 自定义 Duration 类型 |
| `config/config.yaml` | 修改 | 纳秒 → 可读格式 |
| `config/docker-config.yaml` | 修改 | 同上 |
| `internal/compensator/types.go` | 新增 | CompensationLogic 接口 |
| `internal/compensator/executor.go` | 新增 | CompensationExecutor |
| `internal/compensator/interrupt_logic.go` | 新增 | 中断补偿逻辑 |
| `internal/compensator/retry_logic.go` | 新增 | 重试补偿逻辑 |
| `internal/compensator/reschedule_logic.go` | 新增 | 重调度补偿逻辑 |
| `internal/compensator/sharding_logic.go` | 新增 | 分片汇总补偿逻辑 |
| `internal/compensator/interrupt.go` | 修改 | 简化为驱动框架 |
| `internal/compensator/interruptv2.go` | 修改 | 简化为驱动框架 |
| `internal/compensator/retry.go` | 修改 | 简化 |
| `internal/compensator/retryv2.go` | 修改 | 简化 |
| `internal/compensator/reschedule.go` | 修改 | 简化 |
| `internal/compensator/reschedulev2.go` | 修改 | 简化 |
| `internal/compensator/sharding.go` | 修改 | 简化 |
| `internal/compensator/shardingv2.go` | 修改 | 简化 |
| `docker-compose.yml` | 修改 | 全服务 healthcheck |
| `.github/workflows/ci.yml` | 新增 | CI 流水线 |
| `codecov.yml` | 新增 | 覆盖率配置 |
| `internal/domain/task_execution_test.go` | 新增 | 状态机测试 |
| `pkg/loadchecker/composite_test.go` | 新增 | CompositeChecker 测试 |
| `Makefile` | 修改 | 新增 ci/coverage 目标 |
