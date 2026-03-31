# ⚡ Distributed Task Platform

**企业级分布式任务调度平台** — 支持 DAG 工作流编排、智能调度、分片任务、四重补偿的高可用任务调度中台。

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![gRPC](https://img.shields.io/badge/gRPC-Protobuf-244c5a?logo=grpc)](https://grpc.io/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

[English](README.en.md)

---

## 目录

- [架构概览](#架构概览)
- [核心特性](#核心特性)
- [技术栈](#技术栈)
- [快速开始](#快速开始)
- [项目结构](#项目结构)
- [核心概念](#核心概念)
- [配置说明](#配置说明)
- [示例](#示例)
- [文档](#文档)
- [开发指南](#开发指南)

---

## 架构概览

```
┌──────────────────────────────────────────────────────────────┐
│                        接入层                                 │
│              REST API  /  gRPC SDK  /  CLI                   │
└────────────────────────────┬─────────────────────────────────┘
                             │
         ┌───────────────────┼───────────────────┐
         │                   │                   │
    ┌────▼─────┐       ┌────▼─────┐       ┌────▼─────┐
    │Scheduler │       │Scheduler │       │Scheduler │
    │   Node 1 │       │   Node 2 │       │   Node N │
    └────┬─────┘       └────┬─────┘       └────┬─────┘
         │  CAS 乐观锁抢占   │  Prometheus 智能选节点  │
         └─────────┬─────────┴─────────────────────────┘
                   │ gRPC 双向通信
     ┌─────────────┼─────────────────┐
     │             │                 │
┌────▼────┐  ┌────▼────┐     ┌──────▼──────┐
│ Executor│  │ Executor│     │  Executor   │
│ (Spark) │  │ (Flink) │ ... │  (DataX)    │
└─────────┘  └─────────┘     └─────────────┘
                   │
     ┌─────────────┼─────────────┐
     │             │             │
┌────▼───┐  ┌─────▼────┐  ┌────▼────┐
│ MySQL  │  │  Kafka   │  │  etcd   │
│ (分片)  │  │ (事件流)  │  │ (注册)  │
└────────┘  └──────────┘  └─────────┘
```

**调度器（Scheduler）** 通过 CAS 乐观锁抢占任务，基于 Prometheus 指标智能选择最优执行节点，经 gRPC 下发任务到 **执行器（Executor）** 集群。执行器完成任务后通过 Kafka 异步上报状态，形成完整的调度闭环。

---

## 核心特性

### 🧠 智能调度

- **Prometheus 驱动** — 实时查询 CPU、内存、任务数等指标，多维度评分选节点
- **TopN 随机算法** — 从前 N 个最优节点中随机选择，避免羊群效应
- **负载自适应** — 慢节点自动降频，快节点保持高频，保护集群稳定性
- **三级防护** — 令牌桶限流 → 数据库响应检查 → 集群负载对比

### 🔄 DAG 工作流引擎

基于 ANTLR4 的声明式 DSL，支持复杂任务编排：

```
# 并行采集 → 清洗 → 并行转换 → 聚合 → 条件分支
(order_sync && user_sync && product_sync)
-> data_cleaning
-> (order_transform && user_transform)
-> data_aggregate
-> quality_check
? generate_report
: alert_notification
```

| 语法 | 含义 | 示例 |
|------|------|------|
| `->` | 顺序执行 | `A -> B -> C` |
| `&&` | 并行执行（全部成功才继续） | `A && B && C -> D` |
| `\|\|` | 选择执行（任一成功即继续） | `A \|\| B -> C` |
| `? :` | 条件分支 | `A ? B : C` |

### 📊 分片任务

支持将大任务拆分为多个子任务并行执行：

- **Range 分片** — 按固定步长划分数据范围，适合等量数据处理
- **Weighted Dynamic Range** — 按节点权重动态分配，高性能节点处理更多数据

### 🛡️ 四重补偿机制

| 补偿器 | 触发场景 | 处理策略 |
|--------|---------|---------|
| **重试** | 网络抖动、临时错误 | 指数退避重试，可配置最大次数 |
| **重调度** | 执行器宕机 | 排除故障节点，选择健康节点重新调度 |
| **中断** | 任务超时 | 向执行器发送中断信号，释放资源 |
| **分片聚合** | 子任务完成 | 聚合所有子任务状态，决定父任务最终结果 |

### 🔒 并发控制

- **乐观锁（CAS）** — 任务抢占场景，`UPDATE ... SET version = version + 1 WHERE version = ?`
- **分布式锁（Redis/etcd）** — 补偿器互斥场景，防止多节点重复处理

### 📡 服务发现与路由

- **etcd 注册中心** — Lease + Watch 机制，秒级节点感知，60s 故障自动注销
- **自定义 gRPC 负载均衡器** — 支持排除节点（故障转移）和指定路由（任务转移）

### 💾 分库分表

- MySQL 分片：2 库 × 2 张 task 表 + 2 库 × 4 张 task_execution 表
- Snowflake 变体 ID 生成器，保证全局唯一 + 分片路由
- `ShardingLoopJob` 并发扫描所有分片，补偿器覆盖全量数据

### 📈 可观测性

- **Prometheus** — 调度延迟、执行成功率、节点健康、数据库连接池等 100+ 指标
- **Grafana** — 预配置监控面板
- **OpenTelemetry** — 全链路分布式追踪

---

## 技术栈

| 类别 | 技术 | 用途 |
|------|------|------|
| 语言 | Go 1.24+ | 主开发语言 |
| 框架 | [ego](https://github.com/gotomicro/ego) | 微服务框架 |
| 通信 | gRPC + Protobuf | 调度器 ⇄ 执行器通信 |
| 服务发现 | etcd | 注册中心 + 分布式锁 |
| 消息队列 | Kafka (KRaft) | 异步事件（状态上报） |
| 存储 | MySQL 8.0（分库分表） | 任务 + 执行记录 |
| 缓存/锁 | Redis | 分布式锁 + 缓存 |
| 监控 | Prometheus + Grafana | 指标采集 + 可视化 |
| DSL 引擎 | ANTLR4 | DAG 工作流解析 |
| DI | Wire | 编译时依赖注入 |

---

## 快速开始

### 前置条件

- Go 1.24+
- Docker & Docker Compose

### 一键启动（Docker）

```bash
# 1. 构建镜像
make docker-build

# 2. 启动全部服务（Scheduler + MySQL + Redis + Kafka + etcd + Prometheus + Grafana）
make docker-up

# 3. 查看服务状态
docker compose ps

# 4. 查看调度器日志
docker compose logs -f scheduler
```

启动后可访问：

| 服务 | 地址 | 说明 |
|------|------|------|
| Scheduler gRPC | `localhost:9002` | 执行器连接此端口 |
| Governor | `localhost:9003` | 健康检查 + pprof |
| Prometheus | `localhost:9090` | 监控指标 |
| Grafana | `localhost:3000` | 监控面板（admin/123） |

### 本地开发

```bash
# 1. 启动依赖中间件
make e2e_up

# 2. 等待服务就绪后启动调度器
make run_scheduler_only
```

### 停止服务

```bash
# Docker 方式
make docker-down

# 本地开发方式
make e2e_down
```

---

## 项目结构

```
distributed_task_platform/
├── api/                        # gRPC + Protobuf 接口定义
│   └── proto/
│       ├── executor/v1/        #   ExecutorService（Execute/Interrupt/Query/Prepare）
│       └── reporter/v1/        #   ReporterService（Report/BatchReport）
├── cmd/
│   └── scheduler/              # 调度器启动入口
├── config/                     # 配置文件（本地 + Docker）
├── example/                    # 使用示例（长任务 / 分片任务 / gRPC）
├── internal/
│   ├── compensator/            # 四重补偿器（Retry/Reschedule/Interrupt/Sharding × V1/V2）
│   ├── domain/                 # 领域模型（Task/TaskExecution/Plan/PlanTask/Report）
│   ├── dsl/parser/             # ANTLR4 DSL 解析器（词法→语法→AST→执行计划）
│   ├── errs/                   # 错误定义
│   ├── event/                  # 事件系统（Kafka Producer/Consumer）
│   ├── grpc/                   # gRPC 服务端（ReporterServer）
│   ├── repository/             # 数据访问层
│   │   └── dao/                #   GORM DAO（分库分表路由）
│   ├── scheduler/              # 调度引擎（V1 简单循环 / V2 分布式）
│   └── service/
│       ├── invoker/            #   任务下发器（gRPC/HTTP/Local）
│       ├── picker/             #   节点选择器（Prometheus/Routing）
│       ├── runner/             #   任务运行器（Normal/Plan）
│       └── task/               #   任务服务 + 执行服务
├── ioc/                        # Wire 依赖注入配置
├── pkg/                        # 公共组件库
│   ├── grpc/                   #   gRPC 客户端池 + 自定义负载均衡器 + etcd 服务发现
│   ├── id_generator/           #   Snowflake ID 生成器
│   ├── loadchecker/            #   三级负载检查器（限流/数据库/集群）
│   ├── loopjob/                #   分布式循环任务（ShardingLoopJob）
│   ├── mqx/                    #   Kafka 消费者封装
│   ├── prometheus/             #   Prometheus 指标定义
│   ├── retry/                  #   重试策略（指数退避/固定间隔）
│   └── sharding/               #   分库分表路由策略
├── scripts/                    # 部署脚本 + SQL 初始化 + Prometheus 配置
├── docker-compose.yml          # 完整服务编排
├── Dockerfile                  # 多阶段构建
└── Makefile                    # 开发命令集
```

---

## 核心概念

### Task（任务）

任务是调度的基本单元。每个任务定义了 Cron 表达式、执行方式、调度策略等。

```
任务状态流转：
  ACTIVE（可调度）→ PREEMPTED（已被调度器抢占）→ 执行完成 → ACTIVE
                                                    ↓
                                              INACTIVE（手动停止）
```

### TaskExecution（执行记录）

每次调度产生一条执行记录，记录完整的执行快照。

```
执行状态流转：
  PREPARE → RUNNING → SUCCESS
                    → FAILED（不可恢复）
                    → FAILED_RETRYABLE → 重试补偿器 → PREPARE
                    → FAILED_RESCHEDULED → 重调度补偿器 → PREPARE
```

### Plan（DAG 工作流）

通过 DSL 表达式定义多任务之间的依赖关系，运行时解析为 DAG 图按拓扑序执行。

### gRPC 服务协议

| 服务 | 方法 | 方向 | 说明 |
|------|------|------|------|
| `ExecutorService` | `Execute` | Scheduler → Executor | 下发任务到执行器 |
| `ExecutorService` | `Interrupt` | Scheduler → Executor | 中断超时任务 |
| `ExecutorService` | `Query` | Scheduler → Executor | 轮询执行状态 |
| `ExecutorService` | `Prepare` | Scheduler → Executor | 查询业务总数量（分片用） |
| `ReporterService` | `Report` | Executor → Scheduler | 单个状态上报 |
| `ReporterService` | `BatchReport` | Executor → Scheduler | 批量状态上报 |

---

## 配置说明

主要配置项（`config/config.yaml`）：

```yaml
# 调度器核心参数
scheduler:
  batchSize: 100              # 批量抢占任务数
  preemptedTimeout: 10m       # 抢占超时时间
  scheduleInterval: 10s       # 调度间隔
  renewInterval: 5s           # 续约间隔
  maxConcurrentTasks: 1000    # 最大并发任务数

# 智能调度
intelligentScheduling:
  topNCandidates: 3           # TopN 候选节点数
  timeWindow: 30s             # 指标查询窗口

# 负载检查器
loadChecker:
  limiter:
    rateLimit: 10.0           # 令牌桶 QPS
    burstSize: 20             # 突发容量
  database:
    threshold: 100ms          # 数据库响应阈值
  cluster:
    thresholdRatio: 1.2       # 超过均值 20% 触发降频

# 补偿器
compensator:
  retry:
    maxRetryCount: 3          # 最大重试次数
    batchSize: 100            # 批量处理数
```

---

## 示例

### 长任务示例

```bash
# 1. 启动调度器
make run_scheduler

# 2. 启动 Executor（长任务执行器）
cd example/longrunning && go run main.go

# 3. 添加任务并等待调度
go test -run TestStart ./example/longrunning/
```

### 分片任务示例

```bash
# 1. 启动调度器
make run_scheduler

# 2. 启动 Executor（分片任务执行器）
cd example/sharding && go run main.go

# 3. 添加分片任务并等待调度
go test -run TestStart ./example/sharding/
```

---

## 文档

| 文档 | 说明 |
|------|------|
| [项目概览](docs/01-项目概览.md) | 架构设计、应用场景、性能指标 |
| [核心技术亮点](docs/02-核心技术亮点.md) | 十大核心技术深度剖析 |
| [性能优化](docs/03-性能优化.md) | 性能调优方案与实践 |
| [开源方案对比](docs/04-开源方案对比.md) | 与 DolphinScheduler / Airflow / XXL-Job 对比 |
| [面试指南](docs/05-面试指南.md) | 面试重点与回答思路 |
| [架构优化路线](docs/06-架构优化路线.md) | 未来演进方向 |
| [详细设计方案](docs/07-详细设计方案.md) | 12 章节全面设计文档 |
| [优化方案汇总](docs/08-优化方案汇总.md) | 18 个优化点及代码示例 |
| [面试 100 问](docs/09-面试问题100问.md) | 11 维度 100 道深度面试题 |
| [压测方案](docs/压测方案.md) | 3000+ 行全链路压测文档 |

---

## 🚢 部署到远端服务器

```bash
# rsync 推送（自动排除不需要的文件）
rsync -avz \
    --exclude='.git/' \
    --exclude='.github/' \
    --exclude='__pycache__/' \
    --exclude='.workbuddy' \
    --exclude='vendor' \
    --exclude='python/.venv' \
    /Users/ziwh666/GitHub/distributed_task_platform \
    root@182.43.22.165:/data/github/

# 服务器上拉取最新代码
git fetch origin && git reset --hard origin/master
```

> 💡 **免密推送**：建议先配置 SSH 密钥认证，执行一次 `ssh-copy-id root@your-server-ip` 即可免密。

---

## 开发指南

```bash
# 代码格式化
make fmt

# 依赖整理
make tidy

# 格式化 + 依赖整理
make check

# 代码规范检查
make lint

# 单元测试
make ut

# 集成测试（自动启停 Docker）
make e2e

# 基准测试
make bench

# 生成 gRPC 代码
make grpc

# 生成 Go 代码（Wire 等）
make gen
```

---

## License

MIT
