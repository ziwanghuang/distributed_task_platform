# 13 — DAG 工作流引擎文档

> DSL 语法规范、ANTLR4 解析器、DAG 图构建、执行推进和事件驱动机制。

---

## 目录

- [1. DSL 语法规范](#1-dsl-语法规范)
- [2. ANTLR4 解析器](#2-antlr4-解析器)
- [3. Node 类型系统](#3-node-类型系统)
- [4. DAG 图构建流程](#4-dag-图构建流程)
- [5. PlanService](#5-planservice)
- [6. 执行推进（事件驱动）](#6-执行推进事件驱动)
- [7. 完整执行流程示例](#7-完整执行流程示例)

---

## 1. DSL 语法规范

### 操作符

| 操作符 | 含义 | 示例 |
|--------|------|------|
| `->` | 顺序执行 | `A -> B -> C` |
| `&&` | 并行 AND（全部成功才继续） | `(A && B) -> C` |
| `\|\|` | 并行 OR（任一成功即继续） | `(A \|\| B) -> C` |
| `? :` | 条件分支（前驱成功走 `?`，失败走 `:` ） | `A ? B : C` |

### 语法示例

```
# 简单串行
A -> B -> C

# 并行采集 → 清洗 → 并行转换 → 聚合
(order_sync && user_sync && product_sync)
  -> data_cleaning
  -> (order_transform && user_transform)
  -> data_aggregate

# 条件分支
quality_check ? generate_report : alert_notification
```

### ANTLR4 语法文件

位于 `internal/dsl/ast/` 下：

- `TaskOrchestrationLexer.g4` — 词法规则
- `TaskOrchestrationParser.g4` — 语法规则

---

## 2. ANTLR4 解析器

**包路径**：`internal/dsl/parser/`

### 解析流程

```
DSL 文本 → Lexer (词法分析) → Token Stream → Parser (语法分析) → AST → Visitor → PlanNode DAG
```

### NewAstPlan

```go
func NewAstPlan(query string) (*AstPlan, error)
```

1. `NewTaskOrchestrationLexer` → 词法分析
2. `NewTaskOrchestrationParser` → 语法分析
3. `AstPlan.Build()` → Visitor 遍历 AST 构建 DAG 图

### AstPlan 结构

```go
type AstPlan struct {
    pro   parser.IProgramContext       // ANTLR4 语法树根节点
    end   PlanNode                     // DAG 终止节点
    tasks *syncx.Map[string, PlanNode] // 任务名 → PlanNode 映射
    root  []PlanNode                   // DAG 入口节点
}
```

### TaskPlan 接口

```go
type TaskPlan interface {
    AdjoiningNode(name string) (PlanNode, bool)  // 按名查邻接节点
    RootNode() []PlanNode                         // 返回根节点
}
```

---

## 3. Node 类型系统

**包路径**：`internal/dsl/parser/type.go` + `node.go`

### Node 接口

```go
type Node interface {
    NextNodes(execution Execution) []string  // 根据执行结果计算后继
    Type() NodeType                          // 节点类型
    ChildNodes() []string                    // 所有子节点名称
}
```

### 节点类型

| NodeType | 实现 | NextNodes 逻辑 |
|----------|------|----------------|
| `single` | SimpleNode | 前驱成功 → 返回自身名称 |
| `and` | AndNode | 前驱成功 → 返回所有子节点 |
| `or` | OrNode | 前驱成功 → 返回所有子节点 |
| `condition` | ConditionNode | 前驱成功 → successTask；前驱失败 → failureTask |
| `end` | EndNode | 返回空（DAG 终止） |
| `loop` | (预留) | 未实现 |

### PlanNode 结构

```go
type PlanNode struct {
    Pre  Node  // 前驱节点
    Next Node  // 后继节点
    Node       // 嵌入：自身节点
}
```

---

## 4. DAG 图构建流程

```
ExecExpr 文本
    │
    ▼
ANTLR4 Parser → AST
    │
    ▼
TaskOrchestrationVisitor 遍历 AST
    │
    ▼
生成 PlanNode 链（前驱-自身-后继）
    │
    ▼
mergePlan → 合并为全局 DAG
    │
    ▼
AstPlan { tasks: Map[name→PlanNode], root: []PlanNode, end: PlanNode }
```

---

## 5. PlanService

**包路径**：`internal/service/task/plan.go`

### 接口

```go
type PlanService interface {
    GetPlan(ctx context.Context, planID int64) (domain.Plan, error)
    CreateTask(ctx context.Context, planID int64, task domain.Task) error
}
```

### GetPlan 流程

```
1. 并发获取:
   - Plan 基本信息（Task 表）
   - Plan 下所有子任务（Task 表 WHERE plan_id=?）
   - Plan 执行记录（TaskExecution 表）
   - 各子任务执行记录（TaskExecution 表 WHERE plan_exec_id=?）

2. NewAstPlan(execExpr) → 构建 AST

3. 组装 PlanTask 列表:
   - 关联 AST 邻接信息（AstPlanNode）
   - 关联执行状态（TaskExecution）
   - 设置 Pre/Next 关系
   - 找出根节点（PreTask 为空的节点）

4. 返回完整 Plan 领域模型
```

---

## 6. 执行推进（事件驱动）

### 事件流

```
TaskExecution 到达终态(SUCCESS/FAILED)
    │
    ▼
CompleteProducer.Produce(Event) → Kafka
    │
    ▼
CompleteConsumer 消费:
    ├── Event.Type == "normal" && PlanID > 0
    │       → PlanTaskRunner.NextStep(ctx, task)
    │
    ├── Event.Type == "plan"
    │       → 更新 Plan 执行记录为终态 + 释放 Plan 抢占锁
    │
    └── Event.Type == "normal" && PlanID == 0
            → 无额外处理（独立任务）
```

### Event 结构

```go
type Event struct {
    PlanID         int64                      // 所属 Plan ID（0=独立任务）
    ExecID         int64                      // 执行记录 ID
    TaskID         int64                      // 任务 ID
    Version        int64                      // 版本号
    ScheduleNodeID string                     // 调度节点 ID
    Type           domain.TaskType            // normal / plan
    ExecStatus     domain.TaskExecutionStatus // 终态状态
    Name           string                     // 任务名称
}
```

---

## 7. 完整执行流程示例

以 `(A && B) -> C ? D : E` 为例：

```
1. Plan 被调度 → PlanTaskRunner.Run:
   - 抢占 Plan 任务
   - 创建 Plan 执行记录
   - 解析 DSL → A 和 B 是根节点
   - go run(A), go run(B)

2. A 执行完成(SUCCESS) → Kafka Event:
   - Consumer → NextStep(A):
     - A 的后继是 C（AND 节点）
     - CheckPre(C): A 成功但 B 未完成 → 不启动 C

3. B 执行完成(SUCCESS) → Kafka Event:
   - Consumer → NextStep(B):
     - B 的后继是 C（AND 节点）
     - CheckPre(C): A 成功且 B 成功 → 启动 C
     - go run(C)

4. C 执行完成(SUCCESS) → Kafka Event:
   - Consumer → NextStep(C):
     - C 的后继是条件节点 {? D : E}
     - C 成功 → NextNodes 返回 D
     - go run(D)

5. D 执行完成 → 无后继 → 发送 Plan 完成事件
   - Consumer → 更新 Plan 执行记录为 SUCCESS + 释放抢占锁
```
