# 临时文档归档索引

> 本目录整理自 `temp/` 目录下的所有临时文档和资源，按主题分类归档。

---

## 📁 目录结构

```
docs/tmp/
├── README.md                  # 本索引文件
├── event-driven/              # 事件驱动改造相关（5个文件）
├── performance/               # 性能优化相关（3个文件）
├── resume/                    # 简历/求职相关（5个文件）
├── tech-intro/                # 技术介绍相关（1个文件）
└── images/                    # 图片资源（5个文件）
```

---

## 📂 一、事件驱动改造 (`event-driven/`)

与分布式任务平台的事件驱动架构改造相关的分析、方案和实施指南。

| 文件 | 说明 | 关键内容 |
|------|------|---------|
| [event_driven_analysis.md](./event-driven/event_driven_analysis.md) | 事件驱动改造分析报告 | 全面分析可改造模块（调度器、补偿器、健康检查等），给出优先级矩阵和改造顺序建议 |
| [scheduler_event_driven_guide.md](./event-driven/scheduler_event_driven_guide.md) | 调度器事件驱动改造完整指南 | 核心组件实现（事件定义、生产者、调度器、工作协程池）、迁移方案（三阶段灰度）、最佳实践 |
| [scheduler_event_driven_summary.md](./event-driven/scheduler_event_driven_summary.md) | 调度器事件驱动改造方案总结 | 改造方案的精简版总结，包含核心架构、实现要点、迁移步骤和预期收益 |
| [event_driven_timing_solution.md](./event-driven/event_driven_timing_solution.md) | 事件驱动模式下的定时调度解决方案 | 解决"如何确保任务在指定时间被调度"的核心问题，提出混合模式（立即事件+时间轮+轮询兜底） |
| [event_driven_timing_summary.md](./event-driven/event_driven_timing_summary.md) | 事件驱动定时调度方案总结 | 定时调度方案的精简版，包含智能分发策略、时间轮实现、三层保障机制 |

### 阅读建议

```
入门：scheduler_event_driven_summary.md（快速了解改造方案）
     ↓
深入：event_driven_analysis.md（了解全部可改造模块）
     ↓
实施：scheduler_event_driven_guide.md（完整实施指南）
     ↓
定时调度：event_driven_timing_summary.md → event_driven_timing_solution.md
```

---

## 📂 二、性能优化 (`performance/`)

与系统性能提升和优化建议相关的分析报告。

| 文件 | 说明 | 关键内容 |
|------|------|---------|
| [performance_improvement_analysis.md](./performance/performance_improvement_analysis.md) | 性能提升详细分析 | 当前系统性能基准、优化后预测（调度延迟↓93%、数据库QPS↓98.4%）、成本效益分析、不同负载场景对比 |
| [performance_improvement_summary.md](./performance/performance_improvement_summary.md) | 性能提升总结 | 核心指标可视化对比、实施时间线、关键技术点、风险与挑战 |
| [system_optimization_recommendations.md](./performance/system_optimization_recommendations.md) | 系统优化建议 | 全面的优化建议（P0-P2），涵盖事件驱动改造、多级缓存、连接池优化、全链路追踪、智能预测调度等 |

### 阅读建议

```
概览：performance_improvement_summary.md（快速了解性能提升数据）
  ↓
详细：performance_improvement_analysis.md（深入分析每个指标）
  ↓
规划：system_optimization_recommendations.md（完整优化路线图）
```

---

## 📂 三、简历/求职 (`resume/`)

与项目简历描述、求职策略相关的文档。

| 文件 | 说明 | 关键内容 |
|------|------|---------|
| [简历项目描述.md](./resume/简历项目描述.md) | 简历项目描述模板 | 通知平台和分布式任务系统两个项目的完整简历描述，包含核心职责、技术亮点、数据指标 |
| [简历项目结合方案.md](./resume/简历项目结合方案.md) | 大数据平台背景与项目结合方案 | 如何将大数据平台经验与toC项目结合、面试话术、自我介绍模板、常见问题应对策略 |
| [项目选择.md](./resume/项目选择.md) | 项目选择分析 | 四个候选项目（通知平台、分布式任务系统、WebSocket网关、权限系统）的对比分析和推荐 |
| [智能客服平台简历项目.md](./resume/智能客服平台简历项目.md) | 智能客服平台项目描述（完整版） | Python高性能编程+AI Agent+MCP协议的企业级智能客服平台完整项目描述，含面试准备 |
| [智能客服平台简历项目_精简版.md](./resume/智能客服平台简历项目_精简版.md) | 智能客服平台项目描述（精简版） | 智能客服平台的精简版描述，适合直接放入简历 |

### 阅读建议

```
选项目：项目选择.md（确定简历项目组合）
  ↓
写简历：简历项目描述.md（通知平台+任务系统模板）
  ↓
结合背景：简历项目结合方案.md（与大数据平台经验结合）
  ↓
AI项目（可选）：智能客服平台简历项目_精简版.md
```

---

## 📂 四、技术介绍 (`tech-intro/`)

技术概念介绍和科普类文档。

| 文件 | 说明 | 关键内容 |
|------|------|---------|
| [分布式调度系统介绍与应用.md](./tech-intro/分布式调度系统介绍与应用.md) | 分布式调度系统介绍 | 分布式调度系统概念、核心组件、6大典型应用场景（大数据ETL、电商、金融、社交、运维、视频）、主流系统对比、订单超时取消技术选型分析 |

---

## 📂 五、图片资源 (`images/`)

架构图和系统设计图。

| 文件 | 说明 |
|------|------|
| 分布式调度系统1.png | 分布式调度系统架构图（一） |
| 分布式调度系统2.png | 分布式调度系统架构图（二） |
| 权限系统.png | 权限系统架构图 |
| 网关系统.png | WebSocket网关系统架构图 |
| 通知平台.png | 通知平台架构图 |

---

## 📊 文件统计

| 分类 | 文件数 | 总大小 |
|------|--------|--------|
| 事件驱动改造 | 5 | ~93 KB |
| 性能优化 | 3 | ~51 KB |
| 简历/求职 | 5 | ~123 KB |
| 技术介绍 | 1 | ~26 KB |
| 图片资源 | 5 | ~2.3 MB |
| **合计** | **19** | **~2.6 MB** |
