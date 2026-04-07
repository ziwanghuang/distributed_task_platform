# Step 15：可观测性增强 — RED 指标 + 告警规则 + OpenTelemetry 链路追踪

> **目标**：将可观测性从"能看到 3 个指标"升级到"生产级可诊断"——补充完整的 RED 指标体系、配置分级告警规则、接入 OpenTelemetry 分布式链路追踪，实现 Metrics + Alerting + Tracing 三位一体。
>
> **完成后你能看到**：Grafana 大盘展示 10+ 业务指标 → Prometheus AlertManager 推送告警 → Jaeger UI 展示调度器 → gRPC → 执行器 → DB 的完整链路追踪 → 任何一个慢任务都能从 trace 中定位到具体瓶颈。

---

## 1. 架构总览

Step 15 在 Step 9 的可观测性基础上做三个维度的增强：

```
┌─────────────────────────────────────────────────────────────────────────────────────┐
│                              可观测性增强架构                                        │
│                                                                                     │
│  ┌─────────────────────────────────────────────────────────────────────────────────┐│
│  │                        Scheduler 应用层                                         ││
│  │                                                                                 ││
│  │  ┌──────────────────┐  ┌──────────────────┐  ┌──────────────────────────────┐  ││
│  │  │ RED Metrics       │  │ OTel TracerProvider│  │ 告警指标采集                 │  ││
│  │  │                   │  │                    │  │                              │  ││
│  │  │ task_schedule_    │  │ gRPC Interceptor   │  │ task_queue_depth            │  ││
│  │  │   total           │  │ GORM OTel Plugin   │  │ compensator_scan_total      │  ││
│  │  │ task_execution_   │  │ Kafka Propagation  │  │ grpc_client_connection_     │  ││
│  │  │   duration        │  │ ScheduleLoop Span  │  │   count                     │  ││
│  │  │ task_queue_depth  │  │                    │  │                              │  ││
│  │  │ task_retry_total  │  │                    │  │                              │  ││
│  │  │ dag_step_duration │  │                    │  │                              │  ││
│  │  └────────┬─────────┘  └────────┬───────────┘  └──────────────┬───────────────┘  ││
│  └───────────┼─────────────────────┼────────────────────────────┼──────────────────┘│
│              │                     │                            │                    │
│              ▼                     ▼                            ▼                    │
│  ┌──────────────────┐  ┌──────────────────┐  ┌──────────────────────────────────┐  │
│  │ Prometheus :9090  │  │ Jaeger :16686    │  │ Alertmanager :9093               │  │
│  │                   │  │                  │  │                                    │  │
│  │  scrape /metrics  │  │  OTLP Receiver   │  │  ScheduleLoopSlow               │  │
│  │  alerting_rules   │  │  Trace Storage   │  │  TaskQueueBacklog               │  │
│  │                   │  │  Trace Query UI  │  │  CompensatorHighRetryRate        │  │
│  └────────┬──────────┘  └──────────────────┘  │  NodeDown                        │  │
│           │                                    └──────────────────────────────────┘  │
│           ▼                                                                          │
│  ┌──────────────────┐                                                                │
│  │ Grafana :3000     │                                                                │
│  │  RED Dashboard    │                                                                │
│  │  Alert Panel      │                                                                │
│  │  Jaeger Datasource│                                                                │
│  └──────────────────┘                                                                │
└─────────────────────────────────────────────────────────────────────────────────────┘
```

**关键设计点**：
- **RED 指标**补充到 10+ 个，覆盖业务层（任务调度）和基础设施层（gRPC/补偿器）
- **告警规则**分三级：critical（立即处理）、warning（关注）、info（日常巡检）
- **OpenTelemetry** 统一 tracing 接入，gRPC/GORM/Kafka 各层自动注入 span，Jaeger 存储和可视化
- 所有新增组件通过 docker-compose 一键启动，零手动配置

---

## 2. Step 9 → Step 15 变更对比

| 维度 | Step 9（基础可观测性） | Step 15（可观测性增强） |
|------|------|------|
| **自定义指标** | 3 个（loopDuration + sleepDuration + gormQuery） | 10+ 个（RED 指标全覆盖） |
| **告警规则** | 无 | 4 条核心规则 + 3 级分级 |
| **链路追踪** | 无 | OpenTelemetry + Jaeger 全链路 |
| **Prometheus 配置** | 仅自监控 | scheduler scrape + alerting rules + alertmanager |
| **Docker Compose** | 7 个服务 | 9 个服务（+Jaeger +Alertmanager） |
| **Grafana** | 无预配置 | Provisioning 自动导入 RED Dashboard |
| **新增文件** | — | `red_metrics.go`、`tracing.go`、`alerting_rules.yml`、`alertmanager.yml` |

---

## 3. 设计决策分析

### 3.1 RED 方法论 vs USE 方法论

| 方法论 | 适用场景 | 关注维度 |
|--------|---------|---------|
| **RED**（Rate / Error / Duration） | 面向请求型服务（API、调度器） | 请求速率、错误率、延迟 |
| **USE**（Utilization / Saturation / Errors） | 面向资源型系统（CPU、磁盘、队列） | 使用率、饱和度、错误数 |

**决策**：以 RED 为主，USE 为辅。
- 调度器本质是**请求处理引擎**——每次 scheduleLoop 就是一次"请求"，天然适合 RED
- 基础设施指标（queue_depth、connection_count）采用 USE 方法论补充
- 两者结合，覆盖"请求视角"和"资源视角"

### 3.2 Jaeger vs Tempo vs Zipkin

| 方案 | 优点 | 缺点 | 适合场景 |
|------|------|------|---------|
| **Jaeger**（选用） | 功能完整、UI 好、社区活跃、CNCF 毕业 | 独立部署、资源占用较大 | 中大规模、需要高级查询 |
| **Grafana Tempo** | 与 Grafana 深度集成、低成本存储 | UI 依赖 Grafana、功能较新 | 已有 Grafana 生态 |
| **Zipkin** | 轻量、接入简单 | 功能较少、社区活跃度下降 | 小规模、快速验证 |

**决策**：选 Jaeger All-in-One。
- 面试 showcase 项目，Jaeger All-in-One 一个容器搞定（Collector + Query + UI + 内存存储）
- CNCF 毕业项目，面试提及有加分
- 生产环境可平滑切换到 Jaeger + Elasticsearch/Cassandra 后端

### 3.3 OpenTelemetry SDK vs 直接 Jaeger SDK

| 方案 | 优点 | 缺点 |
|------|------|------|
| **OpenTelemetry SDK**（选用） | 厂商中立、一次接入多后端、未来标准 | 抽象层略多 |
| **Jaeger Client SDK** | 直接对接、无额外抽象 | 已被官方废弃，推荐迁移到 OTel |

**决策**：使用 OpenTelemetry SDK。Jaeger 官方已明确推荐通过 OpenTelemetry SDK 接入，Jaeger Client SDK 处于维护模式。OTel SDK 可以同时输出 traces 到 Jaeger 和 metrics 到 Prometheus，一套接入两个后端。

### 3.4 Alertmanager 独立部署 vs Grafana 内置告警

| 方案 | 优点 | 缺点 |
|------|------|------|
| **Prometheus Alertmanager**（选用） | 成熟稳定、与 Prometheus 原生集成 | 多一个组件 |
| **Grafana Alerting** | 无需额外组件、UI 友好 | 规则管理不如 YAML 灵活 |

**决策**：Prometheus Alertmanager。原因：
1. 告警规则用 YAML 管理，可 Git 版本控制
2. 支持告警分组、抑制、静默等高级功能
3. 可对接多种通知渠道（Webhook、邮件、PagerDuty 等）

---

## 4. RED 指标体系设计

### 4.1 现有指标（Step 9）

| 指标名 | 类型 | 标签 | 来源 |
|--------|------|------|------|
| `scheduler_loop_duration_seconds` | Histogram | `node_id` | `scheduler_metrics.go` |
| `load_checker_sleep_duration_seconds` | Histogram | `node_id`, `checker_type` | `scheduler_metrics.go` |
| `gorm_query_duration_seconds` | Histogram | `operation`, `table` | `gorm_plugin.go` |

### 4.2 新增业务层 RED 指标

| 指标名 | 类型 | 标签 | 含义 | RED 维度 |
|--------|------|------|------|---------|
| `task_schedule_total` | Counter | `status`, `node_id` | 任务调度总次数 | **R**ate + **E**rror |
| `task_execution_duration_seconds` | Histogram | `task_type`, `node_id` | 任务执行端到端耗时 | **D**uration |
| `task_queue_depth` | Gauge | `node_id` | 待调度任务积压量 | USE/Saturation |
| `task_retry_total` | Counter | `node_id` | 任务重试累计次数 | **E**rror |
| `dag_step_duration_seconds` | Histogram | `plan_id`, `step_name` | DAG 各步骤耗时 | **D**uration |

### 4.3 新增基础设施层指标

| 指标名 | 类型 | 标签 | 含义 |
|--------|------|------|------|
| `grpc_client_connection_count` | Gauge | `target` | gRPC 客户端活跃连接数 |
| `compensator_scan_total` | Counter | `type`, `node_id` | 补偿器扫描总次数 |

### 4.4 指标注册代码

**文件**：`pkg/prometheus/red_metrics.go`

```go
package prometheus

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// REDMetrics 业务层 RED 指标收集器。
// RED = Rate + Error + Duration，是面向请求型服务的标准可观测性方法论。
type REDMetrics struct {
	nodeID string

	// Rate + Error：任务调度计数
	// status 标签值：success / failed / skipped
	TaskScheduleTotal *prometheus.CounterVec

	// Duration：任务执行端到端耗时
	// task_type 标签值：normal / sharding / plan
	TaskExecutionDuration *prometheus.HistogramVec

	// Saturation：待调度任务积压量
	TaskQueueDepth prometheus.Gauge

	// Error：任务重试次数
	TaskRetryTotal prometheus.Counter

	// Duration：DAG 步骤耗时
	DAGStepDuration *prometheus.HistogramVec

	// 基础设施层
	GRPCClientConnectionCount *prometheus.GaugeVec
	CompensatorScanTotal      *prometheus.CounterVec
}

// NewREDMetrics 创建 RED 指标收集器实例。
func NewREDMetrics(nodeID string) *REDMetrics {
	return &REDMetrics{
		nodeID: nodeID,

		TaskScheduleTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "task_schedule_total",
				Help: "任务调度总次数，按状态分类",
			},
			[]string{"status", "node_id"},
		),

		TaskExecutionDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name: "task_execution_duration_seconds",
				Help: "任务执行端到端耗时（秒）",
				Buckets: []float64{
					0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300,
				},
			},
			[]string{"task_type", "node_id"},
		),

		TaskQueueDepth: promauto.NewGauge(prometheus.GaugeOpts{
			Name:        "task_queue_depth",
			Help:        "当前待调度任务积压量",
			ConstLabels: prometheus.Labels{"node_id": nodeID},
		}),

		TaskRetryTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name:        "task_retry_total",
			Help:        "任务重试累计次数",
			ConstLabels: prometheus.Labels{"node_id": nodeID},
		}),

		DAGStepDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "dag_step_duration_seconds",
				Help:    "DAG 工作流各步骤耗时（秒）",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"plan_id", "step_name"},
		),

		GRPCClientConnectionCount: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "grpc_client_connection_count",
				Help: "gRPC 客户端活跃连接数",
			},
			[]string{"target"},
		),

		CompensatorScanTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "compensator_scan_total",
				Help: "补偿器扫描总次数",
			},
			[]string{"type", "node_id"},
		),
	}
}

// RecordSchedule 记录一次调度结果。
// status: "success" | "failed" | "skipped"
func (m *REDMetrics) RecordSchedule(status string) {
	m.TaskScheduleTotal.WithLabelValues(status, m.nodeID).Inc()
}

// RecordExecution 记录任务执行耗时。
// taskType: "normal" | "sharding" | "plan"
func (m *REDMetrics) RecordExecution(taskType string, durationSeconds float64) {
	m.TaskExecutionDuration.WithLabelValues(taskType, m.nodeID).Observe(durationSeconds)
}

// SetQueueDepth 设置当前待调度积压量。
func (m *REDMetrics) SetQueueDepth(depth float64) {
	m.TaskQueueDepth.Set(depth)
}

// IncRetry 累加一次重试。
func (m *REDMetrics) IncRetry() {
	m.TaskRetryTotal.Inc()
}

// RecordDAGStep 记录 DAG 步骤耗时。
func (m *REDMetrics) RecordDAGStep(planID, stepName string, durationSeconds float64) {
	m.DAGStepDuration.WithLabelValues(planID, stepName).Observe(durationSeconds)
}

// RecordCompensatorScan 记录补偿器扫描。
// compensatorType: "retry" | "reschedule" | "sharding" | "interrupt"
func (m *REDMetrics) RecordCompensatorScan(compensatorType string) {
	m.CompensatorScanTotal.WithLabelValues(compensatorType, m.nodeID).Inc()
}
```

### 4.5 指标埋点位置

```go
// ========== scheduleLoop 内埋点 ==========
func (s *Scheduler) scheduleOneLoop(ctx context.Context) {
    // 1. 查询可调度任务数量 → 更新 queue depth
    tasks, err := s.acquirer.Acquire(ctx, s.batchSize)
    s.redMetrics.SetQueueDepth(float64(len(tasks)))

    for _, task := range tasks {
        start := time.Now()
        err := s.runner.Run(ctx, task)

        // 2. 记录调度结果
        if err != nil {
            s.redMetrics.RecordSchedule("failed")
        } else {
            s.redMetrics.RecordSchedule("success")
        }

        // 3. 记录执行耗时
        duration := time.Since(start).Seconds()
        s.redMetrics.RecordExecution(task.Type.String(), duration)
    }
}

// ========== 补偿器埋点 ==========
func (c *RetryCompensator) compensate(ctx context.Context) {
    c.redMetrics.RecordCompensatorScan("retry")
    // ... 原有逻辑
    for _, exec := range candidates {
        c.redMetrics.IncRetry()
    }
}

// ========== DAG 步骤埋点 ==========
func (p *PlanService) executeStep(ctx context.Context, planID string, node *PlanNode) {
    start := time.Now()
    defer func() {
        p.redMetrics.RecordDAGStep(planID, node.Name, time.Since(start).Seconds())
    }()
    // ... 原有逻辑
}

// ========== gRPC 连接池埋点 ==========
func (c *ClientsV2[T]) getOrCreate(addr string) (T, error) {
    // ... 创建连接后
    c.redMetrics.GRPCClientConnectionCount.WithLabelValues(addr).Inc()
    // ... 关闭连接时
    c.redMetrics.GRPCClientConnectionCount.WithLabelValues(addr).Dec()
}
```

### 4.6 PromQL 查询示例

#### Rate（速率）

```promql
# 任务调度 QPS（每秒调度任务数）
rate(task_schedule_total[1m])

# 按状态分组的调度速率
sum by (status) (rate(task_schedule_total[5m]))

# 调度成功率
sum(rate(task_schedule_total{status="success"}[5m]))
/
sum(rate(task_schedule_total[5m]))
```

#### Error（错误率）

```promql
# 调度失败率
sum(rate(task_schedule_total{status="failed"}[5m]))
/
sum(rate(task_schedule_total[5m]))

# 重试速率
rate(task_retry_total[5m])

# 补偿器触发频率
sum by (type) (rate(compensator_scan_total[5m]))
```

#### Duration（延迟）

```promql
# 任务执行 P99 延迟
histogram_quantile(0.99, sum by (le, task_type) (
    rate(task_execution_duration_seconds_bucket[5m])
))

# 任务执行 P95 延迟（按类型分组）
histogram_quantile(0.95, sum by (le, task_type) (
    rate(task_execution_duration_seconds_bucket[5m])
))

# 任务执行平均延迟
sum(rate(task_execution_duration_seconds_sum[5m]))
/
sum(rate(task_execution_duration_seconds_count[5m]))

# DAG 步骤 P99 延迟
histogram_quantile(0.99, sum by (le, plan_id, step_name) (
    rate(dag_step_duration_seconds_bucket[5m])
))
```

#### Saturation（饱和度）

```promql
# 任务队列积压量
task_queue_depth

# gRPC 活跃连接数
sum(grpc_client_connection_count)
```

### 4.7 指标数据流

```
┌─────────────────────────────────────────────────────────────────────────┐
│  Scheduler 调度循环                                                     │
│    ├── RecordSchedule("success"/"failed")                              │
│    │   → task_schedule_total{status="...", node_id="..."}              │
│    │                                                                    │
│    ├── RecordExecution(taskType, duration)                              │
│    │   → task_execution_duration_seconds{task_type="...", node_id}     │
│    │                                                                    │
│    ├── SetQueueDepth(len(tasks))                                       │
│    │   → task_queue_depth{node_id="..."}                               │
│    │                                                                    │
│  补偿器                                                                │
│    ├── RecordCompensatorScan("retry"/"reschedule"/...)                  │
│    │   → compensator_scan_total{type="...", node_id="..."}             │
│    │                                                                    │
│    ├── IncRetry()                                                      │
│    │   → task_retry_total{node_id="..."}                               │
│    │                                                                    │
│  DAG 执行器                                                            │
│    └── RecordDAGStep(planID, stepName, duration)                       │
│        → dag_step_duration_seconds{plan_id="...", step_name="..."}     │
│                                                                         │
│  gRPC 连接池                                                           │
│    └── GRPCClientConnectionCount.Inc() / .Dec()                        │
│        → grpc_client_connection_count{target="..."}                    │
└───────────────────────────┬─────────────────────────────────────────────┘
                            │ scrape /metrics :9003
                            ▼
                    ┌──────────────────┐         ┌────────────────────┐
                    │  Prometheus       │────────→│  Grafana           │
                    │  :9090            │         │  RED Dashboard     │
                    │  alerting_rules   │────────→│  Alert Panels      │
                    └──────────┬───────┘         └────────────────────┘
                               │ alert
                               ▼
                    ┌──────────────────┐
                    │  Alertmanager    │
                    │  :9093           │
                    │  → Webhook/Email │
                    └──────────────────┘
```

---

## 5. 告警规则配置

### 5.1 告警分级策略

| 级别 | 含义 | 响应时间 | 通知方式 |
|------|------|---------|---------|
| **critical** | 系统不可用或数据丢失风险 | 5 分钟内 | 即时通知（电话/PagerDuty） |
| **warning** | 性能劣化或异常趋势 | 30 分钟内 | 消息推送（企微/钉钉/Slack） |
| **info** | 日常巡检信息 | 下一工作日 | 邮件/Dashboard 展示 |

### 5.2 完整告警规则文件

**文件**：`scripts/prometheus/alerting_rules.yml`

```yaml
groups:
  # ========== 调度器核心告警 ==========
  - name: scheduler_critical
    rules:
      # 调度循环 P99 延迟超过 5 秒
      - alert: ScheduleLoopSlow
        expr: |
          histogram_quantile(0.99,
            sum by (le, node_id) (
              rate(scheduler_loop_duration_seconds_bucket[5m])
            )
          ) > 5
        for: 5m
        labels:
          severity: warning
          team: platform
        annotations:
          summary: "调度循环 P99 延迟超过 5s"
          description: >
            节点 {{ $labels.node_id }} 的调度循环 P99 延迟为
            {{ $value | printf "%.2f" }}s，超过 5s 阈值。
            可能原因：DB 慢查询、任务抢占竞争激烈、负载过高。
          runbook_url: "https://wiki.internal/runbook/schedule-loop-slow"

      # 待调度任务积压超过 1000
      - alert: TaskQueueBacklog
        expr: task_queue_depth > 1000
        for: 10m
        labels:
          severity: critical
          team: platform
        annotations:
          summary: "待调度任务积压超过 1000"
          description: >
            节点 {{ $labels.node_id }} 的任务积压量为 {{ $value }}，
            持续 10 分钟未消化。可能需要扩容调度器节点或检查下游执行器。
          runbook_url: "https://wiki.internal/runbook/task-queue-backlog"

      # 补偿器重试率异常
      - alert: CompensatorHighRetryRate
        expr: |
          sum by (node_id) (rate(task_retry_total[5m])) > 10
        for: 5m
        labels:
          severity: warning
          team: platform
        annotations:
          summary: "任务重试率异常升高"
          description: >
            节点 {{ $labels.node_id }} 的任务重试速率为
            {{ $value | printf "%.1f" }}/s，超过 10/s 阈值。
            可能原因：执行器故障、网络不稳定、任务逻辑 bug。

      # 调度器节点宕机
      - alert: NodeDown
        expr: up{job="scheduler"} == 0
        for: 1m
        labels:
          severity: critical
          team: platform
        annotations:
          summary: "调度器节点不可达"
          description: >
            调度器实例 {{ $labels.instance }} 已不可达超过 1 分钟。
            请立即检查容器状态和网络连通性。

  # ========== 性能预警 ==========
  - name: scheduler_warning
    rules:
      # 调度成功率低于 95%
      - alert: LowScheduleSuccessRate
        expr: |
          (
            sum(rate(task_schedule_total{status="success"}[10m]))
            /
            sum(rate(task_schedule_total[10m]))
          ) < 0.95
        for: 10m
        labels:
          severity: warning
          team: platform
        annotations:
          summary: "调度成功率低于 95%"
          description: >
            近 10 分钟调度成功率为 {{ $value | printf "%.2f" }}%，
            低于 95% 阈值。

      # 任务执行 P99 超过 60 秒
      - alert: TaskExecutionSlow
        expr: |
          histogram_quantile(0.99,
            sum by (le, task_type) (
              rate(task_execution_duration_seconds_bucket[5m])
            )
          ) > 60
        for: 5m
        labels:
          severity: warning
          team: platform
        annotations:
          summary: "任务执行 P99 延迟超过 60s"
          description: >
            任务类型 {{ $labels.task_type }} 的 P99 执行延迟为
            {{ $value | printf "%.1f" }}s。

      # DB 查询 P99 超过 500ms
      - alert: DatabaseSlowQuery
        expr: |
          histogram_quantile(0.99,
            sum by (le, operation) (
              rate(gorm_query_duration_seconds_bucket[5m])
            )
          ) > 0.5
        for: 5m
        labels:
          severity: warning
          team: platform
        annotations:
          summary: "数据库查询 P99 延迟超过 500ms"
          description: >
            操作类型 {{ $labels.operation }} 的 P99 查询延迟为
            {{ $value | printf "%.3f" }}s。

  # ========== 信息类告警 ==========
  - name: scheduler_info
    rules:
      # 补偿器扫描频率
      - alert: CompensatorHighScanRate
        expr: |
          sum by (type) (rate(compensator_scan_total[30m])) > 100
        for: 30m
        labels:
          severity: info
          team: platform
        annotations:
          summary: "补偿器扫描频率偏高"
          description: >
            补偿器 {{ $labels.type }} 的扫描速率为
            {{ $value | printf "%.1f" }}/s，日常基线建议低于 100/s。

      # gRPC 连接数偏高
      - alert: HighGRPCConnectionCount
        expr: sum(grpc_client_connection_count) > 50
        for: 15m
        labels:
          severity: info
          team: platform
        annotations:
          summary: "gRPC 连接数偏高"
          description: >
            当前 gRPC 活跃连接总数为 {{ $value }}，建议检查是否有连接泄漏。
```

### 5.3 Alertmanager 配置

**文件**：`scripts/prometheus/alertmanager.yml`

```yaml
global:
  # 同一组告警的聚合等待时间
  group_wait: 30s
  # 同一组告警的发送间隔
  group_interval: 5m
  # 同一告警重复通知间隔
  repeat_interval: 4h
  # 默认的 resolve 超时
  resolve_timeout: 5m

# 路由树：按 severity 分发
route:
  receiver: 'default-webhook'
  group_by: ['alertname', 'severity']
  routes:
    # critical 级别：立即通知
    - match:
        severity: critical
      receiver: 'critical-webhook'
      group_wait: 10s
      repeat_interval: 1h

    # warning 级别：正常分组
    - match:
        severity: warning
      receiver: 'warning-webhook'
      group_wait: 30s
      repeat_interval: 4h

    # info 级别：低频通知
    - match:
        severity: info
      receiver: 'default-webhook'
      group_wait: 5m
      repeat_interval: 12h

# 抑制规则：critical 触发时抑制同名的 warning/info
inhibit_rules:
  - source_match:
      severity: 'critical'
    target_match:
      severity: 'warning'
    equal: ['alertname']

  - source_match:
      severity: 'warning'
    target_match:
      severity: 'info'
    equal: ['alertname']

# 通知接收器
receivers:
  - name: 'default-webhook'
    webhook_configs:
      - url: 'http://localhost:8080/api/alerts'
        send_resolved: true

  - name: 'critical-webhook'
    webhook_configs:
      - url: 'http://localhost:8080/api/alerts/critical'
        send_resolved: true

  - name: 'warning-webhook'
    webhook_configs:
      - url: 'http://localhost:8080/api/alerts/warning'
        send_resolved: true
```

### 5.4 Prometheus 配置更新

**文件**：`scripts/prometheus/prometheus.yml`（完整版）

```yaml
global:
  scrape_interval: 15s
  evaluation_interval: 15s

# 告警规则文件
rule_files:
  - "alerting_rules.yml"

# Alertmanager 配置
alerting:
  alertmanagers:
    - static_configs:
        - targets:
            - "alertmanager:9093"

scrape_configs:
  # Prometheus 自监控
  - job_name: 'prometheus'
    static_configs:
      - targets: ['localhost:9090']

  # 调度器节点
  - job_name: 'scheduler'
    static_configs:
      - targets: ['task-scheduler:9003']
    scrape_interval: 10s
    metrics_path: /metrics

  # Alertmanager 监控
  - job_name: 'alertmanager'
    static_configs:
      - targets: ['alertmanager:9093']
```

---

## 6. OpenTelemetry 链路追踪

### 6.1 追踪范围

```
┌─────────────────────────────────────────────────────────────────────────┐
│                      一次完整的调度追踪链路                              │
│                                                                         │
│  ┌──────────────────────────────────────────────────────────────────┐  │
│  │  Root Span: scheduleLoop                                         │  │
│  │  TraceID: abc123...                                              │  │
│  │                                                                   │  │
│  │  ├── Span: db.query (FindSchedulableTasks)                       │  │
│  │  │   ├── db.system: mysql                                        │  │
│  │  │   ├── db.statement: SELECT * FROM tasks WHERE ...             │  │
│  │  │   └── duration: 12ms                                          │  │
│  │  │                                                                │  │
│  │  ├── Span: db.update (PreemptTask - CAS)                        │  │
│  │  │   ├── db.statement: UPDATE tasks SET status='running' ...     │  │
│  │  │   └── duration: 5ms                                           │  │
│  │  │                                                                │  │
│  │  ├── Span: grpc.client (ExecutorService.Execute)                 │  │
│  │  │   ├── rpc.system: grpc                                        │  │
│  │  │   ├── rpc.method: Execute                                     │  │
│  │  │   ├── net.peer.name: executor-node-1                          │  │
│  │  │   └── duration: 2.3s                                          │  │
│  │  │   │                                                            │  │
│  │  │   └── Span: grpc.server (ExecutorService.Execute)             │  │
│  │  │       ├── Span: task.execute (业务逻辑)                       │  │
│  │  │       └── duration: 2.1s                                      │  │
│  │  │                                                                │  │
│  │  ├── Span: kafka.produce (TaskCompleteEvent)                     │  │
│  │  │   ├── messaging.system: kafka                                 │  │
│  │  │   ├── messaging.destination: execution_report                 │  │
│  │  │   └── duration: 3ms                                           │  │
│  │  │                                                                │  │
│  │  └── Span: db.update (UpdateTaskStatus)                          │  │
│  │      └── duration: 4ms                                           │  │
│  └──────────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────────┘
```

### 6.2 OpenTelemetry SDK 初始化

**文件**：`pkg/otel/tracing.go`

```go
package otel

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

// Config OpenTelemetry 配置
type Config struct {
	ServiceName    string // 服务名称，如 "task-scheduler"
	ServiceVersion string // 版本号
	OTLPEndpoint   string // OTLP gRPC 端点，如 "jaeger:4317"
	SampleRate     float64 // 采样率 0.0-1.0，生产建议 0.1
}

// InitTracer 初始化 OpenTelemetry TracerProvider。
//
// 返回 shutdown 函数，必须在应用退出时调用以刷新所有 pending spans。
//
// 使用示例：
//
//	shutdown, err := otel.InitTracer(cfg)
//	if err != nil { ... }
//	defer shutdown(ctx)
func InitTracer(cfg Config) (func(context.Context) error, error) {
	ctx := context.Background()

	// 1. 创建 OTLP gRPC Exporter → 发送到 Jaeger/Tempo
	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
		otlptracegrpc.WithInsecure(), // 开发环境不加 TLS
	)
	if err != nil {
		return nil, fmt.Errorf("create OTLP exporter: %w", err)
	}

	// 2. 定义资源信息（服务元数据）
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String(cfg.ServiceName),
			semconv.ServiceVersionKey.String(cfg.ServiceVersion),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create resource: %w", err)
	}

	// 3. 创建 TracerProvider
	sampler := sdktrace.ParentBased(
		sdktrace.TraceIDRatioBased(cfg.SampleRate),
	)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter,
			sdktrace.WithBatchTimeout(5*time.Second),
			sdktrace.WithMaxExportBatchSize(512),
		),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	// 4. 设置全局 TracerProvider 和 Propagator
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, // W3C Trace Context
		propagation.Baggage{},     // W3C Baggage
	))

	// 返回 shutdown 函数
	return tp.Shutdown, nil
}
```

### 6.3 gRPC Interceptor 自动注入

```go
package otel

import (
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
)

// NewGRPCServerOptions 返回带 OTel 追踪的 gRPC Server 选项。
// 自动为每个 RPC 调用创建 span，记录方法名、状态码、错误信息。
func NewGRPCServerOptions() []grpc.ServerOption {
	return []grpc.ServerOption{
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
	}
}

// NewGRPCDialOptions 返回带 OTel 追踪的 gRPC Client 拨号选项。
// 自动传播 trace context 到下游服务。
func NewGRPCDialOptions() []grpc.DialOption {
	return []grpc.DialOption{
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	}
}
```

**集成到现有代码**：

```go
// ioc/grpc.go — 创建 gRPC Server 时注入
func InitSchedulerNodeGRPCServer(reporter *grpc.ReporterServer) *egrpc.Component {
	server := egrpc.Load("server.scheduler.grpc").Build(
		egrpc.WithServerOption(otel.NewGRPCServerOptions()...),
	)
	api.RegisterReporterServiceServer(server, reporter)
	return server
}

// pkg/grpc/clients_v2.go — 创建 gRPC Client 连接时注入
func (c *ClientsV2[T]) getOrCreate(addr string) (T, error) {
	conn, err := grpc.DialContext(ctx, addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()), // 自动传播 trace
	)
	// ...
}
```

### 6.4 GORM OTel Plugin

```go
package otel

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
)

const (
	spanStartKey = "otel:span"
)

// GormOTelPlugin 是 GORM 的 OpenTelemetry 追踪插件。
// 为每个数据库操作创建独立的 span，记录 SQL 语句、表名、操作类型。
type GormOTelPlugin struct {
	tracer trace.Tracer
}

func NewGormOTelPlugin() *GormOTelPlugin {
	return &GormOTelPlugin{
		tracer: otel.Tracer("gorm"),
	}
}

func (p *GormOTelPlugin) Name() string {
	return "otel_gorm_tracing"
}

func (p *GormOTelPlugin) Initialize(db *gorm.DB) error {
	// 注册 before 回调：创建 span
	for _, op := range []string{"query", "create", "update", "delete", "row", "raw"} {
		callbackName := "otel:before_" + op
		err := db.Callback().Query().Before("gorm:"+op).
			Register(callbackName, p.before(op))
		if err != nil {
			return err
		}

		afterCallbackName := "otel:after_" + op
		err = db.Callback().Query().After("gorm:"+op).
			Register(afterCallbackName, p.after)
		if err != nil {
			return err
		}
	}
	return nil
}

func (p *GormOTelPlugin) before(operation string) func(*gorm.DB) {
	return func(db *gorm.DB) {
		ctx := db.Statement.Context
		_, span := p.tracer.Start(ctx, "db."+operation,
			trace.WithSpanKind(trace.SpanKindClient),
			trace.WithAttributes(
				attribute.String("db.system", "mysql"),
				attribute.String("db.operation", operation),
			),
		)
		db.Set(spanStartKey, span)
	}
}

func (p *GormOTelPlugin) after(db *gorm.DB) {
	val, ok := db.Get(spanStartKey)
	if !ok {
		return
	}
	span, ok := val.(trace.Span)
	if !ok {
		return
	}
	defer span.End()

	// 记录表名和 SQL
	if db.Statement != nil {
		span.SetAttributes(
			attribute.String("db.table", db.Statement.Table),
			attribute.String("db.statement", db.Statement.SQL.String()),
		)
	}

	// 记录错误
	if db.Error != nil {
		span.RecordError(db.Error)
		span.SetStatus(codes.Error, db.Error.Error())
	}
}
```

### 6.5 Kafka 传播 Trace Context

```go
package otel

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// KafkaHeaderCarrier 实现 propagation.TextMapCarrier 接口，
// 将 trace context 存储到 Kafka 消息头中。
type KafkaHeaderCarrier map[string]string

func (c KafkaHeaderCarrier) Get(key string) string {
	return c[key]
}

func (c KafkaHeaderCarrier) Set(key, value string) {
	c[key] = value
}

func (c KafkaHeaderCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

// InjectKafkaHeaders 将当前 trace context 注入到 Kafka 消息头。
// 在 Producer 端调用。
func InjectKafkaHeaders(ctx context.Context) KafkaHeaderCarrier {
	carrier := KafkaHeaderCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	return carrier
}

// ExtractKafkaHeaders 从 Kafka 消息头提取 trace context。
// 在 Consumer 端调用，返回带 trace context 的新 context。
func ExtractKafkaHeaders(ctx context.Context, headers KafkaHeaderCarrier) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, headers)
}

// StartKafkaProducerSpan 创建 Kafka 生产者 span。
func StartKafkaProducerSpan(ctx context.Context, topic string) (context.Context, trace.Span) {
	tracer := otel.Tracer("kafka")
	return tracer.Start(ctx, "kafka.produce",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.system", "kafka"),
			attribute.String("messaging.destination", topic),
			attribute.String("messaging.operation", "publish"),
		),
	)
}

// StartKafkaConsumerSpan 创建 Kafka 消费者 span。
func StartKafkaConsumerSpan(ctx context.Context, topic string) (context.Context, trace.Span) {
	tracer := otel.Tracer("kafka")
	return tracer.Start(ctx, "kafka.consume",
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			attribute.String("messaging.system", "kafka"),
			attribute.String("messaging.destination", topic),
			attribute.String("messaging.operation", "receive"),
		),
	)
}
```

**集成到 Producer**：

```go
// internal/service/task/execution_service.go
func (s *executionService) sendCompletedEvent(ctx context.Context, exec TaskExecution) {
	// 1. 创建 Kafka producer span
	ctx, span := otel.StartKafkaProducerSpan(ctx, "execution_report")
	defer span.End()

	// 2. 将 trace context 注入到消息头
	headers := otel.InjectKafkaHeaders(ctx)

	// 3. 发送消息（带 headers）
	err := s.producer.ProduceTaskCompleteEvent(ctx, event, headers)
	if err != nil {
		span.RecordError(err)
	}
}
```

**集成到 Consumer**：

```go
// internal/service/task/execution_event_consumer.go
func (c *ExecutionReportEventConsumer) handleMessage(msg *mq.Message) {
	// 1. 从消息头提取 trace context
	ctx := otel.ExtractKafkaHeaders(context.Background(), msg.Headers)

	// 2. 创建 consumer span（自动关联到 producer 的 trace）
	ctx, span := otel.StartKafkaConsumerSpan(ctx, "execution_report")
	defer span.End()

	// 3. 处理消息（后续操作自动关联到同一 trace）
	c.handleEvent(ctx, msg.Value)
}
```

### 6.6 调度器 scheduleLoop 作为 Root Span

```go
// internal/scheduler/scheduler.go
func (s *Scheduler) scheduleOneLoop(ctx context.Context) {
	tracer := otel.Tracer("scheduler")

	// 创建 root span
	ctx, span := tracer.Start(ctx, "scheduleLoop",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("scheduler.node_id", s.nodeID),
		),
	)
	defer span.End()

	// 后续所有操作（DB 查询、gRPC 调用、Kafka 发送）
	// 自动成为此 span 的子 span
	tasks, err := s.acquirer.Acquire(ctx, s.batchSize)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "acquire failed")
		return
	}

	span.SetAttributes(
		attribute.Int("scheduler.acquired_tasks", len(tasks)),
	)

	for _, task := range tasks {
		s.scheduleTask(ctx, task) // ctx 传递，gRPC/DB 操作自动关联
	}
}
```

### 6.7 完整 Trace 示例

在 Jaeger UI 中查看一次任务调度的完整链路：

```
Trace: abc123def456
Duration: 2.4s
Spans: 8

├── scheduleLoop                    [scheduler]  2.4s
│   ├── db.query (FindSchedulable)  [gorm]       12ms
│   ├── db.update (PreemptTask)     [gorm]        5ms
│   ├── grpc.client (Execute)       [grpc]       2.3s
│   │   └── grpc.server (Execute)   [executor]   2.1s
│   │       └── task.execute        [executor]   2.0s
│   ├── kafka.produce (complete)    [kafka]        3ms
│   └── db.update (UpdateStatus)    [gorm]         4ms

Tags:
  scheduler.node_id = "node-1"
  scheduler.acquired_tasks = 5
  db.system = "mysql"
  rpc.method = "Execute"
  messaging.system = "kafka"
```

---

## 7. Docker Compose 新增服务

### 7.1 新增 Jaeger

```yaml
  # ---- Jaeger (链路追踪存储 + UI) ----
  jaeger:
    image: jaegertracing/all-in-one:1.57
    container_name: task-jaeger
    ports:
      # Jaeger UI
      - "16686:16686"
      # OTLP gRPC Receiver（OpenTelemetry SDK 推送入口）
      - "4317:4317"
      # OTLP HTTP Receiver
      - "4318:4318"
    environment:
      # 内存存储（开发环境足够）
      - COLLECTOR_OTLP_ENABLED=true
      - SPAN_STORAGE_TYPE=memory
      - MEMORY_MAX_TRACES=10000
    networks:
      - task-network
```

### 7.2 新增 Alertmanager

```yaml
  # ---- Alertmanager (告警管理) ----
  alertmanager:
    image: prom/alertmanager:v0.27.0
    container_name: task-alertmanager
    ports:
      - "9093:9093"
    volumes:
      - ./scripts/prometheus/alertmanager.yml:/etc/alertmanager/alertmanager.yml:ro
    command:
      - "--config.file=/etc/alertmanager/alertmanager.yml"
      - "--storage.path=/alertmanager"
    networks:
      - task-network
```

### 7.3 更新 Prometheus 服务

```yaml
  # ---- Prometheus (更新：增加告警规则) ----
  prometheus:
    image: prom/prometheus:latest
    container_name: task-prometheus
    user: root
    volumes:
      - ./scripts/prometheus/prometheus.yml:/etc/prometheus/prometheus.yml:ro
      - ./scripts/prometheus/alerting_rules.yml:/etc/prometheus/alerting_rules.yml:ro
      - prometheus-data:/prometheus
    ports:
      - "9090:9090"
    command:
      - "--web.enable-remote-write-receiver"
      - "--config.file=/etc/prometheus/prometheus.yml"
      - "--web.enable-lifecycle"
    depends_on:
      - alertmanager
    networks:
      - task-network
```

### 7.4 更新后的服务拓扑

```
docker-compose.yml (Step 15)
├── scheduler       ← 调度器（新增 OTel SDK）
├── mysql           ← MySQL 8.0
├── redis           ← Redis
├── kafka           ← Kafka 3.9.0
├── etcd            ← etcd
├── prometheus      ← Prometheus（新增告警规则 + Alertmanager 集成）
├── alertmanager    ← ★ 新增：告警管理器
├── jaeger          ← ★ 新增：链路追踪
└── grafana         ← Grafana（新增 Jaeger 数据源）
```

### 7.5 端口总览

| 服务 | 端口 | 用途 |
|------|------|------|
| scheduler | 9002, 9003 | gRPC + Governor |
| mysql | 13316 | 数据库 |
| redis | 6379 | 缓存 |
| kafka | 9092, 9094 | 消息队列 |
| etcd | 2379, 2380 | 服务发现 |
| prometheus | 9090 | 指标采集 |
| **alertmanager** | **9093** | **告警管理（新增）** |
| **jaeger** | **16686, 4317, 4318** | **追踪 UI + OTLP Receiver（新增）** |
| grafana | 3000 | 监控面板 |

---

## 8. Grafana RED Dashboard 模板

### 8.1 Dashboard JSON 模板

**文件**：`scripts/grafana/dashboards/scheduler-red.json`

以下是核心面板定义（省略 Grafana JSON 框架部分，给出关键 Panel 配置）：

#### Row 1：调度概览（Rate）

| Panel | PromQL | 可视化类型 | 说明 |
|-------|--------|-----------|------|
| 调度 QPS | `sum(rate(task_schedule_total[1m]))` | Stat（大数字） | 每秒调度任务数 |
| 调度成功率 | `sum(rate(task_schedule_total{status="success"}[5m])) / sum(rate(task_schedule_total[5m])) * 100` | Gauge（百分比） | 绿→黄→红阈值 |
| 按状态分组速率 | `sum by (status) (rate(task_schedule_total[5m]))` | Timeseries（堆叠） | success/failed/skipped |
| 任务队列深度 | `task_queue_depth` | Timeseries + 阈值线 | 红线标注 1000 |

#### Row 2：延迟分布（Duration）

| Panel | PromQL | 可视化类型 |
|-------|--------|-----------|
| 调度循环 P99 | `histogram_quantile(0.99, sum by (le) (rate(scheduler_loop_duration_seconds_bucket[5m])))` | Timeseries |
| 任务执行 P50/P95/P99 | `histogram_quantile({0.5,0.95,0.99}, sum by (le, task_type) (rate(task_execution_duration_seconds_bucket[5m])))` | Timeseries（多线） |
| DAG 步骤耗时 | `histogram_quantile(0.95, sum by (le, step_name) (rate(dag_step_duration_seconds_bucket[5m])))` | Bar chart |
| DB 操作延迟 | `histogram_quantile(0.99, sum by (le, operation) (rate(gorm_query_duration_seconds_bucket[5m])))` | Timeseries |

#### Row 3：错误与重试（Error）

| Panel | PromQL | 可视化类型 |
|-------|--------|-----------|
| 调度失败率趋势 | `sum(rate(task_schedule_total{status="failed"}[5m])) / sum(rate(task_schedule_total[5m]))` | Timeseries + 阈值 |
| 重试速率 | `rate(task_retry_total[5m])` | Timeseries |
| 补偿器扫描频率 | `sum by (type) (rate(compensator_scan_total[5m]))` | Timeseries（多线） |
| 活跃告警 | Alertmanager API | Alert list（内置面板） |

#### Row 4：基础设施

| Panel | PromQL | 可视化类型 |
|-------|--------|-----------|
| gRPC 连接数 | `sum by (target) (grpc_client_connection_count)` | Timeseries |
| 负载检查退避 | `histogram_quantile(0.95, rate(load_checker_sleep_duration_seconds_bucket[5m]))` | Timeseries |
| DB 操作 QPS | `sum by (operation) (rate(gorm_query_duration_seconds_count[1m]))` | Timeseries |
| 热点表 Top 5 | `topk(5, sum by (table) (rate(gorm_query_duration_seconds_count[5m])))` | Bar chart |

### 8.2 Grafana Provisioning 自动配置

**文件**：`scripts/grafana/provisioning/datasources/datasources.yml`

```yaml
apiVersion: 1

datasources:
  # Prometheus 数据源
  - name: Prometheus
    type: prometheus
    access: proxy
    url: http://prometheus:9090
    isDefault: true
    editable: false

  # Jaeger 数据源
  - name: Jaeger
    type: jaeger
    access: proxy
    url: http://jaeger:16686
    editable: false
```

**文件**：`scripts/grafana/provisioning/dashboards/dashboards.yml`

```yaml
apiVersion: 1

providers:
  - name: 'default'
    orgId: 1
    folder: 'Task Scheduler'
    type: file
    disableDeletion: false
    updateIntervalSeconds: 30
    options:
      path: /var/lib/grafana/dashboards
      foldersFromFilesStructure: false
```

**docker-compose.yml Grafana 更新**：

```yaml
  grafana:
    image: grafana/grafana-enterprise:latest
    container_name: task-grafana
    ports:
      - "3000:3000"
    environment:
      - GF_SECURITY_ADMIN_PASSWORD=123
    volumes:
      - grafana-data:/var/lib/grafana
      # Provisioning：自动配置数据源和面板
      - ./scripts/grafana/provisioning:/etc/grafana/provisioning:ro
      - ./scripts/grafana/dashboards:/var/lib/grafana/dashboards:ro
    depends_on:
      - prometheus
      - jaeger
    networks:
      - task-network
```

---

## 9. 应用层接入流程

### 9.1 Wire 依赖注入更新

```go
// ioc/otel.go

// InitTracer 初始化 OpenTelemetry TracerProvider。
func InitTracer() (func(context.Context) error, error) {
	return otel.InitTracer(otel.Config{
		ServiceName:    "task-scheduler",
		ServiceVersion: "1.0.0",
		OTLPEndpoint:   "jaeger:4317", // 从配置文件读取
		SampleRate:     1.0,           // 开发环境全采样
	})
}

// InitREDMetrics 初始化 RED 指标收集器。
func InitREDMetrics(nodeID string) *prometheus.REDMetrics {
	return prometheus.NewREDMetrics(nodeID)
}
```

```go
// ioc/wire.go — Provider Set 更新
var BaseSet = wire.NewSet(
	InitDB,
	InitDistributedLock,
	InitEtcdClient,
	InitMQ,
	InitPrometheusClient,
	InitTracer,      // ★ 新增
	InitREDMetrics,  // ★ 新增
	// ...
)
```

### 9.2 main.go 启动更新

```go
func main() {
	egoApp := ego.New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	app := ioc.InitSchedulerApp()

	// ★ 初始化 OpenTelemetry
	shutdown, err := ioc.InitTracer()
	if err != nil {
		panic(fmt.Sprintf("init tracer: %v", err))
	}
	defer shutdown(ctx)

	app.StartTasks(ctx)
	egoApp.Serve(
		egovernor,
		app.GRPC,
		app.Scheduler,
	).Run()
}
```

### 9.3 Go Module 新增依赖

```
go get go.opentelemetry.io/otel@v1.24.0
go get go.opentelemetry.io/otel/sdk@v1.24.0
go get go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc@v1.24.0
go get go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc@v0.49.0
```

---

## 10. 部署验证流程

### 10.1 一键启动

```bash
# 1. 启动全部 9 个服务
make docker-up

# 2. 检查容器状态
docker compose ps
# 期望：9 个服务全部 Running/Healthy

# 3. 验证 Prometheus 告警规则
curl -s http://localhost:9090/api/v1/rules | jq '.data.groups[].name'
# 期望输出：
# "scheduler_critical"
# "scheduler_warning"
# "scheduler_info"

# 4. 验证 Alertmanager
curl -s http://localhost:9093/api/v2/status | jq '.config.original'
# 期望：返回 alertmanager.yml 配置内容

# 5. 验证 Jaeger UI
open http://localhost:16686
# 期望：看到 "task-scheduler" 服务

# 6. 验证 RED 指标
curl -s http://localhost:9003/metrics | grep task_schedule_total
curl -s http://localhost:9003/metrics | grep task_execution_duration_seconds
curl -s http://localhost:9003/metrics | grep task_queue_depth
curl -s http://localhost:9003/metrics | grep compensator_scan_total

# 7. Grafana 查看 Dashboard
open http://localhost:3000
# admin / 123
# 导航到 Task Scheduler → RED Dashboard
```

### 10.2 链路追踪验证

```bash
# 1. 插入测试任务
mysql -h 127.0.0.1 -P 13316 -u root -proot -e "
INSERT INTO task.tasks (name, type, cron, status, next_time)
VALUES ('trace-test', 0, '*/1 * * * *', 0, UNIX_TIMESTAMP());
"

# 2. 等待调度循环执行（~10s）
sleep 15

# 3. 在 Jaeger UI 查看 trace
open http://localhost:16686
# Service: task-scheduler
# Operation: scheduleLoop
# 点击任意 trace → 查看完整链路
```

### 10.3 告警验证

```bash
# 1. 模拟任务积压（插入大量任务）
for i in $(seq 1 1100); do
  mysql -h 127.0.0.1 -P 13316 -u root -proot -e "
  INSERT INTO task.tasks (name, type, cron, status, next_time)
  VALUES ('backlog-test-$i', 0, '*/1 * * * *', 0, UNIX_TIMESTAMP());
  "
done

# 2. 等待告警触发（10 分钟）
# 查看 Prometheus Alerts 页面
open http://localhost:9090/alerts
# 期望：TaskQueueBacklog 告警从 pending → firing

# 3. 查看 Alertmanager
open http://localhost:9093/#/alerts
# 期望：收到 TaskQueueBacklog critical 告警
```

---

## 11. 完整文件清单

| 文件路径 | 作用 | 状态 |
|---------|------|------|
| `pkg/prometheus/red_metrics.go` | RED 指标定义和记录方法 | **新增** |
| `pkg/otel/tracing.go` | OpenTelemetry SDK 初始化 | **新增** |
| `pkg/otel/grpc.go` | gRPC OTel Interceptor | **新增** |
| `pkg/otel/gorm_plugin.go` | GORM OTel 追踪插件 | **新增** |
| `pkg/otel/kafka.go` | Kafka trace context 传播 | **新增** |
| `scripts/prometheus/alerting_rules.yml` | 告警规则（3 组 9 条） | **新增** |
| `scripts/prometheus/alertmanager.yml` | Alertmanager 路由配置 | **新增** |
| `scripts/prometheus/prometheus.yml` | Prometheus 配置 | **修改** |
| `scripts/grafana/provisioning/` | Grafana 自动配置 | **新增** |
| `scripts/grafana/dashboards/scheduler-red.json` | RED Dashboard JSON | **新增** |
| `docker-compose.yml` | 新增 Jaeger + Alertmanager | **修改** |
| `ioc/otel.go` | Wire OTel Provider | **新增** |
| `ioc/wire.go` | Provider Set 更新 | **修改** |

---

## 12. 实现局限与改进方向

| # | 现状 | 改进方案 | 优先级 |
|---|------|---------|--------|
| 1 | Jaeger 使用内存存储 | 接入 Elasticsearch/Cassandra 持久化 | 高（生产必选） |
| 2 | 采样率开发环境 100% | 生产环境改为 10% 或自适应采样 | 高 |
| 3 | Alertmanager Webhook 配置为 localhost | 对接企微/钉钉/PagerDuty | 中 |
| 4 | Dashboard 只有调度器 | 增加执行节点 Dashboard | 中 |
| 5 | 缺少 SLO/SLI 定义 | 基于 RED 指标定义 SLI，配置 Error Budget 告警 | 中 |
| 6 | 缺少日志与 Trace 关联 | 在日志中注入 TraceID，Grafana Loki 关联查询 | 低 |
| 7 | 缺少 Exemplar 支持 | Histogram 指标附带 TraceID Exemplar，Grafana 点击直跳 Jaeger | 低 |

---

## 13. 面试高频 Q&A

### Q1: 为什么选 RED 而不是 USE 方法论？

RED 面向**请求型服务**——调度器的每次 scheduleLoop 本质就是一次请求处理。USE 面向**资源型系统**（CPU、磁盘），更适合基础设施监控。我的设计是 RED 为主（task_schedule_total、task_execution_duration）、USE 为辅（task_queue_depth 是饱和度指标、grpc_connection_count 是利用率指标），两者结合覆盖"请求视角"和"资源视角"。

### Q2: 告警规则为什么要分三级？

借鉴 Google SRE 的告警分级理念：
- **Critical**：影响用户或数据完整性，必须立即响应（如 NodeDown、TaskQueueBacklog）
- **Warning**：性能劣化趋势，需要关注但不紧急（如 ScheduleLoopSlow、LowSuccessRate）
- **Info**：日常巡检信息，用于容量规划（如 CompensatorHighScanRate）

分级的核心价值是**告警疲劳管理**——如果所有告警都是 critical，on-call 人员很快就会忽略告警。通过分级 + 抑制规则（critical 触发时抑制同名 warning），确保真正重要的告警不被淹没。

### Q3: OpenTelemetry vs 直接用 Jaeger SDK？

Jaeger 官方在 2022 年就宣布其 Client SDK 进入维护模式，推荐迁移到 OpenTelemetry SDK。选 OTel 的原因：
1. **厂商中立**：切换后端（Jaeger → Tempo → Zipkin）只需改 Exporter 配置
2. **一套 SDK 两个信号**：OTel SDK 同时支持 traces 和 metrics，减少依赖
3. **社区活跃**：OTel 是 CNCF 第二活跃项目（仅次于 Kubernetes）
4. **面试加分**：提到 OpenTelemetry 表示跟进了云原生可观测性的最新标准

### Q4: gRPC interceptor 自动注入 trace 的原理？

OTel 的 `otelgrpc.NewClientHandler()` / `NewServerHandler()` 利用 gRPC 的 `stats.Handler` 接口：
1. **客户端**：在 RPC 发起前将 trace context 序列化到 gRPC metadata（`traceparent` header）
2. **服务端**：从 gRPC metadata 反序列化 trace context，创建子 span
3. 这种方式比传统的 `UnaryInterceptor` 更完整——同时覆盖 Unary 和 Stream 调用，无需分别注册

关键是 **W3C Trace Context 标准**（`traceparent: 00-{trace_id}-{span_id}-{flags}`），这是跨服务传播 trace 的行业标准格式。

### Q5: Kafka 的 trace propagation 怎么实现的？

Kafka 没有像 gRPC 那样的原生 metadata 机制，需要手动处理：
1. **Producer 端**：调用 `otel.GetTextMapPropagator().Inject(ctx, carrier)` 将 trace context 写入 Kafka 消息 headers
2. **Consumer 端**：调用 `otel.GetTextMapPropagator().Extract(ctx, carrier)` 从 headers 恢复 trace context
3. 恢复的 context 传给后续操作，新创建的 span 自动成为 producer span 的子 span

这和 HTTP 请求在 header 中传播 `traceparent` 是同一个思路，只是 Kafka 消息的"header"需要自己封装一个 `TextMapCarrier` 适配器。

### Q6: 生产环境的采样策略怎么设计？

不能全采样（100% 会造成 Jaeger 存储爆炸），也不能不采样（失去排障能力）。我的方案是**自适应采样**：

```go
// ParentBased：如果上游已决定采样，子 span 跟随
// TraceIDRatioBased：根据 TraceID 做概率采样
sampler := sdktrace.ParentBased(
    sdktrace.TraceIDRatioBased(0.1), // 10% 采样
)
```

进阶策略：
- **正常请求**：10% 采样
- **慢请求**（超过 P95 阈值）：100% 采样（tail-based sampling）
- **错误请求**：100% 采样
- 这需要用 OpenTelemetry Collector 的 `tailsamplingprocessor` 实现

---

## 14. 总结

Step 15 将可观测性从"能看到几个指标"提升到"生产级可诊断"：

| 维度 | Before (Step 9) | After (Step 15) |
|------|-----------------|-----------------|
| **Metrics** | 3 个 Histogram | 10+ 个（RED 全覆盖 + 基础设施） |
| **Alerting** | 无 | 9 条规则、3 级分级、抑制 + 路由 |
| **Tracing** | 无 | OpenTelemetry 全链路（gRPC + GORM + Kafka） |
| **Dashboard** | 手动创建 | Provisioning 自动导入 |
| **可诊断性** | 只能看趋势 | 任意慢请求可追溯到具体 span |

核心设计理念：**可观测性不是锦上添花，是生产系统的基础能力**。RED 指标告诉你"系统有没有问题"，告警规则告诉你"什么时候出了问题"，链路追踪告诉你"问题出在哪里"。三者缺一不可。
