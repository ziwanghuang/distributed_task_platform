# Step 2：gRPC 远程执行 + etcd 服务发现

> **目标**：调度器通过 gRPC 将任务下发到独立的执行器进程，执行器通过 etcd 自动注册/注销，调度器实时感知执行器上下线。
>
> **完成后你能看到**：启动 etcd → 启动执行器（自动注册到 etcd）→ 启动调度器 → 调度器通过 gRPC 远程下发任务到执行器 → 执行器通过 gRPC 上报状态 → 数据库中执行记录状态更新为 SUCCESS。

---

## 1. 架构总览

Step 2 在 Step 1 的基础上，将 `LocalInvoker` 替换为 `GRPCInvoker`，引入 etcd 作为服务注册中心：

```
                    ┌─────────────────────────────────────────────────────────┐
                    │                   Scheduler（调度器进程）                  │
                    │                                                         │
  ┌──────┐         │  Scheduler ──► Runner ──► Invoker Dispatcher            │
  │ MySQL│◄────────┤                                │                        │
  │      │ CAS抢占  │                    ┌───────────┼───────────┐            │
  └──────┘         │                    │           │           │            │
                    │              GRPCInvoker  HTTPInvoker  LocalInvoker    │
                    │                    │                                    │
                    │              ClientsV2<ExecutorServiceClient>           │
                    │                    │                                    │
                    │           ┌────────┴────────┐                          │
                    │           │  Custom Resolver │◄──── etcd Watch         │
                    │           └────────┬────────┘                          │
                    │           ┌────────┴────────┐                          │
                    │           │RoutingRoundRobin│                          │
                    │           │    Balancer      │                          │
                    │           └────────┬────────┘                          │
                    │                    │ gRPC Call                          │
                    │  ReporterServer ◄──┼──────── gRPC Report               │
                    └────────────────────┼───────────────────────────────────┘
                                         │
                              ┌──────────┴──────────┐
                    ┌─────────┤      etcd Cluster    ├─────────┐
                    │         └─────────────────────┘         │
                    │ Register                         Register│
              ┌─────┴──────┐                          ┌───────┴─────┐
              │  Executor A │                          │  Executor B  │
              │  :9004      │                          │  :9005       │
              │             │                          │              │
              │ ExecutorSvc │                          │ ExecutorSvc  │
              │ (gRPC Server)│                         │ (gRPC Server)│
              └─────────────┘                          └──────────────┘
```

### 1.1 Step 1 → Step 2 变更对比

| 维度 | Step 1 | Step 2 |
|------|--------|--------|
| 执行方式 | `LocalInvoker` 进程内执行 | `GRPCInvoker` 远程 gRPC 调用 |
| 服务发现 | 无（单进程） | etcd 注册中心 + Watch |
| 负载均衡 | 无 | 自定义 `RoutingRoundRobin` |
| 通信协议 | 函数调用 | Protobuf + gRPC |
| 部署模型 | 单进程 | 调度器 + N 个执行器 |
| 新增依赖 | 仅 MySQL | + etcd + Protobuf/buf |
| 状态上报 | Invoker 同步返回 | gRPC `ReporterService` |

### 1.2 核心组件清单

| 组件 | 职责 | 本步骤状态 |
|------|------|:--------:|
| **executor.proto** | 定义 ExecutorService（Execute/Interrupt/Query/Prepare） | ✅ 新增 |
| **reporter.proto** | 定义 ReporterService（Report/BatchReport） | ✅ 新增 |
| **etcd Registry** | 服务注册/注销/订阅/查询 | ✅ 新增 |
| **Custom Resolver** | etcd → gRPC 地址列表桥接 | ✅ 新增 |
| **RoutingRoundRobin Balancer** | 指定节点/排除节点/轮询三级路由 | ✅ 新增 |
| **ClientsV2** | 泛型 gRPC 客户端池（懒加载 + 线程安全） | ✅ 新增 |
| **GRPCInvoker** | 通过 gRPC 下发执行请求 | ✅ 新增 |
| **HTTPInvoker** | 通过 HTTP 下发执行请求 | ✅ 新增 |
| **Invoker Dispatcher** | Local → Local/gRPC/HTTP 三路由 | ✅ 升级 |
| **ReporterServer** | 接收执行器状态上报 | ✅ 新增 |
| LocalInvoker | 本地执行（降级/测试） | ✅ 保留 |

---

## 2. 前置依赖

- Go 1.24+
- Docker（MySQL + etcd）
- buf CLI（Protobuf 代码生成）

### 2.1 docker-compose.yml 增量

在 Step 1 的基础上新增 etcd 服务：

```yaml
services:
  # ... 保留 Step 1 的 mysql 服务 ...

  # Step 2 新增：etcd 服务发现
  etcd:
    image: "bitnami/etcd:latest"
    container_name: task-etcd
    environment:
      - ALLOW_NONE_AUTHENTICATION=yes
      - ETCD_ADVERTISE_CLIENT_URLS=http://etcd:2379
    ports:
      - "2379:2379"   # 客户端通信端口
      - "2380:2380"   # 集群节点通信端口
    volumes:
      - etcd-data:/bitnami/etcd
    networks:
      - task-network

volumes:
  # ... 保留 Step 1 的 mysql-data ...
  etcd-data:
```

```bash
# 启动 MySQL + etcd
docker compose up -d mysql etcd
```

### 2.2 安装 buf CLI

```bash
# macOS
brew install bufbuild/buf/buf

# 或者直接下载
# https://buf.build/docs/installation
```

### 2.3 安装新的 Go 依赖

```bash
go get google.golang.org/grpc@latest
go get google.golang.org/protobuf@latest
go get go.etcd.io/etcd/client/v3@v3.5.17
go get go.etcd.io/etcd/client/v3/concurrency@v3.5.17
go get github.com/ego-component/eetcd@latest
```

---

## 3. 目录结构（Step 2 新增部分）

```
distributed_task_platform/
├── api/
│   └── proto/
│       ├── executor/v1/
│       │   └── executor.proto          # ✅ 新增：执行器服务定义
│       ├── reporter/v1/
│       │   └── reporter.proto          # ✅ 新增：状态上报服务定义
│       └── gen/                        # ✅ 新增：buf 生成的 Go 代码
│           ├── executor/v1/
│           │   ├── executor.pb.go
│           │   └── executor_grpc.pb.go
│           └── reporter/v1/
│               ├── reporter.pb.go
│               └── reporter_grpc.pb.go
├── buf.gen.yaml                        # ✅ 新增：buf 代码生成配置
├── internal/
│   └── grpc/
│       └── server.go                   # ✅ 新增：ReporterServer 实现
├── pkg/
│   └── grpc/
│       ├── clients_v2.go               # ✅ 新增：泛型 gRPC 客户端池
│       ├── resolver.go                 # ✅ 新增：自定义 gRPC 解析器
│       ├── registry/
│       │   ├── types.go                # ✅ 新增：Registry 接口定义
│       │   └── etcd/
│       │       └── registry.go         # ✅ 新增：etcd 注册中心实现
│       └── balancer/
│           ├── types.go                # ✅ 新增：Context 路由工具
│           ├── builder.go              # ✅ 新增：Balancer 注册
│           ├── routing_balancer.go     # ✅ 新增：自定义负载均衡器
│           └── routing_picker.go       # ✅ 新增：路由选择器
├── ioc/
│   ├── grpc.go                         # ✅ 新增：gRPC Server/Client 初始化
│   └── registry.go                     # ✅ 新增：etcd Registry 初始化
└── config/
    └── config.yaml                     # ✅ 修改：增加 etcd + gRPC 配置
```

---

## 4. 设计决策 & 替代方案对比

在进入实现之前，先理清 Step 2 涉及的几个关键设计决策，以及为什么选择当前方案。

### 4.1 服务发现：etcd vs Consul vs Nacos vs DNS

| 维度 | etcd | Consul | Nacos | CoreDNS/k8s DNS |
|------|------|--------|-------|-----------------|
| **一致性模型** | 强一致（Raft） | 强一致+AP可选 | AP（默认） | 最终一致 |
| **Watch 能力** | 原生 Watch | Blocking Query | 长轮询 | 无 |
| **TTL 租约** | ✅ Lease | ✅ Session + TTL | ✅ 心跳 | ❌ |
| **Go 生态成熟度** | ★★★★★ | ★★★★ | ★★★ | ★★★ |
| **与 gRPC 集成** | 需自定义 Resolver | 需自定义 | 需自定义 | 原生支持 |
| **部署复杂度** | 低（单节点可用） | 中 | 高（需 MySQL） | 依赖 k8s |

**选择 etcd 的理由**：

1. **强一致性保证**：任务调度场景下，我们需要确保执行器注册/注销的可见性是全局一致的。etcd 基于 Raft 的强一致性天然满足。
2. **Lease + Watch 天然匹配**：Lease 实现故障自动摘除，Watch 实现实时感知——这两个恰好是服务发现的核心需求。
3. **Go 生态第一公民**：etcd 本身就是 Go 项目，client SDK 质量极高。
4. **后续复用**：Step 7 分库分表时还会用 etcd 做分布式锁（etcd mutex），一次引入多处复用。

**为什么不用 k8s DNS？** 本项目需要支持非 k8s 部署环境（裸金属、Docker Compose），不能依赖 k8s 基础设施。

### 4.2 通信协议：gRPC vs HTTP vs MQ

| 维度 | gRPC | HTTP REST | MQ（如 Kafka） |
|------|------|-----------|---------------|
| **延迟** | 低（HTTP/2 多路复用） | 中（HTTP/1.1 短连接） | 高（异步） |
| **序列化效率** | Protobuf（二进制） | JSON（文本） | 自定义 |
| **双向通信** | ✅ Stream | ❌ 需 WebSocket | ✅ 但解耦 |
| **类型安全** | ✅ Proto 编译期检查 | ❌ 运行时 | ❌ |
| **连接管理** | 长连接 + 连接池 | 短连接 / Keep-Alive | Broker 管理 |
| **适用场景** | 同步 RPC | 开放 API | 异步事件 |

**选择 gRPC 的理由**：

1. **调度是同步语义**：调度器需要立刻知道"执行请求是否被接受"，这是典型的 RPC 场景。
2. **Protobuf 类型安全**：编译期检查参数类型，避免 JSON 的字段拼写错误。
3. **HTTP/2 连接复用**：一个 TCP 连接可以并行多个 RPC，高并发场景性能优于 HTTP/1.1。
4. **自定义负载均衡**：gRPC 原生支持客户端负载均衡（Resolver + Balancer），可以实现指定节点路由——这在任务重试场景下非常重要（"重试时排除上次失败的节点"）。

**HTTP Invoker 保留的意义**：有些执行节点可能是非 Go 语言的遗留系统，不方便接入 gRPC，通过 HTTP POST 实现"最小代价接入"。

### 4.3 负载均衡：自定义 Picker vs envoy/nginx 代理

| 方案 | 优点 | 缺点 |
|------|------|------|
| **客户端负载均衡（当前方案）** | 无额外代理开销；支持 context 路由（指定/排除节点） | 每个客户端都要维护节点列表 |
| **Envoy/Nginx 代理** | 客户端简单；统一管理 | 多一跳延迟；无法实现 context 路由 |
| **gRPC xDS（Istio）** | 标准化；服务网格集成 | 架构重；依赖 k8s |

**选择客户端负载均衡的关键原因**：**context 路由**。

任务重试时需要"排除上次失败的节点"，任务恢复时需要"指定到原来的节点"。这些路由策略只有客户端负载均衡才能通过 context 元数据传递实现。Envoy/Nginx 无法感知应用层的业务路由语义。

### 4.4 gRPC 客户端管理：懒加载池 vs 单例 vs 每次新建

| 方案 | 连接数 | 适用场景 |
|------|--------|----------|
| **ClientsV2 懒加载池（当前方案）** | 每个 serviceName 一个连接 | 多服务名 + 服务发现 |
| **全局单例连接** | 1 个 | 所有执行器共享一个 serviceName |
| **每次新建** | 无限 | 不可接受（gRPC 连接创建开销大） |

当前方案 `ClientsV2` 使用 `syncx.Map` + `LoadOrStore` 实现线程安全的懒加载：

- 首次访问某个 `serviceName` 时创建 gRPC 连接
- 后续访问直接从缓存返回
- 竞争时只保留第一个连接，关闭多余连接

这很适合"多租户"场景——不同的业务方可以部署不同 serviceName 的执行器集群。

---

## 5. 实现步骤

### 5.1 Protobuf 定义

#### api/proto/executor/v1/executor.proto

```protobuf
syntax = "proto3";
package executor.v1;

option go_package = "gitee.com/flycash/distributed_task_platform/api/proto/gen/executor/v1";

// ExecutorService 是执行器节点对外暴露的 gRPC 服务。
// 调度器通过此服务下发任务执行/中断/查询/预处理请求。
service ExecutorService {
  // Execute 下发任务执行请求
  rpc Execute(ExecuteRequest) returns (ExecuteResponse);
  // Interrupt 中断正在运行的任务
  rpc Interrupt(InterruptRequest) returns (InterruptResponse);
  // Query 查询任务执行状态
  rpc Query(QueryRequest) returns (QueryResponse);
  // Prepare 分片任务预处理（获取业务元数据）
  rpc Prepare(PrepareRequest) returns (PrepareResponse);
}

// ExecutionStatus 执行状态枚举
enum ExecutionStatus {
  UNKNOWN = 0;
  RUNNING = 1;
  FAILED_RETRYABLE = 2;    // 可重试的失败
  FAILED_RESCHEDULABLE = 3; // 需要重调度的失败
  FAILED = 4;              // 不可恢复的失败
  SUCCESS = 5;
}

// ExecutionState 执行节点上报的状态
message ExecutionState {
  int64 id = 1;              // 执行记录 ID
  string task_name = 2;
  ExecutionStatus status = 3;
  int32 progress = 4;        // 执行进度 0-100
  // 重调度参数：执行器可以要求调度器用新参数重新调度
  map<string, string> rescheduled_params = 5;
  string executor_node_id = 6;
  // 是否请求重调度
  bool request_reschedule = 7;
}

// ---- Execute ----
message ExecuteRequest {
  int64 eid = 1;            // 执行记录 ID
  int64 task_id = 2;
  string task_name = 3;
  map<string, string> params = 4;
}
message ExecuteResponse {
  ExecutionState execution_state = 1;
}

// ---- Interrupt ----
message InterruptRequest {
  int64 eid = 1;
  int64 task_id = 2;
  string task_name = 3;
}
message InterruptResponse {}

// ---- Query ----
message QueryRequest {
  int64 eid = 1;
  int64 task_id = 2;
  string task_name = 3;
}
message QueryResponse {
  ExecutionState execution_state = 1;
}

// ---- Prepare ----
message PrepareRequest {
  int64 eid = 1;
  int64 task_id = 2;
  string task_name = 3;
  map<string, string> params = 4;
}
message PrepareResponse {
  map<string, string> params = 1;
}
```

> **设计要点**：
> - `ExecutionStatus` 区分了 `FAILED_RETRYABLE` 和 `FAILED_RESCHEDULABLE`——前者在同节点重试，后者换节点重调度（Step 4 补偿机制消费）。
> - `rescheduled_params` 允许执行器动态修改参数后重调度，这在"数据迁移"类任务中很有用（比如"从 offset=1000 重新开始"）。
> - `request_reschedule` 布尔标志让执行器主动请求重调度（如节点资源不足时）。

#### api/proto/reporter/v1/reporter.proto

```protobuf
syntax = "proto3";
package reporter.v1;

option go_package = "gitee.com/flycash/distributed_task_platform/api/proto/gen/reporter/v1";

import "executor/v1/executor.proto";

// ReporterService 是调度器对外暴露的 gRPC 服务。
// 执行器通过此服务上报任务执行状态。
service ReporterService {
  // Report 单条上报
  rpc Report(ReportRequest) returns (ReportResponse);
  // BatchReport 批量上报
  rpc BatchReport(BatchReportRequest) returns (BatchReportResponse);
}

message ReportRequest {
  executor.v1.ExecutionState execution_state = 1;
}
message ReportResponse {}

message BatchReportRequest {
  repeated ReportRequest reports = 1;
}
message BatchReportResponse {}
```

> **为什么 ReporterService 定义在调度器侧而不是执行器侧？**
>
> 数据流方向：执行器 → 调度器。服务定义跟随数据消费方。
> 调度器作为状态的"权威源"，由它提供接收接口，执行器作为客户端调用。
> 这与 Prometheus 的 push gateway 模式类似——指标由被监控端推送，但接收端定义接口。

#### buf.gen.yaml

```yaml
version: v2
plugins:
  - remote: buf.build/protocolbuffers/go
    out: api/proto/gen
    opt:
      - paths=source_relative
  - remote: buf.build/grpc/go
    out: api/proto/gen
    opt:
      - paths=source_relative
```

```bash
# 生成 Go 代码
cd api/proto && buf generate
```

### 5.2 服务注册中心

#### pkg/grpc/registry/types.go — 接口定义

```go
package registry

import "context"

// EventType 服务变更事件类型
type EventType int

const (
    EventTypeAdd    EventType = iota + 1 // 服务上线
    EventTypeDelete                       // 服务下线
)

// Event 服务变更事件
type Event struct {
    Type     EventType
    Instance ServiceInstance
}

// ServiceInstance 服务实例信息
type ServiceInstance struct {
    Name    string `json:"name"`
    Address string `json:"address"`
    ID      string `json:"id"`     // 全局唯一节点 ID
    Weight  int    `json:"weight"` // 权重（预留）

    // 容量模型参数（Step 8 Prometheus 智能调度使用）
    InitCapacity int     `json:"init_capacity"`
    MaxCapacity  int     `json:"max_capacity"`
    IncreaseStep int     `json:"increase_step"`
    GrowthRate   float64 `json:"growth_rate"`
}

// Registry 服务注册中心接口
type Registry interface {
    // Register 注册服务实例（绑定租约，故障自动摘除）
    Register(ctx context.Context, si ServiceInstance) error
    // UnRegister 主动注销服务实例
    UnRegister(ctx context.Context, si ServiceInstance) error
    // ListServices 查询指定服务名的所有在线实例
    ListServices(ctx context.Context, name string) ([]ServiceInstance, error)
    // Subscribe 订阅服务变更事件（Watch 模式）
    Subscribe(name string) <-chan Event
    // Close 关闭注册中心，释放资源
    Close() error
}
```

> **接口设计原则**：Registry 是一个抽象接口，不绑定 etcd。如果未来要切换到 Consul 或 Nacos，只需实现这个接口。
>
> **ServiceInstance 的容量模型字段**：`InitCapacity`、`MaxCapacity`、`IncreaseStep`、`GrowthRate` 是为 Step 8 的 Prometheus 智能调度预留的。Step 2 不使用，但提前在注册数据中携带，避免后续做数据迁移。

#### pkg/grpc/registry/etcd/registry.go — etcd 实现

```go
package etcd

import (
    "context"
    "encoding/json"
    "fmt"
    "sync"

    "gitee.com/flycash/distributed_task_platform/pkg/grpc/registry"
    "github.com/ego-component/eetcd"
    "go.etcd.io/etcd/api/v3/mvccpb"
    clientv3 "go.etcd.io/etcd/client/v3"
    "go.etcd.io/etcd/client/v3/concurrency"
)

const defaultPrefix = "/services/task_platform/executor"

var typesMap = map[mvccpb.Event_EventType]registry.EventType{
    mvccpb.PUT:    registry.EventTypeAdd,
    mvccpb.DELETE: registry.EventTypeDelete,
}

type Registry struct {
    sess        *concurrency.Session
    client      *eetcd.Component
    mutex       sync.RWMutex
    watchCancel []func()
}

func NewRegistry(c *eetcd.Component) (*Registry, error) {
    sess, err := concurrency.NewSession(c.Client)
    if err != nil {
        return nil, err
    }
    return &Registry{
        sess:   sess,
        client: c,
    }, nil
}

// Register 注册服务实例到 etcd
// Key: /services/task_platform/executor/{name}/{address}
// Value: ServiceInstance JSON
// 绑定 Session 租约，节点异常退出时 key 自动删除
func (r *Registry) Register(ctx context.Context, si registry.ServiceInstance) error {
    val, err := json.Marshal(si)
    if err != nil {
        return err
    }
    _, err = r.client.Put(ctx, r.instanceKey(si),
        string(val), clientv3.WithLease(r.sess.Lease()))
    return err
}

func (r *Registry) instanceKey(s registry.ServiceInstance) string {
    return fmt.Sprintf("%s/%s/%s", defaultPrefix, s.Name, s.Address)
}

func (r *Registry) UnRegister(ctx context.Context, si registry.ServiceInstance) error {
    _, err := r.client.Delete(ctx, r.instanceKey(si))
    return err
}

func (r *Registry) ListServices(ctx context.Context,
    name string) ([]registry.ServiceInstance, error) {
    resp, err := r.client.Get(ctx, r.serviceKey(name), clientv3.WithPrefix())
    if err != nil {
        return nil, err
    }
    res := make([]registry.ServiceInstance, 0, len(resp.Kvs))
    for _, kv := range resp.Kvs {
        var si registry.ServiceInstance
        err = json.Unmarshal(kv.Value, &si)
        if err != nil {
            return nil, err
        }
        res = append(res, si)
    }
    return res, nil
}

func (r *Registry) serviceKey(name string) string {
    return fmt.Sprintf("%s/%s", defaultPrefix, name)
}

// Subscribe 订阅服务变更事件
// 使用 WithRequireLeader 确保只在 etcd leader 上 Watch（防止脑裂场景）
func (r *Registry) Subscribe(name string) <-chan registry.Event {
    ctx, cancel := context.WithCancel(context.Background())
    ctx = clientv3.WithRequireLeader(ctx)
    r.mutex.Lock()
    r.watchCancel = append(r.watchCancel, cancel)
    r.mutex.Unlock()

    ch := r.client.Watch(ctx, r.serviceKey(name), clientv3.WithPrefix())
    res := make(chan registry.Event)
    go func() {
        for {
            select {
            case resp := <-ch:
                if resp.Canceled {
                    return
                }
                if resp.Err() != nil {
                    continue
                }
                for _, event := range resp.Events {
                    res <- registry.Event{
                        Type: typesMap[event.Type],
                    }
                }
            case <-ctx.Done():
                return
            }
        }
    }()
    return res
}

func (r *Registry) Close() error {
    r.mutex.Lock()
    for _, cancel := range r.watchCancel {
        cancel()
    }
    r.mutex.Unlock()
    return r.sess.Close()
}
```

> **关键实现细节解析**：
>
> 1. **`concurrency.Session` 的作用**：Session 内部维护一个带 TTL 的 Lease，并自动续期（KeepAlive）。服务注册时绑定这个 Lease——如果进程崩溃，KeepAlive 停止，Lease 过期，etcd 自动删除对应的 key，实现"故障自动摘除"。
>
> 2. **`WithRequireLeader` 的意义**：在 etcd 集群出现网络分区（脑裂）时，少数派节点可能仍在响应读请求但数据已过时。`WithRequireLeader` 确保 Watch 只在 leader 节点上生效，避免收到不一致的事件。
>
> 3. **Subscribe 返回 channel 而非回调函数**：Go 的 channel 是天然的异步通知机制，与 `select` 配合可以优雅地处理多路复用（如同时监听 close 信号和事件通知）。

### 5.3 自定义 gRPC Resolver

Resolver 的职责是把 etcd 的服务实例列表转换成 gRPC 能理解的 `resolver.Address` 列表。

#### pkg/grpc/resolver.go

```go
package grpc

import (
    "time"

    "gitee.com/flycash/distributed_task_platform/pkg/grpc/registry"
    "golang.org/x/net/context"
    "google.golang.org/grpc/attributes"
    "google.golang.org/grpc/resolver"
)

// resolverBuilder 创建自定义 gRPC 解析器
// URI scheme: "executor"，示例：executor:///my-service
type resolverBuilder struct {
    r       registry.Registry
    timeout time.Duration
}

func NewResolverBuilder(r registry.Registry, timeout time.Duration) resolver.Builder {
    return &resolverBuilder{r: r, timeout: timeout}
}

func (r *resolverBuilder) Build(target resolver.Target,
    cc resolver.ClientConn, _ resolver.BuildOptions) (resolver.Resolver, error) {
    res := &executorResolver{
        target:   target,
        cc:       cc,
        registry: r.r,
        close:    make(chan struct{}, 1),
        timeout:  r.timeout,
    }
    res.resolve()     // 首次主动拉取全量
    go res.watch()    // 后台监听增量变更
    return res, nil
}

func (r *resolverBuilder) Scheme() string { return "executor" }

// executorResolver 自定义 gRPC 解析器
type executorResolver struct {
    target   resolver.Target
    cc       resolver.ClientConn
    registry registry.Registry
    close    chan struct{}
    timeout  time.Duration
}

func (g *executorResolver) ResolveNow(_ resolver.ResolveNowOptions) {
    g.resolve()
}

func (g *executorResolver) Close() {
    g.close <- struct{}{}
}

// watch 后台监听 etcd 变更事件
func (g *executorResolver) watch() {
    events := g.registry.Subscribe(g.target.Endpoint())
    for {
        select {
        case <-events:
            g.resolve() // 收到事件后重新拉取全量
        case <-g.close:
            return
        }
    }
}

// resolve 从 etcd 拉取全量服务实例，转换为 gRPC Address 列表
func (g *executorResolver) resolve() {
    serviceName := g.target.Endpoint()
    ctx, cancel := context.WithTimeout(context.Background(), g.timeout)
    instances, err := g.registry.ListServices(ctx, serviceName)
    cancel()
    if err != nil {
        g.cc.ReportError(err)
    }

    address := make([]resolver.Address, 0, len(instances))
    for _, ins := range instances {
        address = append(address, resolver.Address{
            Addr:       ins.Address,
            ServerName: ins.Name,
            // 通过 Attributes 传递业务元数据给 Balancer
            Attributes: attributes.New("initCapacity", ins.InitCapacity).
                WithValue("maxCapacity", ins.MaxCapacity).
                WithValue("increaseStep", ins.IncreaseStep).
                WithValue("growthRate", ins.GrowthRate).
                WithValue("nodeID", ins.ID),
        })
    }
    err = g.cc.UpdateState(resolver.State{Addresses: address})
    if err != nil {
        g.cc.ReportError(err)
    }
}
```

> **为什么收到事件后是"重新拉取全量"而不是"增量更新"？**
>
> 简化实现 + 保证一致性。etcd 的 Watch 事件可能因网络抖动丢失。如果只做增量更新，丢失一个 DELETE 事件就会导致 Resolver 持有一个已下线的地址。重新拉取全量虽然多了一次 Get，但保证了地址列表始终与 etcd 一致。对于服务发现场景，服务实例数通常在几十到几百级别，全量拉取的开销可以忽略。

### 5.4 自定义 RoutingRoundRobin Balancer

这是 Step 2 最核心的创新点——支持三级路由策略的负载均衡器。

#### 三级路由策略

```
                        Pick 请求
                            │
                    ┌───────▼───────┐
                    │ context 中有    │
                    │ SpecificNodeID?│
                    └───────┬───────┘
                       Yes  │  No
                    ┌───────┘  └──────────┐
                    ▼                     ▼
            精确匹配指定节点        ┌──────────────┐
            找不到 → Error         │ context 中有   │
                                  │ExcludedNodeID?│
                                  └──────┬───────┘
                                    Yes  │  No
                                ┌────────┘  └───────┐
                                ▼                   ▼
                        在排除指定节点后          标准轮询
                        的候选集内轮询
                        全被排除 → 降级轮询
```

**使用场景**：
- **SpecificNodeID**：任务恢复场景——"上次在节点 A 执行到一半，恢复后继续发到节点 A"
- **ExcludedNodeID**：重试场景——"上次在节点 A 失败了，重试时排除节点 A"
- **标准轮询**：正常调度——均匀分配到所有可用节点

#### pkg/grpc/balancer/types.go — Context 路由工具

```go
package balancer

import "context"

const RoutingRoundRobinName = "routing_round_robin"

type contextKey string

const (
    ExcludedNodeIDContextKey contextKey = "excluded_node_id"
    SpecificNodeIDContextKey contextKey = "specific_node_id"
)

// WithExcludedNodeID 在 context 中设置要排除的节点 ID
func WithExcludedNodeID(ctx context.Context, nodeID string) context.Context {
    if nodeID == "" { return ctx }
    return context.WithValue(ctx, ExcludedNodeIDContextKey, nodeID)
}

func GetExcludeNode(ctx context.Context) (string, bool) {
    nodeID, ok := ctx.Value(ExcludedNodeIDContextKey).(string)
    return nodeID, ok && nodeID != ""
}

// WithSpecificNodeID 在 context 中设置要指定的节点 ID
func WithSpecificNodeID(ctx context.Context, nodeID string) context.Context {
    if nodeID == "" { return ctx }
    return context.WithValue(ctx, SpecificNodeIDContextKey, nodeID)
}

func GetSpecificNodeID(ctx context.Context) (string, bool) {
    nodeID, ok := ctx.Value(SpecificNodeIDContextKey).(string)
    return nodeID, ok && nodeID != ""
}
```

> **通过 context 传递路由信息**是 gRPC Go 客户端负载均衡的标准做法。gRPC 的 `Picker.Pick(info)` 方法可以通过 `info.Ctx` 获取调用方的 context，从而读取业务层注入的路由提示。

#### pkg/grpc/balancer/builder.go — Balancer 注册

```go
package balancer

import "google.golang.org/grpc/balancer"

type routingBalancerBuilder struct{}

func init() {
    // 在包初始化时注册到 gRPC 的全局 Balancer 注册表
    balancer.Register(&routingBalancerBuilder{})
}

func (b *routingBalancerBuilder) Build(cc balancer.ClientConn,
    _ balancer.BuildOptions) balancer.Balancer {
    return newRoutingBalancer(cc)
}

func (b *routingBalancerBuilder) Name() string {
    return RoutingRoundRobinName
}
```

#### pkg/grpc/balancer/routing_balancer.go — 核心 Balancer

```go
package balancer

import (
    "sync"

    "google.golang.org/grpc/balancer"
    "google.golang.org/grpc/balancer/base"
    "google.golang.org/grpc/connectivity"
    "google.golang.org/grpc/resolver"
)

type routingBalancer struct {
    cc          balancer.ClientConn
    mu          sync.RWMutex
    subConnMap  map[resolver.Address]balancer.SubConn // 地址 → 子连接
    scToAddrMap map[balancer.SubConn]resolver.Address // 子连接 → 地址（反向映射）
    nodeIDMap   map[resolver.Address]string           // 地址 → 节点ID
    readySCs    map[balancer.SubConn]string           // 就绪的子连接 → 节点ID
}

// UpdateClientConnState 在 Resolver 更新地址列表时被 gRPC 框架调用
func (b *routingBalancer) UpdateClientConnState(state balancer.ClientConnState) error {
    b.mu.Lock()
    defer b.mu.Unlock()

    newAddrs := make(map[resolver.Address]struct{})
    for _, addr := range state.ResolverState.Addresses {
        newAddrs[addr] = struct{}{}
    }

    // 移除已下线的连接
    for addr, sc := range b.subConnMap {
        if _, ok := newAddrs[addr]; !ok {
            sc.Shutdown()
            delete(b.subConnMap, addr)
            delete(b.scToAddrMap, sc)
            delete(b.nodeIDMap, addr)
        }
    }

    // 添加新上线的连接
    for addr := range newAddrs {
        if _, ok := b.subConnMap[addr]; ok {
            continue
        }
        sc, err := b.cc.NewSubConn([]resolver.Address{addr},
            balancer.NewSubConnOptions{})
        if err != nil {
            continue
        }
        b.subConnMap[addr] = sc
        b.scToAddrMap[sc] = addr
        b.nodeIDMap[addr] = b.extractNodeID(addr)
        sc.Connect() // 异步触发 UpdateSubConnState
    }
    return nil
}

// UpdateSubConnState 在子连接状态变化时被 gRPC 框架调用
func (b *routingBalancer) UpdateSubConnState(sc balancer.SubConn,
    state balancer.SubConnState) {
    b.mu.Lock()
    defer b.mu.Unlock()

    addr, ok := b.scToAddrMap[sc]
    if !ok { return }

    switch state.ConnectivityState {
    case connectivity.Ready:
        b.readySCs[sc] = b.nodeIDMap[addr]
    case connectivity.Idle, connectivity.Connecting, connectivity.TransientFailure:
        delete(b.readySCs, sc)
    case connectivity.Shutdown:
        delete(b.subConnMap, addr)
        delete(b.scToAddrMap, sc)
        delete(b.nodeIDMap, addr)
        delete(b.readySCs, sc)
    }
    b.updatePicker()
}

// updatePicker 根据当前就绪连接集合构建新的 Picker
func (b *routingBalancer) updatePicker() {
    if len(b.readySCs) == 0 {
        b.cc.UpdateState(balancer.State{
            ConnectivityState: connectivity.TransientFailure,
            Picker: base.NewErrPicker(balancer.ErrNoSubConnAvailable),
        })
        return
    }
    readyConns := make([]balancer.SubConn, 0, len(b.readySCs))
    nodeIDs := make([]string, 0, len(b.readySCs))
    for sc, nodeID := range b.readySCs {
        readyConns = append(readyConns, sc)
        nodeIDs = append(nodeIDs, nodeID)
    }
    b.cc.UpdateState(balancer.State{
        ConnectivityState: connectivity.Ready,
        Picker:            newRoutingPicker(readyConns, nodeIDs),
    })
}

func (b *routingBalancer) extractNodeID(addr resolver.Address) string {
    if addr.Attributes != nil {
        if v := addr.Attributes.Value("nodeID"); v != nil {
            if s, ok := v.(string); ok {
                return s
            }
        }
    }
    return addr.Addr // 兜底
}

func (b *routingBalancer) ResolverError(error) {}

func (b *routingBalancer) Close() {
    b.mu.Lock()
    defer b.mu.Unlock()
    for _, sc := range b.subConnMap {
        sc.Shutdown()
    }
    b.subConnMap = nil
    b.scToAddrMap = nil
    b.nodeIDMap = nil
    b.readySCs = nil
}
```

#### pkg/grpc/balancer/routing_picker.go — 路由选择器

```go
package balancer

import (
    "sync/atomic"

    "google.golang.org/grpc/balancer"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
)

type routingPicker struct {
    subConns []balancer.SubConn
    nodeIDs  []string
    next     uint32
}

func (p *routingPicker) Pick(info balancer.PickInfo) (balancer.PickResult, error) {
    if len(p.subConns) == 0 {
        return balancer.PickResult{}, balancer.ErrNoSubConnAvailable
    }

    // 优先级 1：指定节点
    specificNodeID, hasSpecific := GetSpecificNodeID(info.Ctx)
    if hasSpecific {
        for i := range p.nodeIDs {
            if p.nodeIDs[i] == specificNodeID {
                return balancer.PickResult{SubConn: p.subConns[i]}, nil
            }
        }
        return balancer.PickResult{}, status.Errorf(codes.Unavailable,
            "指定的节点不可用: %s", specificNodeID)
    }

    // 优先级 2：排除节点
    excludeNodeID, hasExclude := GetExcludeNode(info.Ctx)
    if !hasExclude || len(p.subConns) == 1 {
        return p.pickRoundRobin(), nil
    }

    candidateIndexes := make([]int, 0, len(p.subConns))
    for i, nodeID := range p.nodeIDs {
        if nodeID != excludeNodeID {
            candidateIndexes = append(candidateIndexes, i)
        }
    }
    if len(candidateIndexes) == 0 {
        // 所有节点都被排除，降级到普通轮询
        return p.pickRoundRobin(), nil
    }

    next := atomic.AddUint32(&p.next, 1)
    idx := int(next-1) % len(candidateIndexes)
    return balancer.PickResult{SubConn: p.subConns[candidateIndexes[idx]]}, nil
}

func (p *routingPicker) pickRoundRobin() balancer.PickResult {
    next := atomic.AddUint32(&p.next, 1)
    idx := int(next-1) % len(p.subConns)
    return balancer.PickResult{SubConn: p.subConns[idx]}
}
```

### 5.5 泛型 gRPC 客户端池

#### pkg/grpc/clients_v2.go

```go
package grpc

import (
    "fmt"
    "time"

    "gitee.com/flycash/distributed_task_platform/pkg/grpc/balancer"
    "gitee.com/flycash/distributed_task_platform/pkg/grpc/registry"
    "github.com/ecodeclub/ekit/syncx"
    "github.com/gotomicro/ego/core/elog"
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"
)

// ClientsV2 泛型 gRPC 客户端池
// 每个 serviceName 对应一个 gRPC 连接，懒加载创建
type ClientsV2[T any] struct {
    clientMap syncx.Map[string, T]
    registry  registry.Registry
    timeout   time.Duration
    creator   func(conn *grpc.ClientConn) T
}

func NewClientsV2[T any](
    registry registry.Registry,
    timeout time.Duration,
    creator func(conn *grpc.ClientConn) T,
) *ClientsV2[T] {
    return &ClientsV2[T]{
        registry: registry,
        timeout:  timeout,
        creator:  creator,
    }
}

func (c *ClientsV2[T]) Get(serviceName string) T {
    // 快速路径
    if client, ok := c.clientMap.Load(serviceName); ok {
        return client
    }

    // 慢速路径：创建新连接
    grpcConn, err := grpc.NewClient(
        fmt.Sprintf("executor:///%s", serviceName),
        grpc.WithResolvers(NewResolverBuilder(c.registry, c.timeout)),
        grpc.WithDefaultServiceConfig(
            fmt.Sprintf(`{"loadBalancingPolicy":%q}`, balancer.RoutingRoundRobinName)),
        grpc.WithTransportCredentials(insecure.NewCredentials()),
    )
    if err != nil {
        panic(err)
    }
    newClient := c.creator(grpcConn)

    // 原子存储：竞争时只保留第一个，关闭多余连接
    actual, loaded := c.clientMap.LoadOrStore(serviceName, newClient)
    if loaded {
        if closeErr := grpcConn.Close(); closeErr != nil {
            elog.DefaultLogger.Warn("关闭多余的gRPC连接失败",
                elog.String("serviceName", serviceName),
                elog.FieldErr(closeErr))
        }
    }
    return actual
}
```

> **连接的完整生命周期**：
> ```
> ClientsV2.Get("my-svc")
>   → grpc.NewClient("executor:///my-svc")
>     → resolverBuilder.Build() 被 gRPC 框架回调
>       → resolve()：从 etcd 拉取实例列表
>       → watch()：后台监听 etcd 变更
>     → routingBalancerBuilder.Build() 被 gRPC 框架回调
>       → 创建 routingBalancer
>       → UpdateClientConnState()：为每个地址创建 SubConn
>       → SubConn.Connect()：异步建立 TCP 连接
>       → UpdateSubConnState(Ready)：更新 Picker
>   → 返回 ExecutorServiceClient
> ```

### 5.6 GRPCInvoker 实现

#### internal/service/invoker/grpc.go

```go
package invoker

import (
    "context"
    "fmt"

    executorv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/executor/v1"
    "gitee.com/flycash/distributed_task_platform/internal/domain"
    "gitee.com/flycash/distributed_task_platform/pkg/grpc"
    "github.com/gotomicro/ego/core/elog"
)

type GRPCInvoker struct {
    grpcClients *grpc.ClientsV2[executorv1.ExecutorServiceClient]
    logger      *elog.Component
}

func NewGRPCInvoker(
    grpcClients *grpc.ClientsV2[executorv1.ExecutorServiceClient],
) *GRPCInvoker {
    return &GRPCInvoker{
        grpcClients: grpcClients,
        logger: elog.DefaultLogger.With(
            elog.FieldComponentName("executor.GRPCInvoker")),
    }
}

func (r *GRPCInvoker) Name() string { return "GRPC" }

// Run 发送 gRPC 执行请求
// context 中可能携带 SpecificNodeID 或 ExcludedNodeID，
// 会被 RoutingPicker 消费实现智能路由
func (r *GRPCInvoker) Run(ctx context.Context,
    exec domain.TaskExecution) (domain.ExecutionState, error) {
    client := r.grpcClients.Get(exec.Task.GrpcConfig.ServiceName)
    resp, err := client.Execute(ctx, &executorv1.ExecuteRequest{
        Eid:      exec.ID,
        TaskId:   exec.Task.ID,
        TaskName: exec.Task.Name,
        Params:   exec.GRPCParams(),
    })
    if err != nil {
        return domain.ExecutionState{},
            fmt.Errorf("发送gRPC请求失败: %w", err)
    }
    return domain.ExecutionStateFromProto(resp.GetExecutionState()), nil
}

// Prepare 分片任务预处理
func (r *GRPCInvoker) Prepare(ctx context.Context,
    exec domain.TaskExecution) (map[string]string, error) {
    client := r.grpcClients.Get(exec.Task.GrpcConfig.ServiceName)
    resp, err := client.Prepare(ctx, &executorv1.PrepareRequest{
        Eid:      exec.ID,
        TaskId:   exec.Task.ID,
        TaskName: exec.Task.Name,
        Params:   exec.GRPCParams(),
    })
    if err != nil {
        return nil, fmt.Errorf("发送gRPC请求失败: %w", err)
    }
    return resp.GetParams(), nil
}
```

### 5.7 Invoker Dispatcher 升级

Step 1 的 Dispatcher 只有 Local，Step 2 升级为三路由：

```go
package invoker

// Dispatcher 路由分发器
// 路由优先级：GrpcConfig → HTTPConfig → LocalInvoker
type Dispatcher struct {
    http  *HTTPInvoker
    grpc  *GRPCInvoker
    local *LocalInvoker
}

func NewDispatcher(http *HTTPInvoker, grpc *GRPCInvoker,
    local *LocalInvoker) *Dispatcher {
    return &Dispatcher{http: http, grpc: grpc, local: local}
}

func (r *Dispatcher) Run(ctx context.Context,
    execution domain.TaskExecution) (domain.ExecutionState, error) {
    switch {
    case execution.Task.GrpcConfig != nil:
        return r.grpc.Run(ctx, execution)
    case execution.Task.HTTPConfig != nil:
        return r.http.Run(ctx, execution)
    default:
        return r.local.Run(ctx, execution)
    }
}

func (r *Dispatcher) Prepare(ctx context.Context,
    execution domain.TaskExecution) (map[string]string, error) {
    switch {
    case execution.Task.GrpcConfig != nil:
        return r.grpc.Prepare(ctx, execution)
    case execution.Task.HTTPConfig != nil:
        return r.http.Prepare(ctx, execution)
    default:
        return r.local.Prepare(ctx, execution)
    }
}
```

> **路由决策在哪一层？** 不是 Runner 决定用哪种 Invoker，而是 Dispatcher 根据 Task 的配置自动路由。Runner 只知道"有一个 Invoker 接口"，不关心底层协议。这遵循了依赖倒置原则。

### 5.8 ReporterServer — 状态上报服务端

```go
package grpc

import (
    "context"

    reporterv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/reporter/v1"
    "gitee.com/flycash/distributed_task_platform/internal/domain"
    "gitee.com/flycash/distributed_task_platform/internal/service/task"
    "github.com/ecodeclub/ekit/slice"
    "github.com/gotomicro/ego/core/elog"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"
)

type ReporterServer struct {
    reporterv1.UnimplementedReporterServiceServer
    execSvc task.ExecutionService
    logger  *elog.Component
}

func NewReporterServer(execSvc task.ExecutionService) *ReporterServer {
    return &ReporterServer{
        execSvc: execSvc,
        logger:  elog.DefaultLogger.With(
            elog.FieldComponentName("scheduler.grpc.ReporterServer")),
    }
}

// Report 单条上报
func (s *ReporterServer) Report(ctx context.Context,
    req *reporterv1.ReportRequest) (*reporterv1.ReportResponse, error) {
    state := req.ExecutionState
    if state == nil {
        return &reporterv1.ReportResponse{}, nil
    }
    err := s.handleReports(ctx,
        s.toDomainReports([]*reporterv1.ReportRequest{req}))
    if err != nil {
        return nil, status.Error(codes.Internal, "处理失败")
    }
    return &reporterv1.ReportResponse{}, nil
}

// BatchReport 批量上报
func (s *ReporterServer) BatchReport(ctx context.Context,
    req *reporterv1.BatchReportRequest) (*reporterv1.BatchReportResponse, error) {
    if len(req.Reports) == 0 {
        return &reporterv1.BatchReportResponse{}, nil
    }
    err := s.handleReports(ctx, s.toDomainReports(req.GetReports()))
    if err != nil {
        return nil, status.Error(codes.Internal, "处理失败")
    }
    return &reporterv1.BatchReportResponse{}, nil
}

func (s *ReporterServer) toDomainReports(
    reqs []*reporterv1.ReportRequest) []*domain.Report {
    return slice.Map(reqs, func(_ int, src *reporterv1.ReportRequest) *domain.Report {
        return &domain.Report{
            ExecutionState: domain.ExecutionStateFromProto(src.GetExecutionState()),
        }
    })
}

func (s *ReporterServer) handleReports(ctx context.Context,
    reports []*domain.Report) error {
    return s.execSvc.HandleReports(ctx, reports)
}
```

### 5.9 IOC 初始化

#### ioc/registry.go

```go
package ioc

import (
    "gitee.com/flycash/distributed_task_platform/pkg/grpc/registry"
    registryEtcd "gitee.com/flycash/distributed_task_platform/pkg/grpc/registry/etcd"
    "github.com/ego-component/eetcd"
)

func InitRegistry() registry.Registry {
    client := eetcd.Load("etcd").Build()
    r, err := registryEtcd.NewRegistry(client)
    if err != nil {
        panic(err)
    }
    return r
}
```

#### ioc/grpc.go

```go
package ioc

import (
    executorv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/executor/v1"
    reporterv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/reporter/v1"
    mygrpc "gitee.com/flycash/distributed_task_platform/internal/grpc"
    pkggrpc "gitee.com/flycash/distributed_task_platform/pkg/grpc"
    "gitee.com/flycash/distributed_task_platform/pkg/grpc/registry"
    "github.com/gotomicro/ego/server/egrpc"
    "google.golang.org/grpc"
    "time"
)

// InitSchedulerNodeGRPCServer 初始化调度器的 gRPC 服务端
// 注册 ReporterService，供执行器上报状态
func InitSchedulerNodeGRPCServer(
    reporterServer *mygrpc.ReporterServer,
    r registry.Registry,
) *egrpc.Component {
    server := egrpc.Load("grpc.scheduler").Build()
    reporterv1.RegisterReporterServiceServer(server.Server, reporterServer)
    // 注入自定义 Resolver（可选：也可以通过 Resolver 做调度器自己的服务发现）
    server.Server.GetServiceInfo() // 触发服务注册
    return server
}

// InitExecutorServiceGRPCClients 初始化执行器的 gRPC 客户端池
func InitExecutorServiceGRPCClients(
    r registry.Registry,
) *pkggrpc.ClientsV2[executorv1.ExecutorServiceClient] {
    return pkggrpc.NewClientsV2[executorv1.ExecutorServiceClient](
        r,
        time.Second,
        func(conn *grpc.ClientConn) executorv1.ExecutorServiceClient {
            return executorv1.NewExecutorServiceClient(conn)
        },
    )
}
```

### 5.10 配置文件增量

```yaml
# config/config.yaml 新增内容

etcd:
  addrs:
    - "127.0.0.1:2379"
  connectTimeout: "1s"

grpc:
  scheduler:
    host: "0.0.0.0"
    port: 9002
  executor:
    host: "0.0.0.0"
    port: 9004
```

### 5.11 NormalTaskRunner 中的路由集成

Step 2 在 Runner 中通过 context 注入路由信息：

```go
// internal/service/runner/normal_task_runner.go（关键变更）

func (r *NormalTaskRunner) handleNormalTask(ctx context.Context,
    task domain.Task) error {
    // ... 抢占 + 创建执行记录 ...

    go func() {
        // 注入路由信息到 context
        invokeCtx := r.WithExcludedNodeIDContext(ctx, execution)
        invokeCtx = r.WithSpecificNodeIDContext(invokeCtx, execution)

        state, err := r.invoker.Run(invokeCtx, execution)
        // ... 处理结果 ...
    }()
    return nil
}

// WithSpecificNodeIDContext 如果 execution 记录了上次的执行节点，
// 指定路由到该节点（用于任务恢复场景）
func (r *NormalTaskRunner) WithSpecificNodeIDContext(
    ctx context.Context, exec domain.TaskExecution) context.Context {
    if exec.ExecutorNodeID != "" {
        return balancer.WithSpecificNodeID(ctx, exec.ExecutorNodeID)
    }
    return ctx
}

// WithExcludedNodeIDContext 如果需要排除某个节点（用于重试场景）
func (r *NormalTaskRunner) WithExcludedNodeIDContext(
    ctx context.Context, exec domain.TaskExecution) context.Context {
    // 重试时排除上次失败的节点（Step 4 补偿机制配合使用）
    if exec.ExcludeNodeID != "" {
        return balancer.WithExcludedNodeID(ctx, exec.ExcludeNodeID)
    }
    return ctx
}
```

---

## 6. 现有实现的不足与优化建议

### 6.1 ClientsV2 的连接泄漏风险

**问题**：`Get()` 方法在竞争时可能创建多余连接，虽然有 `LoadOrStore` + `Close` 兜底，但存在一个边界情况——如果 `Close` 失败且被忽略（当前只是 Warn 日志），连接就泄漏了。

**当前代码**：
```go
actual, loaded := c.clientMap.LoadOrStore(serviceName, newClient)
if loaded {
    if closeErr := grpcConn.Close(); closeErr != nil {
        elog.DefaultLogger.Warn("关闭多余的gRPC连接失败", ...)
    }
}
```

**优化建议**：使用 `sync.Once` 或 `singleflight` 保证每个 serviceName 只创建一次连接，彻底避免多余连接的产生：

```go
import "golang.org/x/sync/singleflight"

type ClientsV2[T any] struct {
    clientMap syncx.Map[string, T]
    registry  registry.Registry
    timeout   time.Duration
    creator   func(conn *grpc.ClientConn) T
    sf        singleflight.Group // 新增
}

func (c *ClientsV2[T]) Get(serviceName string) T {
    if client, ok := c.clientMap.Load(serviceName); ok {
        return client
    }
    // singleflight 保证并发调用只执行一次
    result, _, _ := c.sf.Do(serviceName, func() (any, error) {
        // 双检锁
        if client, ok := c.clientMap.Load(serviceName); ok {
            return client, nil
        }
        grpcConn, err := grpc.NewClient(...)
        if err != nil {
            return nil, err
        }
        newClient := c.creator(grpcConn)
        c.clientMap.Store(serviceName, newClient)
        return newClient, nil
    })
    return result.(T)
}
```

### 6.2 Resolver 的全量拉取效率

**问题**：每次收到 Watch 事件都做全量 `ListServices`。如果执行器集群有 1000 个节点，每次上线/下线一个节点都要拉取 1000 条数据。

**当前方案可接受的原因**：实际生产环境执行器节点数通常在几十到几百，全量拉取开销 < 10ms。

**优化方向**（如果节点数量级到千级别）：

```go
// 方案：Watch 事件携带实例信息，做增量更新
func (g *executorResolver) watch() {
    events := g.registry.Subscribe(g.target.Endpoint())
    for {
        select {
        case event := <-events:
            switch event.Type {
            case registry.EventTypeAdd:
                g.addAddress(event.Instance)
            case registry.EventTypeDelete:
                g.removeAddress(event.Instance)
            }
        case <-g.close:
            return
        }
    }
}
```

但这需要 Subscribe 返回的 Event 携带完整的 `ServiceInstance` 信息。当前实现的 Event 只有 Type，没有 Instance 数据——这是另一个可以优化的点。

### 6.3 Subscribe 返回的 Event 缺少 Instance 信息

**问题**：当前 `Subscribe` 的 Watch 回调只发送了 `EventType`，没有将 etcd 事件中的实例信息解析出来：

```go
// 当前
for _, event := range resp.Events {
    res <- registry.Event{
        Type: typesMap[event.Type],
        // Instance 字段为零值！
    }
}
```

**优化建议**：

```go
for _, event := range resp.Events {
    var instance registry.ServiceInstance
    if event.Type == mvccpb.PUT && event.Kv != nil {
        _ = json.Unmarshal(event.Kv.Value, &instance)
    }
    res <- registry.Event{
        Type:     typesMap[event.Type],
        Instance: instance,
    }
}
```

这样 Resolver 就可以选择做增量更新而非全量拉取。

### 6.4 etcd Session 的 TTL 不可配置

**问题**：`concurrency.NewSession(c.Client)` 使用默认 TTL（60s）。如果网络抖动超过 60s，所有服务实例会被自动摘除。

**优化建议**：

```go
func NewRegistry(c *eetcd.Component, opts ...Option) (*Registry, error) {
    cfg := defaultConfig()
    for _, opt := range opts {
        opt(cfg)
    }
    sess, err := concurrency.NewSession(c.Client,
        concurrency.WithTTL(cfg.SessionTTL))
    // ...
}
```

通过 Option 模式让调用方配置 TTL，适应不同的网络环境。

### 6.5 Balancer 缺少健康检查集成

**问题**：当前 Balancer 只根据 gRPC 连接状态（TCP 层）判断节点是否可用。如果执行器进程假死（TCP 连接还在，但不处理请求），Balancer 无法感知。

**优化方向**：

1. **gRPC 健康检查协议**：使用 `grpc.health.v1.Health` 标准协议，Balancer 定期调用 `Check()` 验证服务可用性。

2. **自定义心跳**：在 `Picker.Pick` 的 `Done` 回调中记录 RPC 成功/失败率，连续失败 N 次后将节点标记为不可用：

```go
func (p *routingPicker) pickRoundRobin() balancer.PickResult {
    next := atomic.AddUint32(&p.next, 1)
    idx := int(next-1) % len(p.subConns)
    return balancer.PickResult{
        SubConn: p.subConns[idx],
        Done: func(info balancer.DoneInfo) {
            if info.Err != nil {
                // 记录失败次数，超过阈值后从就绪池移除
                p.recordFailure(p.nodeIDs[idx])
            }
        },
    }
}
```

### 6.6 ReporterServer 缺少幂等保护

**问题**：如果执行器因为网络超时重试了 `Report` 请求，同一个状态会被处理两次。对于 `SUCCESS` → `SUCCESS` 这种幂等操作没问题，但如果执行器 bug 导致重复上报不同状态，可能会引起状态机混乱。

**优化建议**：在 `HandleReports` 中加入幂等检查：

```go
func (s *executionService) HandleReports(ctx context.Context,
    reports []*domain.Report) error {
    for _, report := range reports {
        // 检查执行记录当前状态是否允许转换
        exec, err := s.execDAO.GetByID(ctx, report.ExecutionState.ID)
        if err != nil { return err }

        if !isValidTransition(exec.Status, report.ExecutionState.Status) {
            s.logger.Warn("无效的状态转换，忽略",
                elog.String("from", exec.Status),
                elog.String("to", string(report.ExecutionState.Status)))
            continue
        }
        // ... 处理状态更新
    }
    return nil
}
```

### 6.7 ClientsV2 没有 Close 方法

**问题**：`ClientsV2` 没有提供 `Close()` 方法来清理所有 gRPC 连接。当调度器优雅关闭时，连接不会被主动断开（虽然 OS 会回收，但不优雅）。

**优化建议**：

```go
func (c *ClientsV2[T]) Close() error {
    var errs []error
    c.clientMap.Range(func(key string, val T) bool {
        // 需要在创建时同时保存 grpcConn 引用
        // 或者让 T 实现 io.Closer 接口
        return true
    })
    return errors.Join(errs...)
}
```

实现这个需要 `ClientsV2` 同时保存 `grpc.ClientConn` 的引用，不仅仅是 `T`。可以用一个 wrapper struct：

```go
type connEntry[T any] struct {
    client T
    conn   *grpc.ClientConn
}

type ClientsV2[T any] struct {
    clientMap syncx.Map[string, connEntry[T]]
    // ...
}
```

---

## 7. 部署验证

### 7.1 启动基础设施

```bash
# 启动 MySQL + etcd
docker compose up -d mysql etcd

# 验证 etcd 就绪
etcdctl --endpoints=http://127.0.0.1:2379 endpoint health
```

### 7.2 插入测试任务（gRPC 类型）

```bash
mysql -h 127.0.0.1 -P 13316 -u root -proot task -e "
INSERT INTO tasks (name, cron_expr, next_time, status, version,
    execution_method, type, max_execution_seconds,
    grpc_config, ctime, utime)
VALUES (
    'test_grpc_task',
    '*/30 * * * * ?',
    UNIX_TIMESTAMP(NOW())*1000,
    'ACTIVE',
    1,
    'REMOTE',
    'normal',
    86400,
    '{\"service_name\": \"longrunning\", \"method\": \"Execute\"}',
    UNIX_TIMESTAMP(NOW())*1000,
    UNIX_TIMESTAMP(NOW())*1000
);
"
```

### 7.3 启动执行器（示例）

```bash
# 编写一个最小执行器（example/longrunning/main.go）
# 1. 实现 ExecutorService gRPC Server
# 2. 启动时注册到 etcd
# 3. 收到 Execute 请求后模拟执行 5s，返回 SUCCESS

cd example/longrunning && go run main.go
```

### 7.4 启动调度器

```bash
make run
```

### 7.5 期望看到的日志

**执行器侧**：
```
[INFO]  已注册到 etcd  serviceName=longrunning  address=192.168.1.100:9004
[INFO]  收到执行请求  eid=1  taskName=test_grpc_task
[INFO]  执行完成  eid=1  status=SUCCESS
```

**调度器侧**：
```
[INFO]  启动调度器  nodeID=xxx
[INFO]  发现可调度任务  count=1
[INFO]  gRPC 执行请求发送成功  serviceName=longrunning  eid=1
```

### 7.6 验证 etcd 服务注册

```bash
# 查看注册的服务
etcdctl --endpoints=http://127.0.0.1:2379 get \
    /services/task_platform/executor --prefix --keys-only

# 期望输出：
# /services/task_platform/executor/longrunning/192.168.1.100:9004

# 查看实例详情
etcdctl --endpoints=http://127.0.0.1:2379 get \
    /services/task_platform/executor --prefix
```

### 7.7 验证故障自动摘除

```bash
# 1. 直接 kill 执行器进程（模拟宕机）
kill -9 <executor_pid>

# 2. 等待约 60s（etcd Lease TTL 默认值）

# 3. 查看 etcd 中的注册信息
etcdctl --endpoints=http://127.0.0.1:2379 get \
    /services/task_platform/executor --prefix --keys-only

# 期望：对应的 key 已消失
```

---

## 8. 验收标准

- [ ] `buf generate` 无错误，生成的 Go 代码可编译
- [ ] `go build ./...` 零错误
- [ ] 执行器启动后在 etcd 中可见注册信息
- [ ] 调度器能通过 gRPC 成功下发任务到执行器
- [ ] 执行器能通过 ReporterService 上报状态
- [ ] 数据库中 `task_executions` 状态正确更新
- [ ] 执行器正常退出后 etcd 中注册信息被清除
- [ ] 执行器异常退出（kill -9）后，等待 Lease TTL 过期，注册信息被自动清除
- [ ] 启动多个执行器，任务能被轮询分配到不同节点
- [ ] Step 1 的 LOCAL 执行方式仍然正常工作

---

## 9. 关键设计决策 & 面试话术

### 9.1 为什么用自定义 Resolver + Balancer 而不是 gRPC-go 内置的？

> "gRPC-go 内置的 `dns_resolver` 只支持 DNS SRV 记录做服务发现，不支持 etcd Watch 实时感知变更。
> 内置的 `round_robin` 和 `pick_first` 策略也不支持业务层的路由语义。
> 我需要两个核心能力：一是 etcd Watch 驱动的实时地址更新，二是通过 context 传递'指定节点/排除节点'的路由提示。
> 所以自定义了 Resolver（etcd → gRPC Address）和 Balancer（三级路由 Picker）。
> gRPC 的 Resolver + Balancer 是插件式设计，自定义实现只需要实现 `resolver.Builder` 和 `balancer.Builder` 两个接口。"

### 9.2 为什么 Picker 要支持排除节点？

> "这是为了任务重试场景。比如任务在节点 A 执行失败了，标记为 FAILED_RETRYABLE。
> 补偿器发起重试时，会在 context 中注入 `ExcludedNodeID=A`。
> RoutingPicker 在 Pick 时会排除节点 A，选择其他节点执行。
> 这就是'换节点重试'的实现原理——不需要修改任何重试逻辑，只需要在 context 中设置一个 key。"

### 9.3 ClientsV2 的泛型设计有什么好处？

> "ClientsV2 是一个泛型客户端池 `ClientsV2[T any]`，T 可以是 `ExecutorServiceClient`，也可以是任何 gRPC Service Client。
> 好处是：如果未来有新的 gRPC 服务（比如 MonitorService），不需要再写一个客户端池，直接 `NewClientsV2[MonitorServiceClient]` 就行。
> 核心逻辑（懒加载、服务发现、负载均衡）被复用，只有工厂函数不同。"

### 9.4 为什么服务注册用 etcd Lease 而不是自己实现心跳？

> "etcd 的 Lease + KeepAlive 机制是由 etcd 客户端 SDK 自动维护的，代码量为零。
> 如果自己实现心跳，需要在执行器端写定时器、在调度器端写超时检测、还要处理时钟偏差——这些 etcd 都帮你做了。
> `concurrency.Session` 创建时绑定一个带 TTL 的 Lease，SDK 自动续期。
> 服务注册时 `clientv3.WithLease(sess.Lease())` 把 KV 和 Lease 绑定。
> 进程崩溃 → KeepAlive 停止 → Lease 过期 → KV 自动删除 → Watch 感知到 DELETE 事件。
> 整个链路不需要写任何心跳代码。"

### 9.5 如果 etcd 挂了怎么办？

> "分两种情况：
>
> 1. **短暂故障（几秒到几十秒）**：etcd 客户端有内置重试。Resolver 的 Watch 会在 etcd 恢复后自动重连。已建立的 gRPC 连接不受影响（连接是直连执行器的，不经过 etcd）。
>
> 2. **长时间故障**：新的执行器无法注册，已注册的执行器因为 Lease 无法续期会过期被摘除。调度器会逐渐失去可用的执行节点。
>    这时候 LocalInvoker 作为降级方案就有意义了——可以在配置中将关键任务的 `execution_method` 临时切回 `LOCAL`。
>
> 核心原则是：etcd 是服务发现的控制面，不在数据面的关键路径上。已经建立的连接不依赖 etcd 存活。"

---

## 10. 下一步（Step 3 预览）

Step 2 完成后，调度器已经能通过 gRPC 远程下发任务。但状态上报目前是同步的（ReporterService gRPC 调用）。Step 3 的目标是：

- 引入 Kafka MQ，实现异步事件上报
- 调度器消费 Kafka 事件更新执行状态
- 解耦执行器和调度器的状态同步链路
- 支持事件回放和审计日志

新增依赖：Kafka + sarama（Go Kafka SDK）
