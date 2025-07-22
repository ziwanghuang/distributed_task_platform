# Picker 智能调度集成测试

本目录包含智能调度功能的集成测试，测试使用真实的 Prometheus 服务来验证节点选择逻辑。

## 前置条件

1. **启动 Prometheus 服务**：
   ```bash
   # 在项目根目录下运行
   docker-compose -f scripts/test_docker_compose.yml up prometheus
   ```

2. **确认 Prometheus 可访问**：
   - 浏览器访问 `http://localhost:9090`
   - 确认 Prometheus 界面正常显示

## 运行测试

```bash
# 在项目根目录下运行
go test -tags=e2e ./internal/test/integration/picker -v -timeout=5m
```

## 测试说明

### 测试架构

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   测试代码       │    │   真实Prometheus  │    │  模拟执行节点    │
│                │    │   (Docker)       │    │  (Push Mode)   │
│ PickerSuite    │───▶│  localhost:9090  │◄───│  每5秒推送指标   │
│                │    │  Remote Write    │    │                │
│                │    │     API         │    │                │
└─────────────────┘    └──────────────────┘    └─────────────────┘
```

### 模拟节点

测试会启动6个模拟执行节点，使用 **推送模式** 向 Prometheus 发送指标：

- **CPU 指标节点**：
  - `cpu-node-01`: 推送 `executor_cpu_idle_percent` 指标
  - `cpu-node-02`: 推送 `executor_cpu_idle_percent` 指标  
  - `cpu-node-03`: 推送 `executor_cpu_idle_percent` 指标

- **内存指标节点**：
  - `memory-node-01`: 推送 `executor_memory_available_bytes` 指标
  - `memory-node-02`: 推送 `executor_memory_available_bytes` 指标
  - `memory-node-03`: 推送 `executor_memory_available_bytes` 指标

**推送频率**: 每5秒通过 Prometheus Remote Write API 推送一次

### 测试用例

1. **TestCPUPriorityStrategy**：
   - 验证 CPU 优先策略选择 CPU 空闲率最高的节点
   - 设置不同的 CPU 空闲率，验证选择逻辑

2. **TestMemoryPriorityStrategy**：
   - 验证内存优先策略选择可用内存最多的节点
   - 设置不同的内存大小，验证选择逻辑

3. **TestFallbackStrategy**：
   - 验证无效配置时的降级策略
   - 确保系统有良好的容错性

4. **TestRealPrometheusConnection**：
   - 验证与真实 Prometheus 的连接
   - 确保测试环境配置正确

## 手动验证

### Prometheus 查询

在 Prometheus UI (`http://localhost:9090`) 中执行查询：

```promql
# 查看所有 CPU 指标
executor_cpu_idle_percent

# 查看所有内存指标
executor_memory_available_bytes

# 查看节点存活状态
up{job="executors"}

# 测试 topk 查询（智能调度实际使用的查询）
topk(2, avg_over_time(executor_cpu_idle_percent[30s]) AND ON(instance) up{job="executors"} == 1)
```

## 故障排查

### Prometheus 连接失败

如果测试提示 Prometheus 连接失败：

1. 确认 Docker 容器正在运行：
   ```bash
   docker ps | grep prometheus
   ```

2. 检查端口是否被占用：
   ```bash
   lsof -i :9090
   ```

3. 重启 Prometheus：
   ```bash
   docker-compose -f scripts/test_docker_compose.yml restart prometheus
   ```

### 节点指标未出现

如果 Prometheus 中看不到节点指标：

1. 检查测试是否正在运行：
   - 模拟节点通过 Remote Write API 推送指标
   - 确保测试套件正在运行且未出错

2. 验证 Prometheus 支持 Remote Write：
   - 确认 Docker Compose 中的 Prometheus 启用了 `--web.enable-remote-write-receiver`
   - 查看 Prometheus 日志是否有错误信息

3. 手动验证 Remote Write API：
   ```bash
   # 检查 Prometheus Remote Write 端点是否可用
   curl -I http://localhost:9090/api/v1/write
   ```

## 配置文件

- `scripts/prometheus/prometheus.yml`: Prometheus 配置
- `scripts/test_docker_compose.yml`: Docker Compose 配置
- `internal/test/integration/ioc/prometheus.go`: Prometheus 客户端初始化 