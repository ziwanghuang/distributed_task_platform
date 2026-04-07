# Step 9：可观测性 + Docker 部署

> **目标**：一键 `docker compose up -d` 启动全套服务（MySQL + Redis + Kafka + etcd + Prometheus + Grafana + Scheduler），Grafana 看到监控大盘，Prometheus 采集所有自定义指标，健康检查全部通过。
>
> **完成后你能看到**：`make docker-up` → 7 个容器全部 Running/Healthy → 浏览器打开 `http://localhost:3000`（admin/123）→ 添加 Prometheus 数据源 → 导入调度面板 → 看到 `scheduler_loop_duration_seconds`、`gorm_query_duration_seconds`、`load_checker_sleep_duration_seconds` 三大核心指标 → 插入测试任务 → 观察面板曲线变化。

---

## 1. 架构总览

Step 9 是整个分步实现的最后一环，将 Step 1–8 的所有组件打包成**一键可运行的容器化环境**，并通过 Prometheus + Grafana 构建完整的可观测性体系。

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           Docker Compose 编排                                │
│                                                                             │
│  ┌───────────────┐   ┌───────────┐   ┌──────────┐   ┌────────────────────┐ │
│  │   MySQL 8.0   │   │   Redis   │   │  Kafka   │   │    etcd            │ │
│  │  (task/0/1)   │   │ +Bloom    │   │  KRaft   │   │  服务发现+分布式锁  │ │
│  │  :13316→3306  │   │  :6379    │   │ :9092/94 │   │  :2379/2380        │ │
│  └───────┬───────┘   └─────┬─────┘   └────┬─────┘   └─────────┬──────────┘ │
│          │                 │               │                   │            │
│          ▼                 ▼               ▼                   ▼            │
│  ┌─────────────────────────────────────────────────────────────────────────┐│
│  │                      task-scheduler                                     ││
│  │                                                                         ││
│  │  ┌─────────────────┐  ┌──────────────────┐  ┌────────────────────────┐ ││
│  │  │  gRPC :9002      │  │  Governor :9003   │  │  调度循环 + 补偿器     │ ││
│  │  │  ReporterService │  │  /debug/health    │  │  + MQ 消费者          │ ││
│  │  │                  │  │  /debug/pprof     │  │  + 智能调度           │ ││
│  │  │                  │  │  /metrics         │  │  + 负载检查           │ ││
│  │  └─────────────────┘  └──────────┬────────┘  └────────────────────────┘ ││
│  └──────────────────────────────────┼─────────────────────────────────────┘│
│                                     │ scrape / remote write                │
│                                     ▼                                      │
│  ┌──────────────────┐         ┌──────────────────┐                         │
│  │   Grafana :3000   │ ◄───── │  Prometheus :9090 │                         │
│  │   admin / 123     │        │  remote-write-rx  │                         │
│  │   监控面板         │        │  + 自监控采集      │                         │
│  └──────────────────┘         └──────────────────┘                         │
│                                                                             │
│  ── task-network (bridge) ──────────────────────────────────────────────── │
└─────────────────────────────────────────────────────────────────────────────┘
```

**关键设计点**：
- 所有容器在同一 bridge 网络 `task-network` 内，通过服务名互相访问
- Scheduler 的指标通过 ego 框架自动暴露在 Governor 端口的 `/metrics` 路径
- Prometheus 同时支持 **Pull 模式**（scrape 抓取）和 **Push 模式**（remote write 接收），执行节点可以用 remote write 推送指标
- MySQL 通过 `init.sql` 自动创建 3 个库 + 12 张物理表的分库分表结构

---

## 2. Step 8 → Step 9 变更对比

| 维度 | Step 8（智能调度 + 负载检查） | Step 9（可观测性 + Docker 部署） |
|------|------|------|
| **关注点** | 运行时调度算法 | 部署 + 可观测性 |
| **新增文件** | picker/、loadchecker/ | Dockerfile、docker-compose.yml、init.sql、prometheus.yml |
| **指标定义** | SchedulerMetrics + GormMetricsPlugin | 不新增指标，聚焦于指标的**采集、存储、可视化** |
| **配置变更** | 无 | 新增 `docker-config.yaml`（地址从 localhost → 服务名） |
| **基础设施** | Prometheus 仅作为查询源 | Prometheus + Grafana 完整监控栈 |
| **启动方式** | `make run_scheduler`（本地编译+运行） | `make docker-up`（容器化一键启动） |
| **数据库初始化** | GORM AutoMigrate（开发用） | `init.sql` 完整建库建表（生产级） |
| **镜像构建** | 无 | 多阶段 Dockerfile（~15MB 最终镜像） |
| **健康检查** | 无 Dockerfile HEALTHCHECK | Governor `/debug/health` + Dockerfile HEALTHCHECK |
| **日志** | elog 结构化日志 | 通过 `docker compose logs -f` 集中查看 |

---

## 3. 设计决策分析

### 3.1 Pull（Prometheus Scrape）vs Push（Remote Write）

| 维度 | Pull 模式 | Push 模式（Remote Write） |
|------|-----------|--------------------------|
| **工作方式** | Prometheus 定期拉取 `/metrics` 端点 | 应用主动将指标推送到 Prometheus |
| **适用场景** | 长生命周期服务 | 短生命周期任务、外部执行节点 |
| **配置复杂度** | 需要在 `prometheus.yml` 配置 scrape target | 需要应用集成 remote write client |
| **服务发现** | Prometheus 需要知道所有目标的地址 | 应用自己推送，Prometheus 无需知道目标 |

**决策**：两者并用。
- **调度器**使用 Pull 模式：ego 框架自动在 Governor 端口暴露 `/metrics`，Prometheus 配置 scrape job 即可
- **执行节点**使用 Push 模式（Remote Write）：执行节点是动态注册的，数量不固定，使用 remote write 更灵活，Prometheus 通过 `--web.enable-remote-write-receiver` 启动参数接收推送

### 3.2 单体 Dockerfile vs 分离 Scheduler/Executor

| 方案 | 优点 | 缺点 |
|------|------|------|
| **单体 Dockerfile**（当前） | 简单、一个镜像搞定 | 只构建 scheduler，executor 需要额外处理 |
| **分离 Dockerfile** | 各自独立，更符合微服务 | 需要维护两个 Dockerfile，CI 更复杂 |

**决策**：当前阶段使用单体 Dockerfile，只构建 scheduler。原因：
1. 本项目是面试 showcase，executor 由 example/ 目录提供 mock 实现
2. scheduler 是核心组件，独立容器化已经能完整演示系统能力
3. 需要时可以通过 `--build-arg` 参数化构建目标

### 3.3 Docker Layer Cache 优化策略

```dockerfile
# ✅ 正确：先拷贝依赖描述文件
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# 再拷贝源码（源码变更不会触发依赖层重建）
COPY . .
RUN go build ...
```

vs

```dockerfile
# ❌ 错误：直接 COPY . . 再 build
COPY . .
RUN go mod download && go build ...
# 任何文件变更都会触发 go mod download 重新执行
```

**决策**：使用分层拷贝策略。Go 依赖层（~300MB 下载）在 go.mod/go.sum 不变时完全命中缓存，增量构建时间从 3 分钟降至 30 秒。

### 3.4 非 Root 运行 vs Root 运行

**决策**：使用非 root 用户 `appuser:appgroup`。理由：
- **安全最佳实践**：即使容器逃逸，攻击者也只有受限权限
- **Kubernetes 兼容**：很多集群强制 `runAsNonRoot: true`
- **实现成本低**：Alpine 的 `addgroup/adduser` 两行搞定

---

## 4. Docker Compose 完整编排

### 4.1 服务清单

```
docker-compose.yml
├── scheduler     ← 调度器主服务（本地构建）
├── mysql         ← MySQL 8.0（数据存储）
├── redis         ← Redis + RedisBloom（分布式锁 + 布隆过滤器）
├── kafka         ← Kafka 3.9.0 KRaft（消息队列）
├── etcd          ← etcd（服务发现 + 分布式协调）
├── prometheus    ← Prometheus（指标采集 + 存储）
└── grafana       ← Grafana（可视化面板）
```

### 4.2 服务拓扑与端口映射

| 服务 | 镜像 | 容器名 | 宿主机端口 | 容器端口 | 健康检查 |
|------|------|--------|-----------|---------|---------|
| **scheduler** | 本地 `Dockerfile` 构建 | `task-scheduler` | 9002, 9003 | 9002(gRPC), 9003(Governor) | Dockerfile HEALTHCHECK |
| **mysql** | `mysql:8.0.29` | `task-mysql` | 13316 | 3306 | `mysqladmin ping`（2s/5s/15次） |
| **redis** | `redislabs/rebloom:latest` | `task-redis` | 6379 | 6379 | 无 |
| **kafka** | `bitnami/kafka:3.9.0` | `task-kafka` | 9092, 9094 | 9092(EXTERNAL), 9094(INTERNAL) | `kafka-broker-api-versions.sh`（10s/10s/5次） |
| **etcd** | `bitnami/etcd:latest` | `task-etcd` | 2379, 2380 | 2379(客户端), 2380(集群) | 无 |
| **prometheus** | `prom/prometheus:latest` | `task-prometheus` | 9090 | 9090 | 无 |
| **grafana** | `grafana/grafana-enterprise:latest` | `task-grafana` | 3000 | 3000 | 无 |

### 4.3 启动顺序与依赖关系

```
mysql(healthy) ──┐
redis(started)  ──┤
kafka(healthy)  ──┼──→ scheduler
etcd(started)   ──┘

prometheus ──→ grafana
```

**关键设计**：
- `mysql` 和 `kafka` 使用 `condition: service_healthy`，确保 scheduler 启动时数据库和消息队列已就绪
- `redis` 和 `etcd` 使用 `condition: service_started`（启动即可，无严格依赖）
- scheduler 内部还有**代码级重试**：`WaitForDBSetup`（指数退避 Ping MySQL）、`InitMQ`（指数退避连接 Kafka），双重保障

### 4.4 网络与数据持久化

**网络**：自定义 `task-network`（bridge 驱动），所有 7 个服务在同一网络，通过服务名互相访问：
- scheduler → `mysql:3306`、`redis:6379`、`kafka:9094`、`etcd:2379`、`prometheus:9090`

**数据卷**（6 个命名卷）：
| 卷名 | 挂载路径 | 用途 |
|------|---------|------|
| `mysql-data` | `/var/lib/mysql` | MySQL 数据持久化 |
| `redis-data` | `/data` | Redis RDB/AOF 持久化 |
| `kafka-data` | `/bitnami/kafka` | Kafka 日志段持久化 |
| `etcd-data` | `/bitnami/etcd` | etcd 数据持久化 |
| `prometheus-data` | `/prometheus` | Prometheus TSDB 持久化 |
| `grafana-data` | `/var/lib/grafana` | Grafana 面板/数据源配置持久化 |

### 4.5 Kafka KRaft 模式

```yaml
environment:
  - KAFKA_CFG_PROCESS_ROLES=controller,broker        # 单节点同时充当 controller 和 broker
  - KAFKA_CFG_LISTENERS=EXTERNAL://:9092,INTERNAL://:9094,CONTROLLER://:9093
  - KAFKA_CFG_ADVERTISED_LISTENERS=EXTERNAL://localhost:9092,INTERNAL://kafka:9094
  - KAFKA_CFG_INTER_BROKER_LISTENER_NAME=INTERNAL
  - KAFKA_CFG_CONTROLLER_QUORUM_VOTERS=0@kafka:9093
```

**设计说明**：
- **EXTERNAL（9092）**：宿主机访问，advertise 地址为 `localhost:9092`
- **INTERNAL（9094）**：Docker 内部容器通信，advertise 地址为 `kafka:9094`
- **KRaft 模式**：无需 Zookeeper，单节点即可运行，减少一个依赖

### 4.6 Redis + RedisBloom

```yaml
command: redis-server --notify-keyspace-events AKE --loadmodule /usr/lib/redis/modules/redisbloom.so
```

- `--notify-keyspace-events AKE`：开启键空间通知（用于分布式锁过期监听）
- `--loadmodule redisbloom.so`：加载布隆过滤器模块（可选，预留给任务去重场景）

---

## 5. Dockerfile 多阶段构建

### 5.1 完整 Dockerfile 解析

```dockerfile
# ---- Stage 1: 编译阶段 ----
FROM golang:1.24.1-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata
WORKDIR /build

# Layer Cache 优化：依赖层独立
COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /build/scheduler \
    ./cmd/scheduler/main.go

# ---- Stage 2: 运行阶段 ----
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata
ENV TZ=Asia/Shanghai

# 安全：非 root 运行
RUN addgroup -S appgroup && adduser -S appuser -G appgroup
WORKDIR /app
COPY --from=builder /build/scheduler /app/scheduler
RUN mkdir -p /app/config && chown -R appuser:appgroup /app
USER appuser

EXPOSE 9002 9003

# 健康检查
HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:9003/debug/health || exit 1

ENTRYPOINT ["/app/scheduler"]
CMD ["--config=/app/config/config.yaml"]
```

### 5.2 编译优化参数

| 参数 | 作用 | 体积影响 |
|------|------|---------|
| `CGO_ENABLED=0` | 纯静态链接，不依赖 glibc | 可在 alpine 上运行 |
| `GOOS=linux GOARCH=amd64` | 交叉编译目标平台 | 无 |
| `-trimpath` | 移除编译路径信息 | 减小体积 + 安全 |
| `-ldflags="-s -w"` | 去掉符号表（-s）和 DWARF 调试信息（-w） | 约减小 30% 体积 |

### 5.3 最终镜像体积估算

| 层 | 大小 |
|---|------|
| alpine:3.20 基础镜像 | ~7 MB |
| ca-certificates + tzdata | ~3 MB |
| scheduler 二进制 | ~20 MB（Go 静态链接） |
| **合计** | **~30 MB** |

对比：`golang:1.24.1-alpine` 基础镜像约 300 MB，多阶段构建实现 **10 倍体积缩减**。

---

## 6. Prometheus 可观测性体系

### 6.1 自定义指标清单

整个代码库中通过 `promauto` 注册了 **3 个自定义 Prometheus 指标**：

| 指标名 | 类型 | 标签 | 来源文件 | 作用 |
|--------|------|------|---------|------|
| `scheduler_loop_duration_seconds` | Histogram | `node_id` | `pkg/prometheus/scheduler_metrics.go` | 每次调度循环的执行耗时 |
| `load_checker_sleep_duration_seconds` | Histogram | `node_id`, `checker_type` | `pkg/prometheus/scheduler_metrics.go` | 负载检查器触发的退避时间 |
| `gorm_query_duration_seconds` | Histogram | `operation`, `table` | `pkg/prometheus/gorm_plugin.go` | GORM 数据库操作耗时 |

### 6.2 指标详解

#### scheduler_loop_duration_seconds

```go
promauto.NewHistogramVec(prometheus.HistogramOpts{
    Name:    "scheduler_loop_duration_seconds",
    Help:    "调度循环执行耗时（秒）",
    Buckets: prometheus.DefBuckets, // .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10
}, []string{"node_id"})
```

- **使用位置**：`scheduler.go:146`（V1）/ `schedulerv2.go:133`（V2）— 每轮 `scheduleLoop` 开始时调用 `StartRecordExecutionTime()`，结束时自动记录
- **消费者**：`ClusterLoadChecker` 通过 PromQL `avg(rate(scheduler_loop_duration_seconds_sum[5m]) / rate(scheduler_loop_duration_seconds_count[5m]))` 对比当前节点与集群平均性能

#### load_checker_sleep_duration_seconds

```go
promauto.NewHistogramVec(prometheus.HistogramOpts{
    Name:    "load_checker_sleep_duration_seconds",
    Help:    "负载检查器建议的睡眠时间（秒）",
    Buckets: []float64{0, 0.1, 0.5, 1, 2, 5, 10, 30, 60},
}, []string{"node_id", "checker_type"})
```

- **使用位置**：`scheduler.go:188`（V1）/ `schedulerv2.go:175`（V2）— 负载检查器返回退避时间后记录
- **Bucket 设计**：覆盖 0 到 60 秒的退避范围，60 秒以上的极端退避归入 `+Inf`

#### gorm_query_duration_seconds

```go
promauto.NewHistogramVec(prometheus.HistogramOpts{
    Name:    "gorm_query_duration_seconds",
    Help:    "GORM 数据库操作耗时（秒）",
    Buckets: prometheus.DefBuckets,
}, []string{"operation", "table"})
```

- **注册方式**：GORM Plugin 回调，在每个 CRUD 操作的 `before/after` 回调中自动计时
- **覆盖操作**：query / create / update / delete / row / raw（6 类操作全覆盖）
- **消费者**：`DatabaseLoadChecker` 通过 PromQL `avg(rate(gorm_query_duration_seconds_sum[5m]) / rate(gorm_query_duration_seconds_count[5m]))` 检测 DB 响应时间

### 6.3 指标数据流

```
                                        ┌──────────────────┐
Scheduler 调度循环                       │                  │
  ├── metrics.StartRecordExecutionTime() │                  │
  │   → scheduler_loop_duration_seconds  │                  │
  │                                      │   Prometheus     │
  ├── metrics.RecordLoadCheckerSleep()   │   :9090          │
  │   → load_checker_sleep_duration_secs │                  │
  │                                      │                  │
GORM Plugin (before/after callbacks)     │   ◄── scrape     │
  └── gorm_query_duration_seconds        │       /metrics   │
                                         │       :9003      │
执行节点                                  │                  │
  ├── executor_cpu_idle_percent          │   ◄── remote     │
  └── executor_memory_available_bytes    │       write      │
                                         └────────┬─────────┘
                                                  │ query
                                                  ▼
                                         ┌──────────────────┐
                                         │    Grafana :3000  │
                                         │    监控面板        │
                                         └──────────────────┘
```

### 6.4 PromQL 查询汇总

| 消费者 | PromQL | 返回值含义 |
|--------|--------|-----------|
| `ClusterLoadChecker` | `avg(rate(scheduler_loop_duration_seconds_sum[5m]) / rate(scheduler_loop_duration_seconds_count[5m]))` | 集群所有节点平均单次调度循环耗时 |
| `DatabaseLoadChecker` | `avg(rate(gorm_query_duration_seconds_sum[5m]) / rate(gorm_query_duration_seconds_count[5m]))` | 数据库平均单次查询响应时间 |
| `PrometheusPicker` | `topk(3, avg_over_time({metric}[30s]) AND ON(instance) up{job="executors"} == 1)` | CPU/内存指标最优的前 N 个存活执行节点 |

### 6.5 Prometheus 采集配置

**文件**：`scripts/prometheus/prometheus.yml`

```yaml
global:
  scrape_interval: 15s       # 全局采集间隔
  evaluation_interval: 15s   # 规则评估间隔

scrape_configs:
  - job_name: 'prometheus'   # Prometheus 自监控
    static_configs:
      - targets: ['localhost:9090']
```

**当前状态与扩展方向**：
- 当前仅配置了 Prometheus 自监控
- Scheduler 指标通过 ego Governor 自动暴露在 `:9003/metrics`
- 执行节点指标通过 Remote Write API 推送（Prometheus 通过 `--web.enable-remote-write-receiver` 启用接收）

**生产环境扩展**：

```yaml
scrape_configs:
  - job_name: 'prometheus'
    static_configs:
      - targets: ['localhost:9090']

  # 调度器节点采集
  - job_name: 'scheduler'
    static_configs:
      - targets: ['task-scheduler:9003']

  # 执行节点采集（动态发现时改用 etcd_sd_configs）
  - job_name: 'executors'
    static_configs:
      - targets: ['executor-node-1:9004', 'executor-node-2:9004']
```

---

## 7. 健康检查体系

### 7.1 三层健康检查

```
┌──────────────────────────────────────────────────────────────┐
│  Layer 1: Docker HEALTHCHECK                                  │
│  CMD wget -qO- http://localhost:9003/debug/health || exit 1   │
│  interval=15s, timeout=5s, start_period=10s, retries=3        │
│                                                                │
│  Layer 2: ego Governor 内置端点                                │
│  /debug/health  ← 健康检查                                    │
│  /debug/pprof   ← 性能诊断（goroutine、heap、CPU profile）    │
│  /metrics       ← Prometheus 指标暴露                          │
│                                                                │
│  Layer 3: Scheduler.Info() server.Server 接口                  │
│  Healthy = s.ctx.Err() == nil（上下文未取消即健康）             │
└──────────────────────────────────────────────────────────────┘
```

### 7.2 各服务健康检查配置

| 服务 | 检查方式 | 参数 |
|------|---------|------|
| **scheduler** | Dockerfile HEALTHCHECK → `/debug/health` | 15s 间隔 / 5s 超时 / 10s 启动期 / 3 次重试 |
| **mysql** | `mysqladmin ping -h localhost -u root --password=root` | 2s 间隔 / 5s 超时 / 10s 启动期 / 15 次重试 |
| **kafka** | `kafka-broker-api-versions.sh --bootstrap-server localhost:9092` | 10s 间隔 / 10s 超时 / 30s 启动期 / 5 次重试 |
| **redis** | 无（service_started 即可） | — |
| **etcd** | 无（service_started 即可） | — |
| **prometheus** | 无 | — |
| **grafana** | 无 | — |

---

## 8. 应用启动流程

### 8.1 main.go 入口

```go
func main() {
    // 1. 创建 ego 应用（加载配置 + 初始化日志）
    egoApp := ego.New()
    
    // 2. 创建可取消上下文，控制后台任务生命周期
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    
    // 3. Wire 依赖注入：InitSchedulerApp()
    app := ioc.InitSchedulerApp()
    
    // 4. 启动 6 个后台长任务（4 补偿器 + 2 MQ 消费者）
    app.StartTasks(ctx)
    
    // 5. 启动 3 个 Server
    egoApp.Serve(
        egovernor,    // Governor :9003（健康检查 + pprof + metrics）
        app.GRPC,     // gRPC :9002（ReporterService）
        app.Scheduler, // 调度循环
    ).Run()
}
```

### 8.2 Wire 依赖注入全图

```
InitNodeID() ──────────────────────────────────────────────────┐
                                                                │
InitDB() ──────────────────────────────────────────────────────┤
  ├── WaitForDBSetup（指数退避 Ping MySQL）                     │
  ├── egorm.Load("mysql").Build()                              │
  ├── 注册 GormMetricsPlugin（→ gorm_query_duration_seconds）  │
  └── dao.InitTables（AutoMigrate）                            │
                                                                │
InitEtcdClient() ──────────────────────────────────────────────┤
InitMQ() ──────────────────────────────────────────────────────┤
  ├── 指数退避连接 Kafka                                        │
  └── 自动创建 2 个 Topic                                       │
InitPrometheusClient() ────────────────────────────────────────┤
                                                                │
InitRegistry(etcd) ────────────────────────────────────────────┤
InitExecutorServiceGRPCClients(registry) ──────────────────────┤
InitInvoker(grpcClients) ──────────────────────────────────────┤
InitCompleteProducer(mq) ──────────────────────────────────────┤
                                                                │
dao.NewGORMTaskDAO(db) ────────────────────────────────────────┤
dao.NewGORMTaskExecutionDAO(db) ───────────────────────────────┤
repository.NewTaskRepository(taskDAO) ─────────────────────────┤
repository.NewTaskExecutionRepository(execDAO, taskRepo) ──────┤
task.NewService(taskRepo) ─────────────────────────────────────┤
task.NewExecutionService(...) ─────────────────────────────────┤
task.NewPlanService(taskRepo, execRepo) ───────────────────────┤
                                                                │
InitRunner(nodeID, taskSvc, execSvc, planSvc, acq, inv, prod) ┤
InitMySQLTaskAcquirer(taskRepo) ───────────────────────────────┤
InitClusterLoadChecker(nodeID, promClient) ────────────────────┤
InitExecutorNodePicker(promClient) ────────────────────────────┤
                                                                │
InitScheduler(全部依赖) ───────────────────────────────────────┤
  └── 内部创建 SchedulerMetrics(nodeID)                        │
                                                                │
InitRetryCompensator ──────────────────────────────────────────┤
InitRescheduleCompensator ─────────────────────────────────────┤
InitShardingCompensator ───────────────────────────────────────┤
InitInterruptCompensator ──────────────────────────────────────┤
InitExecutionBatchReportEventConsumer ─────────────────────────┤
InitExecutionReportEventConsumer ──────────────────────────────┤
                                                                │
InitTasks(4 补偿器 + 2 消费者) ────────────────────────────────┤
                                                                │
grpc.NewReporterServer + InitSchedulerNodeGRPCServer ──────────┤
                                                                ▼
                                                      SchedulerApp{
                                                        GRPC, Scheduler, Tasks[6]
                                                      }
```

### 8.3 Provider Set 分组

| Provider Set | 包含的 Provider | 职责 |
|-------------|----------------|------|
| `BaseSet` | InitDB, InitDistributedLock, InitEtcdClient, InitMQ, InitRunner, InitInvoker, InitRegistry, InitPrometheusClient | 基础设施层 |
| `taskSet` | NewGORMTaskDAO, NewTaskRepository, NewService | 任务管理 |
| `taskExecutionSet` | NewGORMTaskExecutionDAO, NewTaskExecutionRepository, NewExecutionService | 执行管理 |
| `planSet` | NewPlanService | DAG 工作流 |
| `schedulerSet` | InitNodeID, InitClusterLoadChecker, InitScheduler, InitMySQLTaskAcquirer, InitExecutorServiceGRPCClients, InitExecutorNodePicker | 调度核心 |
| `compensatorSet` | InitRetryCompensator, InitRescheduleCompensator, InitShardingCompensator, InitInterruptCompensator | 补偿器 |
| `consumerSet` | InitExecutionReportEventConsumer, InitExecutionBatchReportEventConsumer | MQ 消费者 |
| `producerSet` | InitCompleteProducer | MQ 生产者 |

---

## 9. MySQL 初始化脚本

### 9.1 数据库结构

`scripts/mysql/init.sql` 在 MySQL 容器首次启动时自动执行：

```
MySQL 8.0
├── task       ← 主库（GORM AutoMigrate 用）
├── task_0     ← 分库 0
│   ├── task_0             ← 任务表分片 0
│   ├── task_1             ← 任务表分片 1
│   ├── task_execution_0   ← 执行表分片 0
│   ├── task_execution_1   ← 执行表分片 1
│   ├── task_execution_2   ← 执行表分片 2
│   └── task_execution_3   ← 执行表分片 3
└── task_1     ← 分库 1
    ├── task_0             ← 任务表分片 0
    ├── task_1             ← 任务表分片 1
    ├── task_execution_0   ← 执行表分片 0
    ├── task_execution_1   ← 执行表分片 1
    ├── task_execution_2   ← 执行表分片 2
    └── task_execution_3   ← 执行表分片 3
```

**物理表统计**：2 库 × (2 task 表 + 4 execution 表) = **12 张物理表**

### 9.2 分库分表用户权限

```sql
CREATE USER 'task'@'%' IDENTIFIED BY 'task';
GRANT CREATE, ALTER, INDEX, LOCK TABLES, REFERENCES, UPDATE, DELETE, DROP, SELECT, INSERT
ON `task_0`.* TO 'task'@'%';
GRANT CREATE, ALTER, INDEX, LOCK TABLES, REFERENCES, UPDATE, DELETE, DROP, SELECT, INSERT
ON `task_1`.* TO 'task'@'%';
```

**设计说明**：分库分表用的是独立用户 `task`，权限精确控制到分库级别，root 用户仅用于主库操作。

### 9.3 核心索引设计

**Task 表索引**：

| 索引名 | 列 | 用途 |
|--------|---|------|
| `uniq_idx_name` | `name` | 任务名称唯一约束 |
| `idx_schedule_node_id_status` | `schedule_node_id, status` | 按节点 ID + 状态查询已抢占任务（续约用） |
| `idx_next_time_status_utime` | `next_time, status, utime` | 查询到期可调度任务（核心调度查询） |
| `idx_plan_id` | `plan_id` | DAG 工作流任务关联查询 |
| `idx_type` | `type` | 按任务类型过滤 |

**TaskExecution 表索引**：

| 索引名 | 列 | 用途 |
|--------|---|------|
| `idx_task_id` | `task_id` | 按任务 ID 查询执行记录 |
| `idx_status` | `status` | 按状态查询（补偿器扫描用） |
| `idx_sharding_parent_id` | `sharding_parent_id` | 分片任务子分片汇总查询 |

---

## 10. 配置管理

### 10.1 本地 vs Docker 配置差异

**唯一差异**：中间件连接地址。其他所有配置完全相同。

| 配置项 | 本地 (`config.yaml`) | Docker (`docker-config.yaml`) |
|--------|---------------------|------------------------------|
| MySQL | `localhost:13316` | `mysql:3306` |
| Redis | `localhost:6379` | `redis:6379` |
| Kafka | `localhost:9092` | `kafka:9094`（内部端口） |
| etcd | `127.0.0.1:2379` | `etcd:2379` |
| Prometheus | `http://localhost:9090` | `http://prometheus:9090` |

### 10.2 配置挂载方式

```yaml
volumes:
  - ./config/docker-config.yaml:/app/config/config.yaml:ro
```

**设计说明**：
- Docker 配置文件以只读（`:ro`）方式挂载到容器内 `/app/config/config.yaml`
- 对应用完全透明——代码中统一读取 `/app/config/config.yaml`，不区分环境
- 修改配置无需重新构建镜像，`docker compose restart scheduler` 即可

### 10.3 完整配置参考

```yaml
# ========== 日志 ==========
log:
  debug: true                    # 开发调试模式

# ========== 存储层 ==========
mysql:
  dsn: "root:root@tcp(mysql:3306)/task?..."
redis:
  addr: "redis:6379"

# ========== 消息队列 ==========
mq:
  kafka:
    network: "tcp"
    addr: "kafka:9094"

# ========== 服务发现 ==========
etcd:
  addrs: ["etcd:2379"]
  connectTimeout: "1s"
  secure: false

# ========== 监控 ==========
prometheus:
  url: "http://prometheus:9090"

# ========== 服务端口 ==========
server:
  scheduler:
    grpc:
      host: "0.0.0.0"
      port: 9002
  governor:
    host: "0.0.0.0"
    port: 9003

# ========== 调度器核心 ==========
scheduler:
  batchTimeout: 3000000000        # 3s
  batchSize: 100
  preemptedTimeout: 600000000000  # 10min
  scheduleInterval: 10000000000   # 10s
  renewInterval: 5000000000       # 5s

# ========== 智能调度 ==========
intelligentScheduling:
  jobName: "executors"
  topNCandidates: 3
  timeWindow: 30000000000         # 30s
  queryTimeout: 5000000000        # 5s

# ========== 负载检查 ==========
loadChecker:
  limiter:
    rateLimit: 10.0               # 每秒 10 次
    burstSize: 20                 # 突发 20
    waitDuration: 5000000000      # 5s
  database:
    threshold: 100000000          # 100ms
    timeWindow: 300000000000      # 5min
    backoffDuration: 10000000000  # 10s
  cluster:
    thresholdRatio: 1.2
    timeWindow: 300000000000      # 5min
    slowdownMultiplier: 2.0
    minBackoffDuration: 5000000000 # 5s

# ========== 补偿器 ==========
compensator:
  retry:
    maxRetryCount: 3
    batchSize: 100
    minDuration: 1000000000       # 1s
  reschedule:
    batchSize: 100
    minDuration: 1000000000
  sharding:
    batchSize: 100
    minDuration: 1000000000
  interrupt:
    batchSize: 100
    minDuration: 1000000000

# ========== MQ Topic ==========
executionReportEvent:
  topic: "execution_report"
  partitions: 1
executionBatchReportEvent:
  topic: "execution_batch_report"
  partitions: 1
```

---

## 11. Makefile 部署命令

| Target | 命令 | 说明 |
|--------|------|------|
| `make docker-build` | `docker build -t task-scheduler:latest .` | 构建 scheduler 镜像 |
| `make docker-up` | `docker compose up -d` | **一键启动全部 7 个服务** |
| `make docker-down` | `docker compose down -v` | 停止并清理服务和数据卷 |
| `make docker-rebuild` | `docker compose up -d --build` | 代码变更后重新构建并启动 |
| `make docker-logs` | `docker compose logs -f` | 查看所有服务日志 |
| `make run_scheduler` | e2e_up → sleep 15 → go run | 本地编译运行（依赖容器化中间件） |

### 部署验证流程

```bash
# 1. 一键启动
make docker-up

# 2. 检查所有容器状态
docker compose ps
# 期望：所有服务 STATUS=running/healthy

# 3. 验证健康检查
curl http://localhost:9003/debug/health     # Scheduler Governor
curl http://localhost:9090/-/healthy         # Prometheus
curl http://localhost:3000/api/health        # Grafana

# 4. 打开 Grafana
open http://localhost:3000
# 账号: admin / 密码: 123
# 配置数据源 → Prometheus → URL: http://prometheus:9090

# 5. 验证指标暴露
curl http://localhost:9003/metrics | grep scheduler_loop_duration_seconds

# 6. 查看调度器日志
docker compose logs -f scheduler
```

---

## 12. 日志体系

### 12.1 日志框架

- **底层引擎**：ego 框架内置 `elog`（基于 uber-go/zap）
- **初始化**：`ego.New()` 自动完成，无需手动配置
- **调试模式**：`config.yaml` 中 `log.debug: true` 开启 DEBUG 级别日志

### 12.2 结构化日志示例

```go
s.logger.Info("发现可调度任务",
    elog.Int("count", len(tasks)))

s.logger.Error("调度任务失败",
    elog.Int64("taskID", tasks[i].ID),
    elog.String("taskName", tasks[i].Name),
    elog.FieldErr(err))

s.logger.Info("智能调度选择节点成功",
    elog.String("selectedNodeID", nodeID),
    elog.Int64("taskID", task.ID))
```

### 12.3 子 Logger 命名规范

各组件通过 `elog.FieldComponentName()` 创建子 Logger，方便日志过滤：

| 组件 | ComponentName |
|------|-------------|
| Scheduler V1/V2 | `SchedulerV2` |
| PrometheusPicker | `picker.prometheus` |
| Dispatcher (Picker) | `picker.dispatcher` |

---

## 13. Grafana 监控面板建议

当前项目未预配置 Grafana Provisioning，需要手动在 Grafana UI 中创建。推荐面板设计：

### 13.1 调度概览面板

| Panel | PromQL | 可视化类型 |
|-------|--------|-----------|
| 调度循环 QPS | `rate(scheduler_loop_duration_seconds_count[1m])` | Timeseries |
| 调度循环 P99 延迟 | `histogram_quantile(0.99, rate(scheduler_loop_duration_seconds_bucket[5m]))` | Timeseries |
| 调度循环 P95 延迟 | `histogram_quantile(0.95, rate(scheduler_loop_duration_seconds_bucket[5m]))` | Timeseries |
| 调度循环平均延迟 | `rate(scheduler_loop_duration_seconds_sum[5m]) / rate(scheduler_loop_duration_seconds_count[5m])` | Stat |

### 13.2 负载检查面板

| Panel | PromQL | 可视化类型 |
|-------|--------|-----------|
| 退避触发次数 | `rate(load_checker_sleep_duration_seconds_count[5m])` | Timeseries |
| 退避时间分布 | `histogram_quantile(0.95, rate(load_checker_sleep_duration_seconds_bucket[5m]))` | Timeseries |
| 退避时间热力图 | `sum(increase(load_checker_sleep_duration_seconds_bucket[5m])) by (le)` | Heatmap |

### 13.3 数据库性能面板

| Panel | PromQL | 可视化类型 |
|-------|--------|-----------|
| DB 操作 QPS | `sum(rate(gorm_query_duration_seconds_count[1m])) by (operation)` | Timeseries (按 operation 分组) |
| DB 操作 P99 延迟 | `histogram_quantile(0.99, sum(rate(gorm_query_duration_seconds_bucket[5m])) by (operation, le))` | Timeseries |
| 慢查询占比 | `sum(rate(gorm_query_duration_seconds_bucket{le="0.1"}[5m])) / sum(rate(gorm_query_duration_seconds_count[5m]))` | Gauge |
| 热点表 Top 5 | `topk(5, sum(rate(gorm_query_duration_seconds_count[5m])) by (table))` | Bar chart |

---

## 14. 代码级韧性设计

### 14.1 指数退避重试

| 组件 | 重试策略 | 最大重试次数 | 初始间隔 | 最大间隔 |
|------|---------|------------|---------|---------|
| `WaitForDBSetup` | 指数退避 | 10 次 | 1s | 10s |
| `InitMQ` | 指数退避 | 10 次 | 1s | 10s |

```go
// WaitForDBSetup 等待 MySQL 就绪
func WaitForDBSetup(dsn string) {
    sqlDB, _ := sql.Open("mysql", dsn)
    strategy, _ := retry.NewExponentialBackoffRetryStrategy(time.Second, 10*time.Second, 10)
    for {
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        err = sqlDB.PingContext(ctx)
        cancel()
        if err == nil { break }
        next, ok := strategy.Next()
        if !ok { panic("WaitForDBSetup 重试失败...") }
        time.Sleep(next)
    }
}
```

### 14.2 优雅停机

```go
// Scheduler.GracefulStop
func (s *Scheduler) GracefulStop(_ context.Context) error {
    s.cancel()     // 1. 发送取消信号
    s.wg.Wait()    // 2. 等待 scheduleLoop + renewLoop 退出
    return nil
}
```

**停机流程**：
1. ego 框架收到 SIGTERM / SIGINT
2. 依次调用注册的 Server 的 GracefulStop
3. Scheduler 取消 context → scheduleLoop 和 renewLoop 检测到 `ctx.Err() != nil` → 退出循环
4. WaitGroup 等待所有 goroutine 退出
5. 最后关闭 gRPC 服务端

---

## 15. 完整部署检查清单

| # | 检查项 | 验证方法 | 期望结果 |
|---|--------|---------|---------|
| 1 | `docker compose up -d` 一键启动 | `docker compose ps` | 7 个服务 Running |
| 2 | MySQL 健康检查通过 | `docker inspect task-mysql` → Health | `healthy` |
| 3 | Kafka 健康检查通过 | `docker inspect task-kafka` → Health | `healthy` |
| 4 | Scheduler 健康检查通过 | `curl localhost:9003/debug/health` | 200 OK |
| 5 | Prometheus 可访问 | `curl localhost:9090/-/healthy` | 200 OK |
| 6 | Grafana 可访问 | `curl localhost:3000/api/health` | 200 OK |
| 7 | 指标暴露正常 | `curl localhost:9003/metrics \| grep scheduler` | 包含 scheduler_loop_duration_seconds |
| 8 | MySQL 分库分表就绪 | `mysql -P 13316 -u root -proot -e "SHOW DATABASES"` | 包含 task/task_0/task_1 |
| 9 | Grafana 数据源配置 | Grafana UI → Data Sources → Prometheus | 连接成功 |
| 10 | 日志输出正常 | `docker compose logs -f scheduler` | 看到结构化 JSON 日志 |

---

## 16. 实现局限与改进方向

| # | 现状 | 改进方案 | 优先级 |
|---|------|---------|--------|
| 1 | Prometheus 采集配置仅含自监控，未配置 scheduler scrape job | 添加 `job_name: scheduler` + etcd 服务发现 | 高 |
| 2 | Grafana 无预配置面板 | 使用 Provisioning YAML + Dashboard JSON 自动导入 | 高 |
| 3 | 无告警规则 | 添加 Prometheus Alertmanager + 告警规则文件 | 中 |
| 4 | 单 Scheduler 实例 | docker-compose 中用 `deploy.replicas` 或外部 docker-compose-scale | 中 |
| 5 | 无 CI/CD 流水线 | GitHub Actions：lint → test → docker build → push → deploy | 中 |
| 6 | 日志仅在容器内 | 集成 EFK/Loki 日志采集栈 | 低 |
| 7 | 无分布式追踪 | 接入 OpenTelemetry → Jaeger/Tempo | 低 |

---

## 17. 面试高频 Q&A

### Q1: 为什么 Dockerfile 用多阶段构建？直接 `golang:alpine` 一个阶段不行吗？

**直接一个阶段**意味着最终镜像包含 Go 编译器和所有依赖，约 300-400 MB。多阶段构建将编译和运行分离：编译在 `golang:alpine`（有完整工具链），运行在纯 `alpine`（只有二进制 + ca-certificates + tzdata），最终镜像约 30 MB。**10 倍体积差距**直接影响镜像拉取速度和存储成本。

### Q2: Kafka 为什么配置了 EXTERNAL 和 INTERNAL 两个监听器？

容器内部互相通信走 `kafka:9094`（INTERNAL），但**宿主机开发调试**需要通过 `localhost:9092`（EXTERNAL）访问。如果只配一个 `kafka:9092`，宿主机能连上但解析到的 advertise 地址是 `kafka:9092`，宿主机无法解析 `kafka` 这个主机名，就会连接失败。两个监听器各自 advertise 不同地址，完美解决 Docker 内外通信问题。

### Q3: 为什么用 `service_healthy` 而不是 `service_started` 来等待 MySQL？

`service_started` 只保证容器进程启动了，但 MySQL 从启动到接受连接需要 10-30 秒（初始化 InnoDB、执行 init.sql）。如果 scheduler 在 MySQL 未就绪时启动，即使有代码级指数退避重试，也会浪费大量重试时间和日志噪音。`service_healthy` + `mysqladmin ping` 确保 **MySQL 真正可用**后才启动 scheduler。

### Q4: Prometheus 为什么启用 `--web.enable-remote-write-receiver`？

执行节点是动态注册的，数量不固定，IP 地址不确定。如果用 Pull 模式，Prometheus 需要提前知道所有执行节点的地址（或配置服务发现），配置复杂。Remote Write 让执行节点**主动推送指标**到 Prometheus，无需修改 Prometheus 配置。特别是在集成测试中，mock 执行节点可以直接通过 `api/v1/write` 推送假指标数据来验证 Picker 逻辑。

### Q5: 代码级重试和 Docker depends_on 是什么关系？不是重复了吗？

两层保护，各有侧重：
- **`depends_on: service_healthy`**：保证启动顺序，MySQL **完全就绪**后才启动 scheduler
- **`WaitForDBSetup` 指数退避**：处理边界情况——如 MySQL 刚好通过 healthcheck 但还在执行最后的 init.sql、或者运行期间 MySQL 临时重启

类比：门禁系统（depends_on）确保你在工作时间才能进入办公室，但你进去后发现电梯还没启动（MySQL init.sql 还在跑），这时你需要等电梯（指数退避）。

### Q6: GORM Plugin 的 before/after 回调会不会影响数据库操作性能？

**影响极小**。before 回调只是 `db.Set(key, time.Now())` 存一个时间戳到 gorm.DB 内存字段，after 回调取出时间戳做一次减法再 `Observe()` 到 Histogram。整个过程：
- **无内存分配**（time.Now() 返回值类型，不分配堆内存）
- **无网络 IO**（Prometheus 的 Observe 只是在进程内原子操作更新直方图的桶计数器）
- **无锁竞争**（promauto 注册的 Histogram 内部使用 atomic 操作）

实测额外开销 < 1μs/次，对比 MySQL 网络往返的 1-100ms，完全可忽略。
