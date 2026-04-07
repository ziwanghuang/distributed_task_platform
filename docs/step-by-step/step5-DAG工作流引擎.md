# Step 5：DAG 工作流引擎

> **目标**：在 Step 4 的补偿机制之上，构建**自定义 DSL → ANTLR4 解析 → DAG 图构建 → 事件驱动执行推进**的完整工作流引擎，支持顺序、并行（AND/OR）、条件分支、汇合等编排模式。
>
> **完成后你能看到**：启动 MySQL + etcd + Kafka → 启动调度器 → 启动执行器 → 插入 DAG 工作流任务（DSL 表达式 `TaskA->TaskB->(TaskC&&TaskD);TaskC->(TaskE&&TaskF)->TaskEnd;TaskD->TaskE->TaskEnd;`）→ 调度器抢占 Plan 任务 → PlanTaskRunner 解析 DSL 构建 DAG → 并行启动根节点 TaskA → 每个子任务完成后 Kafka 事件驱动 NextStep → 依赖检查通过后启动后继节点 → 所有路径汇聚到 TaskEnd → Plan 整体标记 SUCCESS。

---

## 1. 架构总览

Step 5 在 Step 4 之上引入了一个完整的 **DAG 工作流层**。核心思路：用一套自定义 DSL 声明多任务编排关系，ANTLR4 解析 DSL 生成 AST，Visitor 遍历 AST 构建 DAG 图（`PlanNode` 邻接结构），`PlanTaskRunner` 启动根节点，后续节点的推进完全由 **Kafka 完成事件** 驱动（`CompleteConsumer` → `NextStep`），而非轮询。

```
┌────────────────────────────────────────────────────────────────────────────┐
│                           Scheduler 进程                                   │
│                                                                            │
│  ┌──────────────────────────────────────────────────────────────────────┐  │
│  │                     Runner Dispatcher                                │  │
│  │                                                                      │  │
│  │   task.Type == "Plan"          task.Type == "Normal"                 │  │
│  │         │                              │                             │  │
│  │         ▼                              ▼                             │  │
│  │  ┌──────────────┐            ┌────────────────────┐                 │  │
│  │  │PlanTaskRunner│            │ NormalTaskRunner    │                 │  │
│  │  │              │            │ (Run/Retry/         │                 │  │
│  │  │ 1. 创建 Plan │            │  Reschedule)        │                 │  │
│  │  │    执行记录   │            └────────────────────┘                 │  │
│  │  │ 2. 解析 DSL  │                     ▲                             │  │
│  │  │    构建 DAG   │                     │ 内嵌复用                     │  │
│  │  │ 3. 启动根节点 ├─────────────────────┘                             │  │
│  │  └──────┬───────┘                                                   │  │
│  │         │ go p.run(rootTask)                                        │  │
│  └─────────┼────────────────────────────────────────────────────────┘  │
│            │                                                            │
│            │ NormalTaskRunner.Run → gRPC Execute                        │
│            ▼                                                            │
│  ┌──────────────┐    gRPC Execute    ┌──────────────┐                  │
│  │   Invoker    │ ──────────────────►│   Executor   │                  │
│  │              │◄────── Report ─────│   Node       │                  │
│  └──────┬───────┘                    └──────────────┘                  │
│         │                                                               │
│         ▼ Kafka produce                                                 │
│  ┌──────────────────────────────────────────────────────────────────┐  │
│  │                      Kafka: complete_topic                       │  │
│  └──────────────────────────────────┬───────────────────────────────┘  │
│                                     │ consume                          │
│                                     ▼                                  │
│  ┌──────────────────────────────────────────────────────────────────┐  │
│  │                    CompleteConsumer.handle()                      │  │
│  │                                                                  │  │
│  │  event.Type == Normal && PlanID > 0                              │  │
│  │    → handlePlanTask() → PlanTaskRunner.NextStep()                │  │
│  │      → 获取 DAG → 找后继节点 → CheckPre → go p.run(nextTask)     │  │
│  │                                                                  │  │
│  │  event.Type == Plan                                              │  │
│  │    → handlePlan() → 更新 Plan 执行状态 + 释放锁 + 更新 NextTime  │  │
│  └──────────────────────────────────────────────────────────────────┘  │
│                                                                        │
│  ┌──────────────────────────────────────────────────────────────────┐  │
│  │                         PlanService                              │  │
│  │                                                                  │  │
│  │  GetPlan(ctx, planID, planExecID):                               │  │
│  │    1. errgroup 并发查询：Plan任务信息 / 子任务列表 /              │  │
│  │       Plan执行记录 / 子任务执行记录                               │  │
│  │    2. NewAstPlan(execExpr) → ANTLR4 解析 → DAG 图                │  │
│  │    3. 组装 PlanTask 列表，设置 Pre/Next 关系                     │  │
│  │    4. 识别根节点（无前置依赖的节点）                              │  │
│  └──────────────────────────────────────────────────────────────────┘  │
│                                                                        │
│  ┌──────────────────────────────────────────────────────────────────┐  │
│  │                         DSL 解析层                                │  │
│  │                                                                  │  │
│  │  DSL 文本                                                        │  │
│  │    → ANTLR4 Lexer (TaskOrchestrationLexer.g4)                    │  │
│  │    → ANTLR4 Parser (TaskOrchestrationParser.g4)                  │  │
│  │    → AST                                                         │  │
│  │    → TaskOrchestrationVisitor.Visit()                            │  │
│  │    → PlanNode DAG (邻接结构: Pre ↔ Next, 节点类型路由)           │  │
│  └──────────────────────────────────────────────────────────────────┘  │
└────────────────────────────────────────────────────────────────────────┘
```

### Step 4 vs Step 5 对比

| 维度 | Step 4 | Step 5 |
|------|--------|--------|
| **任务类型** | 仅 Normal 任务 | Normal + **Plan（DAG 工作流）** |
| **任务编排** | 无，每个任务独立调度 | DSL 声明式编排（顺序/并行/条件/汇合） |
| **DAG 解析** | 无 | ANTLR4 Lexer + Parser → Visitor → PlanNode 图 |
| **执行推进** | 单次 Run 完成 | 事件驱动 NextStep，Kafka 完成事件触发后继节点 |
| **Runner 体系** | NormalTaskRunner 单一 Runner | Dispatcher → PlanTaskRunner / NormalTaskRunner 双分支 |
| **依赖检查** | 无 | `CheckPre()`：AND 节点全部成功 / OR 节点任一成功 |
| **条件分支** | 无 | `ConditionNode`：成功走 successTask，失败走 failTask |
| **Plan 生命周期** | — | 创建 Plan 执行记录 → 根节点启动 → 事件驱动推进 → 叶子节点完成 → Plan 终态 |
| **核心新增文件** | 4 个补偿器 | DSL 语法文件 + 解析器 + 6 种节点类型 + PlanTaskRunner + PlanService + CompleteConsumer |

### 新增 / 修改文件清单

| 文件 | 说明 |
|------|------|
| `internal/dsl/ast/TaskOrchestrationLexer.g4` | 词法规则：TASK_NAME、ARROW、AND、OR、QUESTION、COLON、花括号 |
| `internal/dsl/ast/TaskOrchestrationParser.g4` | 语法规则：program、expression、sequence、or、and、conditional、joinGroup |
| `internal/dsl/ast/parser/*.go` | ANTLR4 自动生成代码（Lexer/Parser/Visitor/Listener） |
| `internal/dsl/parser/type.go` | Node 接口、Execution 结构、TaskPlan 接口、NodeType 枚举 |
| `internal/dsl/parser/node.go` | 6 种节点实现：Simple、And、Or、Condition、End、Loop |
| `internal/dsl/parser/plan.go` | PlanNode 邻接结构、AstPlan 入口（NewAstPlan → Build） |
| `internal/dsl/parser/visitor.go` | TaskOrchestrationVisitor：AST → DAG 转换核心 |
| `internal/dsl/parser/compare.go` | TreeMap 排序辅助 |
| `internal/service/runner/plan_task_runner.go` | PlanTaskRunner：Run + NextStep + run 重试循环 |
| `internal/service/runner/dispather.go` | Dispatcher：按 task.Type 路由到对应 Runner（新增） |
| `internal/service/task/plan.go` | PlanService：并发查询 + DSL 解析 + DAG 组装 |
| `internal/domain/plan.go` | Plan 领域模型：RootTask / GetTask |
| `internal/domain/plantask.go` | PlanTask 领域模型：SetPre / SetNext / CheckPre / NextStep |
| `internal/event/complete/consumer.go` | CompleteConsumer：事件路由 + handlePlanTask + handlePlan（修改） |
| `internal/domain/task.go` | Task 增加 PlanID / PlanExecID / Type / ExecExpr 字段（修改） |
| `internal/repository/dao/task.go` | FindByPlanID 查询（修改） |
| `internal/repository/dao/task_execution.go` | FindExecutionByPlanID / FindExecutionByTaskIDAndPlanExecID（修改） |
| `ioc/runner.go` | InitRunner：Normal → Plan → Dispatcher 装配链（修改） |

---

## 2. 设计决策分析

### 2.1 DSL 解析：ANTLR4 vs 手写递归下降

| 维度 | ANTLR4 | 手写递归下降 |
|------|--------|-------------|
| **开发效率** | 写 .g4 文件即可，自动生成 Lexer/Parser/Visitor | 需要手工实现 tokenizer + parser，代码量大 |
| **可维护性** | 语法规则集中在 .g4 文件中，修改后 regenerate | 语法散落在代码各处，难以一目了然 |
| **错误提示** | ANTLR4 内置丰富的语法错误报告 | 需要自己实现错误定位和提示 |
| **性能** | 有一定解析开销（生成通用解析器） | 针对性优化，极致性能 |
| **依赖** | 引入 antlr4-go 运行时依赖 | 无外部依赖 |
| **学习曲线** | 需要了解 ANTLR4 语法和 Visitor 模式 | 只需熟悉 Go 语言 |

**本项目选择 ANTLR4 的理由**：

1. **DSL 语法复杂度中等偏上** — 包含顺序（→）、并行（&&/||）、条件（? :）、汇合（{}）四种运算符，还要处理优先级和多表达式（分号分隔），手写容易出错
2. **面试展示价值** — "ANTLR4 + Visitor 模式构建 DSL 引擎" 比 "手写 split + switch" 有说服力得多
3. **扩展性** — 未来如果加 Loop / Timeout / 子 Plan 等语法，只需改 .g4 文件 + 加新 Visitor 方法

**权衡**：ANTLR4 引入了 `antlr4-go` 依赖，生成的代码量较大（6 个自动生成文件）。但 DSL 解析发生在 Plan 启动时（非热路径），性能不是瓶颈。

### 2.2 DAG 推进方式：事件驱动 vs 轮询

| 维度 | 事件驱动（Kafka 完成事件） | 轮询（周期性扫描 DB） |
|------|--------------------------|---------------------|
| **实时性** | 毫秒级，任务完成立即触发后继 | 取决于轮询间隔（秒级） |
| **资源消耗** | 按需触发，无空转 | 持续扫描 DB，即使没有需要推进的任务 |
| **实现复杂度** | 需要 MQ + Consumer，事件路由逻辑 | 简单的 for + sleep + query |
| **可靠性** | Kafka 保证 at-least-once，需要幂等处理 | DB 扫描天然幂等 |
| **扩展性** | 消费者可水平扩展 | 扫描并发受 DB 连接池限制 |

**本项目选择事件驱动的理由**：

1. **Step 3 已有 Kafka 基础设施** — CompleteConsumer 已经存在，只需要扩展路由逻辑，增量改动极小
2. **DAG 推进对实时性要求高** — 一个 Plan 可能有 7+ 个节点，如果每个节点等 3 秒轮询间隔，整个 Plan 执行就被人为拉长 20+ 秒
3. **自然的职责分离** — 子任务执行完产生事件 → Consumer 负责路由 → PlanTaskRunner.NextStep 负责推进，每层职责清晰

### 2.3 PlanTaskRunner：内嵌 NormalTaskRunner vs 独立实现

| 维度 | 内嵌复用 | 独立实现 |
|------|---------|---------|
| **代码复用** | Plan 子任务的 gRPC 调用、重试、重调度直接复用 NormalTaskRunner | 需要 copy 一份逻辑或提取公共函数 |
| **耦合度** | PlanTaskRunner 依赖 NormalTaskRunner 的内部行为 | 完全解耦 |
| **灵活性** | Plan 子任务和普通任务的执行路径完全一致 | 可以为 Plan 子任务定制执行策略 |

**本项目选择内嵌复用**：`PlanTaskRunner` 内嵌 `NormalTaskRunner`，Plan 的子任务本质上就是一个个 Normal 任务，它们的抢占→创建执行记录→gRPC 调用→状态上报→重试/重调度全部走 NormalTaskRunner 的标准路径。PlanTaskRunner 只负责 **Plan 级别的编排逻辑**（创建 Plan 执行记录、解析 DAG、启动根节点、NextStep 推进）。

### 2.4 节点类型系统：接口多态 vs if-else

| 维度 | 接口多态（Node 接口） | if-else 硬编码 |
|------|---------------------|---------------|
| **扩展性** | 新增节点类型只需实现 Node 接口 | 每次新增类型要改所有 switch 分支 |
| **可测试性** | 每种节点独立测试 | 逻辑耦合在一起 |
| **代码清晰度** | 行为封装在节点内部 | 行为散落在外部函数中 |

**本项目选择接口多态**：6 种节点类型（Simple、And、Or、Condition、End、Loop），每种节点的 `NextNodes()` 逻辑完全不同——AND 要求所有子节点完成、OR 只要一个完成、Condition 根据执行结果选择分支。用接口封装后，Visitor 只需要 `planNode.Next.NextNodes(execution)` 一行调用，无需关心具体节点类型。

---

## 3. 实现详解

### 3.1 DSL 语法设计

#### 3.1.1 词法规则（Lexer）

```antlr
// internal/dsl/ast/TaskOrchestrationLexer.g4
lexer grammar TaskOrchestrationLexer;

// 任务名：字母数字下划线组成
TASK_NAME : [a-zA-Z_] [a-zA-Z0-9_]* ;

// 运算符
ARROW     : '->' ;      // 顺序连接
AND       : '&&' ;      // 并行（全部成功）
OR        : '||' ;      // 并行（任一成功）
QUESTION  : '?' ;       // 条件判断
COLON     : ':' ;       // 条件分支
SEMI      : ';' ;       // 表达式分隔符
LPAREN    : '(' ;
RPAREN    : ')' ;
LBRACE    : '{' ;       // 汇合组开始
RBRACE    : '}' ;       // 汇合组结束

WS        : [ \t\r\n]+ -> skip ;
```

**设计要点**：

- `TASK_NAME` 是最基础的 token，代表一个子任务的名字
- `SEMI`（分号）允许在一个 DSL 中声明多条表达式链，它们会被 `mergePlan()` 合并成一个全局 DAG
- `{}`（花括号）用于 **汇合组（JoinGroup）**，表示花括号内的多个任务必须全部完成后才能触发后继

#### 3.1.2 语法规则（Parser）

```antlr
// internal/dsl/ast/TaskOrchestrationParser.g4
parser grammar TaskOrchestrationParser;
options { tokenVocab=TaskOrchestrationLexer; }

program
    : expression (SEMI expression)* EOF
    ;

expression
    : sequenceExpression
    ;

sequenceExpression
    : orExpression (ARROW orExpression)*
    ;

orExpression
    : andExpression (OR andExpression)*
    ;

andExpression
    : conditionalExpression (AND conditionalExpression)*
    ;

conditionalExpression
    : basicExpression (QUESTION basicExpression COLON basicExpression)?
    ;

basicExpression
    : TASK_NAME
    | LPAREN expression RPAREN
    | joinGroup
    ;

joinGroup
    : LBRACE TASK_NAME (AND TASK_NAME)* RBRACE
    ;
```

**运算符优先级**（从低到高）：

| 优先级 | 运算符 | 含义 |
|--------|--------|------|
| 最低 | `->` | 顺序连接 |
| | `\|\|` | OR 并行 |
| | `&&` | AND 并行 |
| 最高 | `? :` | 条件分支 |

**DSL 示例**：

```
# 简单顺序
TaskA -> TaskB -> TaskC

# AND 并行：A 和 B 都完成后才执行 C
TaskA && TaskB -> TaskC

# OR 并行：A 或 B 任一完成即执行 C
TaskA || TaskB -> TaskC

# 条件分支：A 完成后，如果 A 成功走 B，失败走 C
TaskA -> TaskA ? TaskB : TaskC

# 复杂组合（实际集成测试用例）
TaskA->TaskB->(TaskC&&TaskD);TaskC->(TaskE&&TaskF)->TaskEnd;TaskD->TaskE->TaskEnd;
```

#### 3.1.3 多表达式合并机制

DSL 用分号分隔多条表达式，每条表达式描述图的一部分。`program` 规则匹配 `expression (SEMI expression)* EOF`，Visitor 的 `VisitProgram` 会依次访问每条表达式，得到多个局部 DAG，然后调用 `mergePlan()` 将它们合并为一个全局 DAG。

**合并规则**：如果同一个任务名在多条表达式中出现，它们引用同一个 `PlanNode`（通过 `tasks` map 保证唯一性），前驱/后继关系取并集。

### 3.2 ANTLR4 Visitor → DAG 构建

#### 3.2.1 核心数据结构

```go
// internal/dsl/parser/plan.go

// PlanNode —— DAG 的邻接结构
type PlanNode struct {
    Pre  Node   // 前驱节点（实现 Node 接口）
    Next Node   // 后继节点（实现 Node 接口）
    Node Node   // 自身节点
}

// AstPlan —— TaskPlan 接口的实现
type AstPlan struct {
    tasks *syncx.Map[string, PlanNode]  // 任务名 → PlanNode
    root  []PlanNode                     // 根节点列表（无前驱）
    end   PlanNode                       // 终止节点
}
```

**`PlanNode` 的三个 Node 字段解读**：

- `Pre`：我的前驱是谁？用于 `CheckPre()` —— AND 节点要求所有前驱成功，OR 节点只要任一前驱成功
- `Next`：我的后继是谁？用于 `NextStep()` —— 拿到后继节点列表，检查依赖后启动
- `Node`：我自己是什么类型？`SimpleNode`（单任务）、`EndNode`（终止节点）等

#### 3.2.2 Node 类型系统

```go
// internal/dsl/parser/type.go

type Node interface {
    NextNodes(execution Execution) []string  // 根据执行结果返回下一步任务名列表
    Type() NodeType                          // 节点类型
    ChildNodes() []string                    // 子节点名列表
}

type Execution struct {
    TaskName string
    Status   TaskExecutionStatus  // Success / Failed / Running
}
```

6 种节点实现：

| 节点类型 | `NextNodes()` 行为 | 使用场景 |
|---------|-------------------|---------|
| `SimpleNode` | 返回自身任务名 `[taskName]` | 单个任务节点 |
| `AndNode` | 返回所有子节点名 `[child1, child2, ...]` | `A && B` — 全部子节点必须完成 |
| `OrNode` | 返回所有子节点名 `[child1, child2, ...]` | `A \|\| B` — 任一子节点完成即可 |
| `ConditionNode` | 根据 `execution.Status` 返回 `[successTask]` 或 `[failTask]` | `A ? B : C` — 条件分支 |
| `EndNode` | 返回空 `[]` | DAG 终止节点，无后继 |
| `LoopNode` | 保留节点，未实现 | 预留循环语义 |

**关键设计**：`ConditionNode.NextNodes()` 接收 `Execution` 参数，这是条件分支能工作的核心——它需要知道前驱任务的执行结果，才能决定走哪个分支：

```go
// internal/dsl/parser/node.go
func (c *ConditionNode) NextNodes(execution Execution) []string {
    if execution.Status == TaskExecutionStatusSuccess {
        return []string{c.successTask}
    }
    return []string{c.failTask}
}
```

#### 3.2.3 Visitor 遍历逻辑

`TaskOrchestrationVisitor` 实现了 ANTLR4 生成的 Visitor 接口，核心方法：

**`VisitProgram`** — 入口：遍历所有 expression，收集多个局部 DAG，调用 `mergePlan()` 合并

```go
func (v *TaskOrchestrationVisitor) VisitProgram(ctx *ast.ProgramContext) interface{} {
    plans := make([]TaskPlan, 0, len(ctx.AllExpression()))
    for _, expr := range ctx.AllExpression() {
        plan := v.Visit(expr.(*ast.ExpressionContext))
        plans = append(plans, plan.(TaskPlan))
    }
    return v.mergePlan(plans)
}
```

**`VisitSequenceExpression`** — 处理 `A -> B -> C`：左侧的出口连接右侧的入口

```go
func (v *TaskOrchestrationVisitor) VisitSequenceExpression(ctx *ast.SequenceExpressionContext) interface{} {
    ors := ctx.AllOrExpression()
    if len(ors) == 1 {
        return v.Visit(ors[0])
    }
    // 依次连接：前一个的出口 → 后一个的入口
    left := v.Visit(ors[0]).(TaskPlan)
    for i := 1; i < len(ors); i++ {
        right := v.Visit(ors[i]).(TaskPlan)
        // left.end.Next = right.root 的 Pre
        // right.root.Pre = left.end 的 Next
        left = v.connect(left, right)
    }
    return left
}
```

**`VisitAndExpression`** — 处理 `A && B`：创建 `AndNode`，子节点为 A 和 B

**`VisitOrExpression`** — 处理 `A || B`：创建 `OrNode`，子节点为 A 和 B

**`VisitConditionalExpression`** — 处理 `A ? B : C`：创建 `ConditionNode`，successTask=B, failTask=C

**`VisitJoinGroup`** — 处理 `{A && B}`：花括号内的任务作为一个汇合组，全部完成后触发后继

**`mergePlan()`** — 核心合并逻辑：

```go
func (v *TaskOrchestrationVisitor) mergePlan(plans []TaskPlan) TaskPlan {
    // 1. 合并所有 plan 的 tasks map（同名任务共享同一个 PlanNode）
    // 2. 合并 Pre/Next 关系
    // 3. 遍历所有 PlanNode，找出没有 Pre 的作为 root
    // 4. 找出没有 Next 的作为 end
    // 5. 如果有多个 end，创建一个 EndNode 作为全局终止节点
}
```

#### 3.2.4 DSL → DAG 转换示例

以集成测试的 DSL 为例：

```
TaskA->TaskB->(TaskC&&TaskD);TaskC->(TaskE&&TaskF)->TaskEnd;TaskD->TaskE->TaskEnd;
```

解析后的 DAG 结构（`→` 表示顺序依赖）：

```
TaskA → TaskB → [TaskC (AND) TaskD]
                     │            │
                     ▼            ▼
              [TaskE (AND) TaskF] TaskE
                     │            │
                     ▼            ▼
                  TaskEnd      TaskEnd
```

等价的 PlanNode 连接关系：

| 任务 | Pre（前驱） | Next（后继） |
|------|-----------|------------|
| TaskA | 无（root） | SimpleNode("TaskB") |
| TaskB | SimpleNode("TaskA") | AndNode(["TaskC", "TaskD"]) |
| TaskC | AndNode(...) | AndNode(["TaskE", "TaskF"]) |
| TaskD | AndNode(...) | SimpleNode("TaskE") |
| TaskE | AndNode(...) / SimpleNode("TaskD") | SimpleNode("TaskEnd") |
| TaskF | AndNode(...) | SimpleNode("TaskEnd") |
| TaskEnd | SimpleNode("TaskE") / SimpleNode("TaskF") | 无（end） |

### 3.3 PlanService：并发查询 + DAG 组装

```go
// internal/service/task/plan.go

func (p *PlanService) GetPlan(ctx context.Context, planID, planExecID int64) (domain.Plan, error) {
    var (
        plan       domain.Plan
        tasks      []domain.Task
        planExec   domain.TaskExecution
        taskExecs  map[int64]domain.TaskExecution
    )

    // 1. errgroup 并发查询 4 个数据源
    eg, egCtx := errgroup.WithContext(ctx)

    eg.Go(func() error {
        // 查询 Plan 任务自身信息（task 表, type=Plan）
        t, err := p.taskSvc.FindByID(egCtx, planID)
        plan = domain.Plan{ID: t.ID, Name: t.Name, ExecExpr: t.ExecExpr, ...}
        return err
    })
    eg.Go(func() error {
        // 查询 Plan 下的所有子任务（task 表, plan_id=planID）
        tasks, err = p.repo.FindByPlanID(egCtx, planID)
        return err
    })
    eg.Go(func() error {
        // 查询 Plan 执行记录（task_execution 表）
        planExec, err = p.execSvc.FindByID(egCtx, planExecID)
        return err
    })
    eg.Go(func() error {
        // 查询所有子任务的执行记录（按 plan_exec_id）
        taskExecs, err = p.execRepo.FindExecutionsByPlanExecID(egCtx, planExecID)
        return err
    })

    if err := eg.Wait(); err != nil {
        return domain.Plan{}, err
    }

    // 2. 解析 DSL → 构建 DAG
    astPlan := parser.NewAstPlan(plan.ExecExpr)

    // 3. 组装 PlanTask 列表
    steps := make([]*domain.PlanTask, 0, len(tasks))
    for _, t := range tasks {
        pt := &domain.PlanTask{
            Task:        t,
            AstPlanNode: astPlan.GetTask(t.Name),
        }
        if exec, ok := taskExecs[t.ID]; ok {
            pt.Execution = &exec
        }
        steps = append(steps, pt)
    }

    // 4. 设置 Pre/Next 关系 + 识别根节点
    for _, pt := range steps {
        pt.SetPre(steps)
        pt.SetNext(steps, plan.Execution)
    }

    plan.Steps = steps
    plan.Root = plan.RootTask()
    return plan, nil
}
```

**errgroup 并发查询设计**：4 个查询互不依赖，并行执行后合并结果。这是一个经典的 Go 并发模式——`errgroup` 提供 `ctx` 取消传播和 error 收集，任一查询失败则提前返回。

### 3.4 PlanTask 领域模型

```go
// internal/domain/plantask.go

type PlanTask struct {
    PreTask     []*PlanTask          // 前驱任务列表
    NextTask    []*PlanTask          // 后继任务列表
    AstPlanNode parser.PlanNode      // 对应的 DAG 节点
    Task                             // 内嵌 Task（继承所有字段）
    Execution   *TaskExecution       // 当前执行记录（可能为 nil）
}
```

**`SetPre()`** — 根据 AST 的 Pre 节点，从 steps 列表中找到对应的 PlanTask，建立 `PreTask` 关系：

```go
func (p *PlanTask) SetPre(steps []*PlanTask) {
    if parser.NodeIsNil(p.AstPlanNode.Pre) {
        return
    }
    for _, child := range p.AstPlanNode.Pre.ChildNodes() {
        for _, step := range steps {
            if step.Name == child {
                p.PreTask = append(p.PreTask, step)
            }
        }
    }
}
```

**`SetNext()`** — 通过 `AstPlanNode.Next.NextNodes(execution)` 获取后继任务名，再从 steps 中找到对应 PlanTask。**这里的关键是 `NextNodes()` 接收 `Execution` 参数**，使得 `ConditionNode` 能根据前驱任务的执行状态选择分支：

```go
func (p *PlanTask) SetNext(steps []*PlanTask, execution *TaskExecution) {
    if parser.NodeIsNil(p.AstPlanNode.Next) {
        return
    }
    // 构造 Execution 参数（当前任务的执行状态）
    exec := p.newAstExecution()
    nextNames := p.AstPlanNode.Next.NextNodes(exec)
    for _, name := range nextNames {
        for _, step := range steps {
            if step.Name == name {
                p.NextTask = append(p.NextTask, step)
            }
        }
    }
}
```

**`CheckPre()`** — DAG 推进的核心检查：后继节点是否可以启动？

```go
func (p *PlanTask) CheckPre() bool {
    if len(p.PreTask) == 0 {
        return true  // 根节点，无前驱
    }
    switch p.AstPlanNode.Pre.Type() {
    case parser.AndNodeType:
        // AND：所有前驱都必须成功
        for _, pre := range p.PreTask {
            if pre.Execution == nil || pre.Execution.Status != ExecStatusSuccess {
                return false
            }
        }
        return true
    case parser.OrNodeType:
        // OR：任一前驱成功即可
        for _, pre := range p.PreTask {
            if pre.Execution != nil && pre.Execution.Status == ExecStatusSuccess {
                return true
            }
        }
        return false
    default:
        // SimpleNode 等：前驱只有一个，检查是否成功
        for _, pre := range p.PreTask {
            if pre.Execution == nil || pre.Execution.Status != ExecStatusSuccess {
                return false
            }
        }
        return true
    }
}
```

### 3.5 PlanTaskRunner：Plan 级别编排

#### 3.5.1 Run() — Plan 启动

```go
// internal/service/runner/plan_task_runner.go

func (p *PlanTaskRunner) Run(ctx context.Context, t domain.Task) error {
    // 1. 抢占 Plan 任务
    task, err := p.taskSvc.Acquire(ctx, t)
    if err != nil {
        return err
    }

    // 2. 创建 Plan 级别的执行记录（状态 RUNNING）
    exec, err := p.execSvc.CreateExecution(ctx, task, domain.ExecStatusRunning)
    if err != nil {
        return err
    }

    // 3. 获取完整 Plan（解析 DSL → 构建 DAG → 组装 PlanTask）
    plan, err := p.planSvc.GetPlan(ctx, task.ID, exec.ID)
    if err != nil {
        return err
    }

    // 4. 并行启动所有根节点
    for _, rootTask := range plan.Root {
        rt := rootTask
        go p.run(ctx, rt)
    }
    return nil
}
```

#### 3.5.2 NextStep() — 事件驱动的 DAG 推进

```go
func (p *PlanTaskRunner) NextStep(ctx context.Context, event event.Event) error {
    // 1. 获取 DAG
    plan, err := p.planSvc.GetPlan(ctx, event.PlanID, event.PlanExecID)
    if err != nil {
        return err
    }

    // 2. 找到当前完成的任务
    currentTask := plan.GetTask(event.Name)
    if currentTask == nil {
        return fmt.Errorf("task %s not found in plan", event.Name)
    }

    // 3. 获取后继节点
    nextTasks := currentTask.NextStep()
    if len(nextTasks) == 0 {
        // 没有后继 → 这是叶子节点 → 产生 Plan 完成事件
        return p.producer.Produce(ctx, event.Event{
            PlanID:    event.PlanID,
            Type:      domain.PlanTaskType,
            ExecStatus: domain.ExecStatusSuccess,
            ...
        })
    }

    // 4. 检查每个后继节点的前置依赖是否满足
    for _, nextTask := range nextTasks {
        if nextTask.CheckPre() {
            nt := nextTask
            go p.run(ctx, nt)
        }
        // 不满足的跳过，等其他前驱完成后再次触发 NextStep 时自然会通过
    }
    return nil
}
```

**NextStep 的幂等性**：Kafka at-least-once 可能导致同一事件被消费多次。但由于 `CheckPre()` 只检查前驱的执行状态，而 `p.run()` 内部会通过 CAS 抢占防止重复执行，所以重复消费是安全的。

#### 3.5.3 run() — 子任务执行重试循环

```go
func (p *PlanTaskRunner) run(ctx context.Context, task *domain.PlanTask) {
    const (
        maxRunRetries        = 10
        defaultRetrySleepTime = 500 * time.Millisecond
    )
    for i := 0; i < maxRunRetries; i++ {
        err := p.NormalTaskRunner.Run(ctx, task.Task)
        switch {
        case err == nil:
            return  // 成功，退出
        case errors.Is(err, errs.ErrTaskPreemptFailed):
            return  // 别人抢占了，正常退出
        case ctx.Err() != nil:
            return  // 上下文取消，退出
        default:
            // 其他错误（如 DB 瞬时故障），等待后重试
            time.Sleep(defaultRetrySleepTime)
        }
    }
}
```

**为什么 Plan 子任务需要额外的重试循环？** NormalTaskRunner.Run() 是"获取任务→抢占→创建执行记录→执行"的一体流程，如果 DB 连接闪断导致抢占失败，NormalTaskRunner 只会返回 error 不会重试。但对于 Plan 子任务，"能不能执行"直接影响整个 DAG 的推进，所以在外层包了一个 10 次重试。

### 3.6 Runner Dispatcher：类型路由

```go
// internal/service/runner/dispather.go

type Dispatcher struct {
    planRunner   *PlanTaskRunner
    normalRunner *NormalTaskRunner
}

func (d *Dispatcher) Run(ctx context.Context, task domain.Task) error {
    switch task.Type {
    case domain.PlanTaskType:
        return d.planRunner.Run(ctx, task)
    default:
        return d.normalRunner.Run(ctx, task)
    }
}

func (d *Dispatcher) Retry(ctx context.Context, task domain.Task) error {
    // Plan 任务不触发重试，重试总是走 NormalTaskRunner
    return d.normalRunner.Retry(ctx, task)
}

func (d *Dispatcher) Reschedule(ctx context.Context, task domain.Task) error {
    return d.normalRunner.Reschedule(ctx, task)
}
```

**设计选择**：`Retry` 和 `Reschedule` 始终委托给 `normalRunner`。原因是 Plan 任务本身不执行业务逻辑，只负责编排。如果 Plan 的某个子任务失败需要重试，那是 **子任务**（Normal 类型）的重试，而非 Plan 级别的重试。

### 3.7 事件驱动的 DAG 推进

#### 3.7.1 事件结构

```go
// internal/event/complete.go
type Event struct {
    PlanID         int64  `json:"plan_id"`
    PlanExecID     int64  `json:"plan_exec_id"`
    ExecID         int64  `json:"exec_id"`
    TaskID         int64  `json:"task_id"`
    Version        int64  `json:"version"`
    ScheduleNodeID string `json:"schedule_node_id"`
    Type           string `json:"type"`       // "Normal" / "Plan"
    ExecStatus     uint8  `json:"exec_status"`
    Name           string `json:"name"`       // 任务名
}
```

**PlanID 的双重用途**：

- `PlanID > 0 && Type == "Normal"` → 这是 Plan 的子任务完成事件 → 触发 NextStep
- `PlanID == 0 && Type == "Normal"` → 这是独立的普通任务完成事件 → 无需额外处理
- `Type == "Plan"` → 这是 Plan 整体完成事件 → 更新 Plan 终态

#### 3.7.2 CompleteConsumer 事件路由

```go
// internal/event/complete/consumer.go

func (c *CompleteConsumer) handle(ctx context.Context, event event.Event) error {
    switch {
    case event.Type == domain.NormalTaskType && event.PlanID > 0:
        // Plan 子任务完成 → 驱动 DAG 推进
        return c.handlePlanTask(ctx, event)
    case event.Type == domain.PlanTaskType:
        // Plan 整体完成 → 更新终态 + 释放锁
        return c.handlePlan(ctx, event)
    default:
        // 普通任务完成 → 无额外处理
        return nil
    }
}

func (c *CompleteConsumer) handlePlanTask(ctx context.Context, evt event.Event) error {
    // 直接调用 PlanTaskRunner.NextStep
    return c.planRunner.NextStep(ctx, evt)
}

func (c *CompleteConsumer) handlePlan(ctx context.Context, evt event.Event) error {
    // 1. 更新 Plan 执行记录状态（SUCCESS/FAILED）
    err := c.execSvc.UpdateExecStatus(ctx, evt.ExecID, evt.ExecStatus)
    if err != nil {
        return err
    }
    // 2. 更新 Plan 任务的下次调度时间
    err = c.taskSvc.UpdateNextTime(ctx, evt.TaskID)
    if err != nil {
        return err
    }
    // 3. 释放 Plan 任务的 CAS 锁（version 递增，status 回到 Waiting）
    return c.taskSvc.ReleaseLock(ctx, evt.TaskID, evt.Version)
}
```

#### 3.7.3 完整数据流时序

以 `TaskA -> TaskB -> TaskC` 为例：

```
时刻 T0: Scheduler 抢占 Plan 任务
  │
  ├─ PlanTaskRunner.Run()
  │   ├─ 创建 Plan 执行记录（RUNNING）
  │   ├─ PlanService.GetPlan()
  │   │   ├─ errgroup 并发查询 4 个数据源
  │   │   ├─ NewAstPlan("TaskA->TaskB->TaskC")
  │   │   │   ├─ ANTLR4 Lexer → tokens
  │   │   │   ├─ ANTLR4 Parser → AST
  │   │   │   └─ Visitor.Visit() → PlanNode DAG
  │   │   └─ 组装 PlanTask，识别根节点 [TaskA]
  │   │
  │   └─ go p.run(ctx, TaskA)  // 启动根节点
  │
  ▼
时刻 T1: TaskA 开始执行
  │
  ├─ NormalTaskRunner.Run(TaskA)
  │   ├─ CAS 抢占 TaskA
  │   ├─ 创建 TaskA 执行记录
  │   ├─ gRPC Execute → Executor
  │   └─ Executor 执行完毕 → gRPC Report → CompleteProducer → Kafka
  │
  ▼
时刻 T2: Kafka 消费 TaskA 完成事件
  │
  ├─ CompleteConsumer.handle()
  │   ├─ event.Type == Normal && PlanID > 0
  │   └─ handlePlanTask() → PlanTaskRunner.NextStep()
  │       ├─ PlanService.GetPlan()（重新加载 DAG + 最新执行状态）
  │       ├─ 找到 TaskA → nextTasks = [TaskB]
  │       ├─ TaskB.CheckPre() → TaskA 已成功 → true
  │       └─ go p.run(ctx, TaskB)
  │
  ▼
时刻 T3: TaskB 完成 → 同样流程
  │
  ├─ NextStep → nextTasks = [TaskC]
  ├─ TaskC.CheckPre() → TaskB 已成功 → true
  └─ go p.run(ctx, TaskC)
  │
  ▼
时刻 T4: TaskC 完成
  │
  ├─ NextStep → nextTasks = []（无后继，叶子节点）
  └─ Produce Plan 完成事件 → Kafka
  │
  ▼
时刻 T5: Kafka 消费 Plan 完成事件
  │
  ├─ CompleteConsumer.handle()
  │   ├─ event.Type == Plan
  │   └─ handlePlan()
  │       ├─ 更新 Plan 执行记录 → SUCCESS
  │       ├─ 更新 Plan NextTime
  │       └─ 释放 Plan CAS 锁
  │
  ▼
  DAG 工作流执行完毕
```

### 3.8 IOC 装配

```go
// ioc/runner.go
func InitRunner(
    planSvc *task.PlanService,
    invoker invoker.Invoker,
    execSvc *task.ExecutionService,
    taskSvc *task.TaskService,
    producer event.CompleteProducer,
) runner.Runner {
    // 1. 基础 Runner
    normalRunner := runner.NewNormalTaskRunner(invoker, execSvc, taskSvc, producer)

    // 2. Plan Runner（内嵌 NormalRunner）
    planRunner := runner.NewPlanRunner(planSvc, normalRunner, producer)

    // 3. Dispatcher（顶层入口）
    return runner.NewDispatcherRunner(planRunner, normalRunner)
}
```

装配链：`Dispatcher → PlanTaskRunner → NormalTaskRunner → Invoker → gRPC`

调度器的 `Scheduler.Schedule()` 只和 `Dispatcher` 交互，完全不感知 Plan/Normal 的区别。

### 3.9 数据库变更

#### Task 表新增字段

```sql
ALTER TABLE task ADD COLUMN type     VARCHAR(20) DEFAULT 'NORMAL';
ALTER TABLE task ADD COLUMN plan_id  BIGINT      DEFAULT 0;
ALTER TABLE task ADD COLUMN exec_expr TEXT        DEFAULT '';
```

| 字段 | 说明 |
|------|------|
| `type` | 任务类型："NORMAL"（普通任务）/ "PLAN"（DAG 工作流） |
| `plan_id` | 子任务关联的 Plan 任务 ID（0 表示非 Plan 子任务） |
| `exec_expr` | DSL 表达式，仅 Plan 类型任务有值 |

#### TaskExecution 表新增字段

```sql
ALTER TABLE task_execution ADD COLUMN plan_id      BIGINT DEFAULT 0;
ALTER TABLE task_execution ADD COLUMN plan_exec_id BIGINT DEFAULT 0;
```

| 字段 | 说明 |
|------|------|
| `plan_id` | 关联的 Plan 任务 ID |
| `plan_exec_id` | 关联的 Plan 执行记录 ID，用于关联同一次 Plan 执行中所有子任务的执行记录 |

#### 关键查询

```go
// FindByPlanID —— 查询 Plan 下的所有子任务
db.Where("plan_id = ?", planID).Find(&tasks)

// FindExecutionsByPlanExecID —— 查询一次 Plan 执行中所有子任务的执行记录
db.Where("plan_exec_id = ?", planExecID).Find(&execs)
// 结果转为 map[taskID]execution，O(1) 查找

// FindExecutionByTaskIDAndPlanExecID —— 查询特定子任务在特定 Plan 执行中的执行记录
db.Where("task_id = ? AND plan_exec_id = ?", taskID, planExecID).First(&exec)
```

---

## 4. 实现不足与优化方向

### 4.1 每次 NextStep 重新加载完整 DAG

**现状**：`PlanTaskRunner.NextStep()` 每次被调用都会执行 `PlanService.GetPlan()`，这意味着每个子任务完成都要重新：
1. 并发查询 4 个数据库表
2. ANTLR4 解析 DSL 文本
3. 构建 PlanNode DAG
4. 组装 PlanTask 列表

**问题**：对于一个 7 节点的 DAG，整个执行过程会调用 7 次 `GetPlan()`，其中 DSL 解析结果和 DAG 结构完全不变（只有执行状态在变），存在明显的重复计算。

**优化方案**：

| 方案 | 做法 | 优劣 |
|------|------|------|
| **DAG 缓存** | 以 `planID` 为 key 缓存 `AstPlan`（DAG 结构不变），NextStep 只刷新执行状态 | 简单有效，但需要处理 DSL 表达式变更时的缓存失效 |
| **Plan 内存快照** | Run() 时构建完整 Plan 对象，通过 channel 传递给后续 NextStep | 性能最优，但 NextStep 由 Kafka Consumer 在不同 goroutine 中调用，需要跨 goroutine 通信 |
| **Redis 缓存** | DAG 结构和执行状态都存 Redis，NextStep 只查 Redis | 适合多实例部署场景，但增加了 Redis 依赖 |

**推荐**：短期做 DAG 缓存（用 `sync.Map` 按 `planID` 缓存 `AstPlan`），长期考虑 Redis 方案。

### 4.2 CheckPre 的线性搜索

**现状**：`Plan.GetTask(name)` 通过线性遍历 `steps` 列表查找任务，`SetPre/SetNext` 内部也是嵌套循环匹配任务名。

**问题**：如果 DAG 有 N 个节点，每个节点的 SetPre/SetNext 是 O(N) 查找，整体 O(N²)。对于现阶段的规模（<20 节点）不是问题，但不够优雅。

**优化**：`PlanService.GetPlan()` 组装时构建 `map[string]*PlanTask` 索引，SetPre/SetNext 直接按名字 O(1) 查找。

### 4.3 Plan 执行的错误处理不完整

**现状**：
- 如果某个子任务执行失败（FAILED），当前实现不会主动标记 Plan 为失败
- Plan 的终态判定依赖"叶子节点完成时发送 Plan 完成事件"，但如果中间节点失败导致后继永远无法触发，Plan 会永远停留在 RUNNING 状态

**优化方案**：

1. **NextStep 增加失败处理**：子任务失败时，检查是否还有其他路径可以推进。如果所有路径都被阻断，直接产生 Plan FAILED 事件
2. **Plan 超时机制**：Plan 执行记录也设置 Deadline，超时后由补偿器标记为 FAILED
3. **子任务重试联动**：子任务进入 FAILED_RETRYABLE 时，暂停 DAG 推进（不触发后继），等子任务重试成功后再继续

### 4.4 ConditionNode 的局限性

**现状**：`ConditionNode` 只支持二元分支（成功/失败），无法处理更复杂的条件逻辑（如"任务 A 返回值 > 100 走分支 X，否则走分支 Y"）。

**优化方向**：扩展 `Execution` 结构，增加 `Result map[string]string` 字段，`ConditionNode` 可以基于任务返回值做条件判断，而非仅依赖成功/失败状态。

### 4.5 LoopNode 未实现

`LoopNode` 在代码中有定义但 `NextNodes()` 返回空。未来如果要支持循环语义（如"重复执行 TaskA 直到返回成功"），需要在 Node 接口中扩展状态管理（循环计数器、退出条件）。

### 4.6 并发安全

**现状**：`PlanTaskRunner.run()` 是通过 `go p.run(ctx, task)` 启动的 goroutine，多个子任务可能并发执行 `NextStep()`。

**当前的安全保证**：
- 每个子任务的 CAS 抢占保证不会重复执行
- `CheckPre()` 是只读检查，依赖 DB 中的执行状态，不存在竞态

**潜在风险**：如果两个前驱任务（如 AND 节点的 A 和 B）几乎同时完成，它们的 NextStep 可能同时检查后继节点 C 的 CheckPre，都发现条件满足，都尝试 `go p.run(ctx, C)`。但这不会造成问题——`NormalTaskRunner.Run()` 内部的 CAS 抢占会保证只有一个 goroutine 成功。另一个会收到 `ErrTaskPreemptFailed` 并退出。

---

## 5. 集成测试验证

### 5.1 测试场景

集成测试文件 `internal/test/integration/plan_test.go` 覆盖了完整的 DAG 工作流执行：

**DSL 表达式**：`TaskA->TaskB->(TaskC&&TaskD);TaskC->(TaskE&&TaskF)->TaskEnd;TaskD->TaskE->TaskEnd;`

**预期执行顺序**：`[TaskA, TaskB, TaskC, TaskD, TaskF, TaskE, TaskEnd]`

**测试流程**：

```go
func TestPlanExecution(t *testing.T) {
    // 1. 创建 Plan 任务（type=Plan, exec_expr=上述 DSL）
    // 2. 创建 7 个子任务（TaskA ~ TaskF + TaskEnd, plan_id 指向 Plan）
    // 3. 启动调度器 + 执行器（本地 Mock）
    // 4. 等待 Plan 执行完成
    // 5. 验证执行顺序：[A, B, C, D, F, E, TaskEnd]
    // 6. 验证 Plan 执行记录状态 == SUCCESS
    // 7. 验证所有子任务执行记录状态 == SUCCESS
}
```

### 5.2 部署验证

```bash
# 1. 启动全部中间件
docker compose up -d   # MySQL + etcd + Kafka

# 2. 启动调度器
make run_scheduler_only

# 3. 启动执行器
cd example/longrunning && go run main.go

# 4. 插入 DAG 工作流任务
#    Plan 任务：
#      INSERT INTO task (name, type, exec_expr, cron_expr, ...)
#      VALUES ('my_dag', 'Plan', 'TaskA->TaskB->(TaskC&&TaskD);...', '*/30 * * * * *', ...);
#    子任务：
#      INSERT INTO task (name, type, plan_id, ...)
#      VALUES ('TaskA', 'Normal', <plan_id>, ...);
#      -- 以此类推 TaskB ~ TaskEnd

# 5. 观察日志：
#    [INFO] Scheduler: acquired task my_dag (type=Plan)
#    [INFO] PlanTaskRunner: parsing DSL, building DAG
#    [INFO] PlanTaskRunner: starting root task TaskA
#    [INFO] NormalTaskRunner: executing TaskA
#    [INFO] CompleteConsumer: TaskA completed, calling NextStep
#    [INFO] PlanTaskRunner: NextStep → TaskB, CheckPre passed
#    [INFO] NormalTaskRunner: executing TaskB
#    ... (TaskC, TaskD 并行执行)
#    ... (TaskE, TaskF 依赖满足后执行)
#    [INFO] PlanTaskRunner: NextStep → TaskEnd, no next tasks
#    [INFO] PlanTaskRunner: producing Plan complete event
#    [INFO] CompleteConsumer: Plan completed, status=SUCCESS
```

### ✅ 验收标准

- [x] `TaskA -> TaskB -> TaskC` 顺序执行正确
- [x] `TaskA && TaskB -> TaskC` AND 并行，全部成功后触发 C
- [x] `TaskA || TaskB -> TaskC` OR 并行，任一成功即触发 C
- [x] `TaskA -> TaskA ? TaskB : TaskC` 条件分支正确
- [x] 多表达式（分号分隔）合并为统一 DAG
- [x] 集成测试：7 节点 DAG 执行顺序符合预期
- [x] Plan 执行完成后状态 == SUCCESS，所有子任务状态 == SUCCESS
- [x] DSL 语法错误时 ANTLR4 报告清晰的错误信息

---

## 6. 面试高频考点

### 6.1 "为什么用 ANTLR4 而不是手写解析器？"

> 我们的 DSL 包含 4 种运算符（顺序、AND、OR、条件分支）和优先级规则，手写递归下降解析器容易在优先级处理和错误报告上出问题。ANTLR4 让我把语法规则集中在两个 .g4 文件里（总共不到 100 行），自动生成 Lexer/Parser/Visitor 骨架代码，我只需要实现 Visitor 的业务逻辑（AST → DAG 转换）。而且 ANTLR4 内置的语法错误报告比手写的要完善得多。性能上不是问题，因为 DSL 解析只在 Plan 启动时发生一次，不在热路径上。

### 6.2 "DAG 推进为什么用事件驱动而不是轮询？"

> 两个原因：一是实时性，DAG 节点多的时候（7+ 个节点），如果每个节点等 3 秒轮询，整个 Plan 执行被人为拉长 20 多秒，事件驱动是毫秒级触发；二是我们在 Step 3 就已经建好了 Kafka 完成事件 + CompleteConsumer 的基础设施，DAG 推进只需要在 Consumer 里加一个路由分支——`PlanID > 0 ? handlePlanTask : skip`——增量改动非常小。本质上这是在已有的事件系统上叠加了一层 DAG 编排语义。

### 6.3 "CheckPre 的并发安全怎么保证的？"

> 分两层看。第一层：CheckPre 本身是只读操作，它查的是 DB 里的 execution status，不存在内存竞态。第二层：如果 AND 节点的两个前驱几乎同时完成，两个 NextStep 调用会同时对后继节点做 CheckPre，可能都返回 true，都尝试启动后继任务——这没问题，因为 NormalTaskRunner.Run() 内部有 CAS 抢占（UPDATE task SET status=Running WHERE version=?），只有一个 goroutine 能抢占成功，另一个会收到 ErrTaskPreemptFailed 正常退出。所以 CAS 抢占是最终的并发安全保证。

### 6.4 "PlanTaskRunner 和 NormalTaskRunner 的关系？"

> 内嵌复用。PlanTaskRunner 内嵌了 NormalTaskRunner，Plan 的每个子任务本质上就是一个普通任务——抢占、创建执行记录、gRPC 调用、状态上报全部走 NormalTaskRunner 的标准路径。PlanTaskRunner 只负责 Plan 级别的事情：创建 Plan 执行记录、解析 DSL 构建 DAG、找根节点启动、NextStep 推进 DAG。这个设计的好处是 Plan 子任务自动继承了 NormalTaskRunner 的所有能力——重试、重调度、超时中断——不需要额外适配。

### 6.5 "如果中间节点失败了，Plan 会怎样？"

> 坦率说，这是当前实现的一个不足。如果中间节点失败（非可重试的 FAILED），后继节点的 CheckPre 永远不会通过，DAG 停滞在中间状态，Plan 会一直 RUNNING。我们的补救方案有两个：短期靠 Plan 级别的超时补偿（设置 Deadline），超时后强制标记 FAILED；长期的优化是在 NextStep 中加入失败传播逻辑——子任务失败时检查是否还有其他路径可走，如果所有路径都被阻断，立即产生 Plan FAILED 事件。

---

## 7. 小结

Step 5 搭建了完整的 DAG 工作流引擎，核心数据流：

```
DSL 文本 → ANTLR4 Lexer/Parser → AST → Visitor → PlanNode DAG
                                                        │
                                                        ▼
PlanService 并发查询 DB + 组装 PlanTask ← ─ ─ ─ ─ ─ ─ ┘
         │
         ▼
PlanTaskRunner.Run() → 启动根节点
         │
         ▼ (每个子任务完成)
Kafka 完成事件 → CompleteConsumer → NextStep()
         │
         ├─ 获取 DAG + 最新执行状态
         ├─ 找后继节点 → CheckPre
         └─ 依赖满足 → go p.run(nextTask)
         │
         ▼ (叶子节点完成)
Plan 完成事件 → handlePlan() → 更新终态 + 释放锁
```

关键设计选择：
1. **ANTLR4 + Visitor** 处理中等复杂度的 DSL 解析
2. **事件驱动**（Kafka）实现毫秒级 DAG 推进
3. **Node 接口多态** 封装 6 种节点类型的行为差异
4. **内嵌复用** NormalTaskRunner，Plan 子任务继承全部单任务能力
5. **CAS 抢占** 作为并发安全的最终保证

> **下一步：Step 6 — 分片任务**，将在 DAG 工作流之上构建分片执行能力，一个大任务拆成多个子任务分发到多个执行器节点并行执行。
