# Step 1：最小可调度闭环（本地执行）

> **目标**：任务按 Cron 表达式被调度，通过 CAS 乐观锁抢占，在本地执行。这是整个系统最核心的闭环。
>
> **完成后你能看到**：启动调度器 → 日志中每 10 秒打印一次"本地执行任务 taskID=1" → 数据库中 task 的 version 递增、next_time 更新。

---

## 1. 架构总览

Step 1 的完整数据流：

```
                         ┌──────────────────────────────────────────┐
                         │          Scheduler（调度器进程）           │
                         │                                          │
  ┌──────┐  scheduleLoop │  ┌──────────┐    ┌──────────┐           │
  │ MySQL│◄──────────────┼──│ TaskSvc  │◄───│Scheduler │           │
  │      │  查询可调度任务  │  │          │    │          │           │
  │      │               │  └──────────┘    └────┬─────┘           │
  │      │               │                       │ 逐个调用          │
  │      │               │                  ┌────▼─────┐           │
  │      │  CAS 抢占      │                  │  Runner  │           │
  │      │◄──────────────┼──────────────────│Dispatcher│           │
  │      │               │                  └────┬─────┘           │
  │      │               │                       │ 路由到 Normal     │
  │      │               │                  ┌────▼─────────┐       │
  │      │  创建执行记录   │                  │NormalTaskRunner│       │
  │      │◄──────────────┼──────────────────│              │       │
  │      │               │                  └────┬─────────┘       │
  │      │               │                       │ 调用 Invoker     │
  │      │               │                  ┌────▼─────┐           │
  │      │               │                  │  Local   │           │
  │      │               │                  │ Invoker  │           │
  │      │               │                  │(模拟执行) │           │
  │      │  更新状态       │                  └──────────┘           │
  │      │◄──────────────┼──────────────────────────────────────────┘
  └──────┘               │
                         │  renewLoop（每 5s 续约，防止长任务被误判超时）
                         └──────────────────────────────────────────┘
```

### 涉及的核心组件

| 组件 | 职责 | 本步骤是否实现 |
|------|------|:------------:|
| Scheduler | 调度主循环 + 续约循环 | ✅ |
| Runner (Dispatcher) | 按任务类型路由到不同 Runner | ✅（仅 Normal） |
| NormalTaskRunner | 抢占 → 创建执行记录 → 调用 Invoker | ✅ |
| TaskAcquirer | CAS 乐观锁抢占/释放/续约 | ✅ |
| Invoker (Dispatcher) | 按协议路由到不同调用器 | ✅（仅 Local） |
| LocalInvoker | 本地模拟执行 | ✅ |
| TaskService | 查询可调度任务、更新 NextTime | ✅ |
| ExecutionService | 创建/更新执行记录 | ✅（基础版） |
| TaskRepository | domain ↔ DAO 转换 | ✅ |
| TaskDAO (GORM) | 数据库操作 | ✅ |
| LoadChecker | 负载检查，动态调频 | ❌（Step 8） |
| Prometheus Picker | 智能节点选择 | ❌（Step 8） |
| gRPC Invoker | 远程 gRPC 调用 | ❌（Step 2） |
| Kafka Producer | 异步事件上报 | ❌（Step 3） |
| 补偿器 | 重试/重调度/中断 | ❌（Step 4） |

---

## 2. 前置依赖

- Go 1.24+
- Docker（用于启动 MySQL）

### 2.1 docker-compose.yml（最小版本）

```yaml
# docker-compose.yml
services:
  mysql:
    image: mysql:8.0.29
    container_name: task-mysql
    command: --default_authentication_plugin=mysql_native_password
    environment:
      MYSQL_ROOT_PASSWORD: root
    ports:
      - "13316:3306"
    healthcheck:
      test: ["CMD", "mysqladmin", "ping", "-h", "localhost"]
      interval: 2s
      timeout: 5s
      retries: 15
    volumes:
      - task_mysql_data:/var/lib/mysql

volumes:
  task_mysql_data:
```

```bash
# 启动 MySQL
docker compose up -d mysql
```

---

## 3. 目录结构（Step 1 完成后）

```
distributed_task_platform/
├── api/proto/                  # 预留（Step 2 填充）
├── cmd/
│   └── scheduler/
│       ├── main.go             # ✅ 入口
│       └── ioc/
│           └── wire.go         # ✅ 依赖注入（Wire 或手动）
├── config/
│   └── config.yaml             # ✅ 配置
├── internal/
│   ├── domain/
│   │   ├── task.go             # ✅ Task 领域模型
│   │   └── task_execution.go   # ✅ TaskExecution 领域模型
│   ├── errs/
│   │   └── errors.go           # ✅ 错误定义
│   ├── repository/
│   │   ├── task.go             # ✅ TaskRepository 接口+实现
│   │   ├── task_execution.go   # ✅ TaskExecutionRepository 接口+实现
│   │   └── dao/
│   │       ├── init.go         # ✅ AutoMigrate
│   │       ├── task.go         # ✅ TaskDAO GORM 实现
│   │       └── task_execution.go # ✅ TaskExecutionDAO GORM 实现
│   └── service/
│       ├── scheduler/
│       │   └── scheduler.go    # ✅ 调度器主循环
│       ├── runner/
│       │   ├── types.go        # ✅ Runner 接口
│       │   ├── dispatcher.go   # ✅ Runner 路由器
│       │   └── normal_task_runner.go # ✅ 普通任务执行器
│       ├── acquirer/
│       │   └── task_acquirer.go # ✅ CAS 抢占器
│       ├── invoker/
│       │   ├── types.go        # ✅ Invoker 接口
│       │   ├── dispatcher.go   # ✅ Invoker 路由器
│       │   └── local.go        # ✅ 本地调用器
│       └── task/
│           ├── service.go      # ✅ 任务服务
│           └── execution_service.go # ✅ 执行记录服务（基础版）
├── pkg/
│   └── sqlx/
│       └── json.go             # ✅ GORM JSON 列工具
├── scripts/
│   └── mysql/
│       └── step1_schema.sql    # ✅ 建表 SQL
├── go.mod
├── go.sum
├── Makefile
└── docker-compose.yml
```

---

## 4. 实现步骤

### 4.1 初始化项目

```bash
mkdir distributed_task_platform && cd distributed_task_platform
go mod init gitee.com/flycash/distributed_task_platform

# 安装核心依赖
go get github.com/gotomicro/ego@v1.2.4
go get github.com/ego-component/egorm@v1.1.4
go get github.com/robfig/cron/v3@v3.0.1
go get github.com/google/wire@v0.6.0
```

### 4.2 数据库表结构

```sql
-- scripts/mysql/step1_schema.sql
CREATE DATABASE IF NOT EXISTS `task`;
USE `task`;

-- 任务定义表
CREATE TABLE `tasks` (
  `id`                    BIGINT AUTO_INCREMENT PRIMARY KEY,
  `biz_id`                BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT 'biz_id',
  `name`                  VARCHAR(255) NOT NULL COMMENT '任务名称',
  `cron_expr`             VARCHAR(100) NOT NULL COMMENT 'cron表达式',
  `execution_method`      ENUM('LOCAL', 'REMOTE') NOT NULL DEFAULT 'LOCAL' COMMENT '执行方式',
  `scheduling_strategy`   ENUM('CPU_PRIORITY', 'MEMORY_PRIORITY') NOT NULL DEFAULT 'CPU_PRIORITY',
  `grpc_config`           JSON COMMENT 'gRPC配置',
  `http_config`           JSON COMMENT 'HTTP配置',
  `retry_config`          JSON COMMENT '重试配置',
  `schedule_params`       JSON COMMENT '调度参数',
  `sharding_rule`         JSON COMMENT '分片规则',
  `max_execution_seconds` BIGINT NOT NULL DEFAULT 86400 COMMENT '最大执行秒数',
  `schedule_node_id`      VARCHAR(255) DEFAULT NULL COMMENT '抢占的调度节点ID',
  `next_time`             BIGINT NOT NULL DEFAULT 0 COMMENT '下次执行时间(ms)',
  `status`                ENUM('ACTIVE', 'PREEMPTED', 'INACTIVE') NOT NULL DEFAULT 'ACTIVE',
  `version`               BIGINT NOT NULL DEFAULT 1 COMMENT '乐观锁版本号',
  `plan_id`               BIGINT NOT NULL DEFAULT 0,
  `type`                  ENUM('normal', 'plan') NOT NULL DEFAULT 'normal',
  `exec_expr`             VARCHAR(2048) NOT NULL DEFAULT '' COMMENT 'DAG DSL表达式',
  `ctime`                 BIGINT NOT NULL DEFAULT 0 COMMENT '创建时间',
  `utime`                 BIGINT NOT NULL DEFAULT 0 COMMENT '更新时间',

  UNIQUE INDEX `uniq_idx_name` (`name`),
  INDEX `idx_next_time_status_utime` (`next_time`, `status`, `utime`),
  INDEX `idx_schedule_node_id_status` (`schedule_node_id`, `status`),
  INDEX `idx_plan_id` (`plan_id`),
  INDEX `idx_type` (`type`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 执行记录表
CREATE TABLE `task_executions` (
  `id`                 BIGINT AUTO_INCREMENT PRIMARY KEY,
  `task_id`            BIGINT NOT NULL COMMENT '关联的任务ID',
  `sharding_parent_id` BIGINT DEFAULT NULL COMMENT '分片父执行ID',
  `status`             ENUM('PREPARE','RUNNING','SUCCESS','FAILED','FAILED_RETRYABLE','FAILED_RESCHEDULED')
                       NOT NULL DEFAULT 'PREPARE',
  `running_progress`   INT NOT NULL DEFAULT 0 COMMENT '执行进度 0-100',
  `executor_node_id`   VARCHAR(255) DEFAULT NULL COMMENT '执行节点ID',
  `deadline`           BIGINT NOT NULL DEFAULT 0 COMMENT '截止时间(ms)',
  `start_time`         BIGINT NOT NULL DEFAULT 0 COMMENT '开始时间(ms)',
  `end_time`           BIGINT NOT NULL DEFAULT 0 COMMENT '结束时间(ms)',
  `retry_count`        BIGINT NOT NULL DEFAULT 0 COMMENT '已重试次数',
  `next_retry_time`    BIGINT NOT NULL DEFAULT 0 COMMENT '下次重试时间(ms)',

  -- 冗余任务快照字段
  `task_snapshot_name`             VARCHAR(255) NOT NULL DEFAULT '',
  `task_snapshot_type`             VARCHAR(20) NOT NULL DEFAULT 'normal',
  `task_snapshot_execution_method` VARCHAR(20) NOT NULL DEFAULT 'LOCAL',
  `task_snapshot_grpc_config`      JSON,
  `task_snapshot_http_config`      JSON,
  `task_snapshot_schedule_params`  JSON,
  `task_snapshot_retry_config`     JSON,
  `task_snapshot_plan_id`          BIGINT NOT NULL DEFAULT 0,
  `task_snapshot_plan_exec_id`     BIGINT NOT NULL DEFAULT 0,
  `task_snapshot_max_execution_seconds` BIGINT NOT NULL DEFAULT 86400,

  `ctime`              BIGINT NOT NULL DEFAULT 0,
  `utime`              BIGINT NOT NULL DEFAULT 0,

  INDEX `idx_task_id` (`task_id`),
  INDEX `idx_status_utime` (`status`, `utime`),
  INDEX `idx_sharding_parent_id` (`sharding_parent_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

> **为什么 execution 表冗余了 task 的快照字段？**
> 因为执行记录是创建时刻的快照。如果 task 的配置后来被修改了（比如改了 gRPC 服务名），正在运行的 execution 不应该受影响。这是领域驱动设计中"聚合边界"的体现。

### 4.3 领域模型

#### internal/domain/task.go

```go
package domain

import (
    "time"

    "github.com/robfig/cron/v3"
)

// TaskStatus 任务状态
type TaskStatus string

const (
    TaskStatusActive    TaskStatus = "ACTIVE"    // 可调度
    TaskStatusPreempted TaskStatus = "PREEMPTED" // 已抢占
    TaskStatusInactive  TaskStatus = "INACTIVE"  // 停止执行
)

type TaskExecutionMethod string

const (
    TaskExecutionMethodLocal  TaskExecutionMethod = "LOCAL"
    TaskExecutionMethodRemote TaskExecutionMethod = "REMOTE"
)

type TaskType string

const (
    NormalTaskType TaskType = "normal"
    PlanTaskType   TaskType = "plan"
)

// Task 任务领域模型
type Task struct {
    ID              int64
    Name            string
    CronExpr        string
    Type            TaskType
    ExecutionMethod TaskExecutionMethod
    NextTime        int64             // 下次执行时间（毫秒时间戳）
    Status          TaskStatus
    Version         int64             // CAS 版本号
    ScheduleNodeID  string
    ScheduleParams  map[string]string
    MaxExecutionSeconds int64         // 最大执行秒数，默认 86400（24h）
    CTime           int64
    UTime           int64

    // Step 1 暂不使用，预留接口
    // GrpcConfig     *GrpcConfig
    // HTTPConfig     *HTTPConfig
    // RetryConfig    *RetryConfig
    // ShardingRule   *ShardingRule
    // ExecExpr       string
    // PlanID         int64
    // PlanExecID     int64
}

// CalculateNextTime 根据 Cron 表达式计算下次执行时间
func (t *Task) CalculateNextTime() (time.Time, error) {
    p := cron.NewParser(
        cron.Second | cron.Minute | cron.Hour |
        cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
    )
    s, err := p.Parse(t.CronExpr)
    if err != nil {
        return time.Time{}, err
    }
    return s.Next(time.Now()), nil
}
```

> **设计决策**：Step 1 的 Task 结构体只保留核心字段。GrpcConfig、ShardingRule 等在后续步骤加入时再取消注释。这样代码能编译通过，又预留了扩展空间。

#### internal/domain/task_execution.go

```go
package domain

// TaskExecutionStatus 执行状态
type TaskExecutionStatus string

const (
    TaskExecutionStatusPrepare TaskExecutionStatus = "PREPARE"
    TaskExecutionStatusRunning TaskExecutionStatus = "RUNNING"
    TaskExecutionStatusSuccess TaskExecutionStatus = "SUCCESS"
    TaskExecutionStatusFailed  TaskExecutionStatus = "FAILED"
)

// ExecutionState 执行节点上报的状态
type ExecutionState struct {
    ID       int64
    Status   TaskExecutionStatus
    Progress int32
}

// TaskExecution 执行记录领域模型
type TaskExecution struct {
    ID              int64
    Status          TaskExecutionStatus
    StartTime       int64
    EndTime         int64
    RunningProgress int32
    Task            Task // 创建时刻从 Task 冗余的快照
    CTime           int64
    UTime           int64
}
```

### 4.4 错误定义

#### internal/errs/errors.go

```go
package errs

import "errors"

var (
    ErrTaskPreemptFailed  = errors.New("任务抢占失败：版本号不匹配或状态不对")
    ErrInvalidTaskCronExpr = errors.New("无效的 Cron 表达式")
    ErrTaskNotFound       = errors.New("任务不存在")
    ErrExecutionNotFound  = errors.New("执行记录不存在")
)
```

### 4.5 数据访问层（DAO）

#### internal/repository/dao/task.go

这是 Step 1 最核心的文件之一——CAS 抢占就在这里实现。

```go
package dao

import (
    "context"
    "database/sql"
    "time"

    "gitee.com/flycash/distributed_task_platform/internal/errs"
    "gitee.com/flycash/distributed_task_platform/pkg/sqlx"
    "github.com/ego-component/egorm"
    "gorm.io/gorm"
)

// Task DAO 对象，映射 tasks 表
type Task struct {
    ID                  int64              `gorm:"primaryKey;autoIncrement"`
    Name                string             `gorm:"type:varchar(255);not null;uniqueIndex:uniq_idx_name"`
    CronExpr            string             `gorm:"type:varchar(100);not null"`
    ExecutionMethod     string             `gorm:"type:ENUM('LOCAL','REMOTE');not null;default:'LOCAL'"`
    ScheduleNodeID      sql.NullString     `gorm:"type:varchar(255)"`
    ScheduleParams      sqlx.JSONColumn[map[string]string] `gorm:"type:json"`
    MaxExecutionSeconds int64              `gorm:"type:bigint;not null;default:86400"`
    NextTime            int64              `gorm:"type:bigint;not null;index:idx_next_time_status_utime,priority:1"`
    Status              string             `gorm:"type:ENUM('ACTIVE','PREEMPTED','INACTIVE');not null;default:'ACTIVE';index:idx_next_time_status_utime,priority:2"`
    Version             int64              `gorm:"type:bigint;not null;default:1"`
    Type                string             `gorm:"type:ENUM('normal','plan');not null;default:'normal'"`
    Ctime               int64              `gorm:"comment:创建时间"`
    Utime               int64              `gorm:"index:idx_next_time_status_utime,priority:3;comment:更新时间"`
}

func (Task) TableName() string { return "tasks" }

// TaskDAO 接口
type TaskDAO interface {
    Create(ctx context.Context, task Task) (*Task, error)
    GetByID(ctx context.Context, id int64) (*Task, error)
    FindSchedulableTasks(ctx context.Context, preemptedTimeoutMs int64, limit int) ([]*Task, error)
    Acquire(ctx context.Context, id, version int64, scheduleNodeID string) (*Task, error)
    Renew(ctx context.Context, scheduleNodeID string) error
    Release(ctx context.Context, id int64, scheduleNodeID string) (*Task, error)
    UpdateNextTime(ctx context.Context, id, version, nextTime int64) (*Task, error)
}

type GORMTaskDAO struct {
    db *egorm.Component
}

func NewGORMTaskDAO(db *egorm.Component) *GORMTaskDAO {
    return &GORMTaskDAO{db: db}
}

func (g *GORMTaskDAO) Create(ctx context.Context, task Task) (*Task, error) {
    now := time.Now().UnixMilli()
    task.Ctime = now
    task.Utime = now
    err := g.db.WithContext(ctx).Create(&task).Error
    return &task, err
}

func (g *GORMTaskDAO) GetByID(ctx context.Context, id int64) (*Task, error) {
    var task Task
    err := g.db.WithContext(ctx).Where("id = ?", id).First(&task).Error
    return &task, err
}

// FindSchedulableTasks 查询可调度任务
// 条件1：状态 = ACTIVE 且 next_time <= 当前时间
// 条件2：状态 = PREEMPTED 但 utime 超过超时时间（僵尸任务恢复）
func (g *GORMTaskDAO) FindSchedulableTasks(ctx context.Context,
    preemptedTimeoutMs int64, limit int) ([]*Task, error) {

    now := time.Now().UnixMilli()
    var tasks []*Task
    err := g.db.WithContext(ctx).
        Where("(status = ? AND next_time <= ?) OR (status = ? AND utime < ?)",
            "ACTIVE", now,
            "PREEMPTED", now-preemptedTimeoutMs,
        ).
        Limit(limit).
        Find(&tasks).Error
    return tasks, err
}

// Acquire CAS 抢占任务 —— Step 1 最核心的 SQL
// UPDATE tasks SET status='PREEMPTED', version=version+1,
//   schedule_node_id=?, utime=?
// WHERE id=? AND version=?
func (g *GORMTaskDAO) Acquire(ctx context.Context,
    id, version int64, scheduleNodeID string) (*Task, error) {

    now := time.Now().UnixMilli()
    result := g.db.WithContext(ctx).
        Model(&Task{}).
        Where("id = ? AND version = ?", id, version).
        Updates(map[string]any{
            "status":           "PREEMPTED",
            "schedule_node_id": scheduleNodeID,
            "version":          gorm.Expr("version + 1"),
            "utime":            now,
        })

    if result.Error != nil {
        return nil, result.Error
    }
    if result.RowsAffected == 0 {
        return nil, errs.ErrTaskPreemptFailed
    }

    // 返回抢占后的最新状态
    return g.GetByID(ctx, id)
}

// Release 释放任务
func (g *GORMTaskDAO) Release(ctx context.Context,
    id int64, scheduleNodeID string) (*Task, error) {

    now := time.Now().UnixMilli()
    result := g.db.WithContext(ctx).
        Model(&Task{}).
        Where("id = ? AND schedule_node_id = ?", id, scheduleNodeID).
        Updates(map[string]any{
            "status":           "ACTIVE",
            "schedule_node_id": nil,
            "utime":            now,
        })
    if result.Error != nil {
        return nil, result.Error
    }
    return g.GetByID(ctx, id)
}

// Renew 续约：更新当前节点所有 PREEMPTED 任务的 utime + version
func (g *GORMTaskDAO) Renew(ctx context.Context, scheduleNodeID string) error {
    now := time.Now().UnixMilli()
    return g.db.WithContext(ctx).
        Model(&Task{}).
        Where("schedule_node_id = ? AND status = ?", scheduleNodeID, "PREEMPTED").
        Updates(map[string]any{
            "utime":   now,
            "version": gorm.Expr("version + 1"),
        }).Error
}

// UpdateNextTime 更新下次执行时间（带乐观锁）
func (g *GORMTaskDAO) UpdateNextTime(ctx context.Context,
    id, version, nextTime int64) (*Task, error) {

    now := time.Now().UnixMilli()
    result := g.db.WithContext(ctx).
        Model(&Task{}).
        Where("id = ? AND version = ?", id, version).
        Updates(map[string]any{
            "next_time": nextTime,
            "status":    "ACTIVE",
            "schedule_node_id": nil,
            "version":   gorm.Expr("version + 1"),
            "utime":     now,
        })
    if result.Error != nil {
        return nil, result.Error
    }
    return g.GetByID(ctx, id)
}
```

> **CAS 抢占是怎么防止重复调度的？**
>
> ```sql
> UPDATE tasks SET status='PREEMPTED', version=version+1
> WHERE id=1 AND version=5
> ```
>
> 如果两个调度器同时读到 version=5，第一个 UPDATE 成功后 version 变成 6，
> 第二个 UPDATE 的 WHERE version=5 匹配不到任何行，RowsAffected=0，返回 ErrTaskPreemptFailed。
> 没有锁，没有事务隔离级别的要求，纯粹靠数据库的原子 UPDATE。

#### internal/repository/dao/task_execution.go（Step 1 简化版）

```go
package dao

import (
    "context"
    "time"

    "github.com/ego-component/egorm"
)

type TaskExecution struct {
    ID                                 int64  `gorm:"primaryKey;autoIncrement"`
    TaskID                             int64  `gorm:"type:bigint;not null;index:idx_task_id"`
    Status                             string `gorm:"type:ENUM('PREPARE','RUNNING','SUCCESS','FAILED','FAILED_RETRYABLE','FAILED_RESCHEDULED');not null;default:'PREPARE'"`
    RunningProgress                    int32  `gorm:"type:int;not null;default:0"`
    StartTime                          int64  `gorm:"type:bigint;not null;default:0"`
    EndTime                            int64  `gorm:"type:bigint;not null;default:0"`
    Deadline                           int64  `gorm:"type:bigint;not null;default:0"`
    TaskSnapshotName                   string `gorm:"type:varchar(255);not null;default:''"`
    TaskSnapshotType                   string `gorm:"type:varchar(20);not null;default:'normal'"`
    TaskSnapshotExecutionMethod        string `gorm:"type:varchar(20);not null;default:'LOCAL'"`
    TaskSnapshotMaxExecutionSeconds    int64  `gorm:"type:bigint;not null;default:86400"`
    Ctime                              int64
    Utime                              int64
}

func (TaskExecution) TableName() string { return "task_executions" }

type TaskExecutionDAO interface {
    Create(ctx context.Context, exec TaskExecution) (*TaskExecution, error)
    UpdateStatus(ctx context.Context, id int64, status string, progress int32) error
}

type GORMTaskExecutionDAO struct {
    db *egorm.Component
}

func NewGORMTaskExecutionDAO(db *egorm.Component) *GORMTaskExecutionDAO {
    return &GORMTaskExecutionDAO{db: db}
}

func (g *GORMTaskExecutionDAO) Create(ctx context.Context, exec TaskExecution) (*TaskExecution, error) {
    now := time.Now().UnixMilli()
    exec.Ctime = now
    exec.Utime = now
    err := g.db.WithContext(ctx).Create(&exec).Error
    return &exec, err
}

func (g *GORMTaskExecutionDAO) UpdateStatus(ctx context.Context,
    id int64, status string, progress int32) error {

    now := time.Now().UnixMilli()
    updates := map[string]any{
        "status":           status,
        "running_progress": progress,
        "utime":            now,
    }
    if status == "SUCCESS" || status == "FAILED" {
        updates["end_time"] = now
    }
    return g.db.WithContext(ctx).
        Model(&TaskExecution{}).
        Where("id = ?", id).
        Updates(updates).Error
}
```

#### internal/repository/dao/init.go

```go
package dao

import "github.com/ego-component/egorm"

func InitTables(db *egorm.Component) error {
    return db.AutoMigrate(&Task{}, &TaskExecution{})
}
```

### 4.6 Repository 层

#### internal/repository/task.go

```go
package repository

import (
    "context"

    "gitee.com/flycash/distributed_task_platform/internal/domain"
    "gitee.com/flycash/distributed_task_platform/internal/repository/dao"
)

type TaskRepository interface {
    Create(ctx context.Context, task domain.Task) (domain.Task, error)
    GetByID(ctx context.Context, id int64) (domain.Task, error)
    SchedulableTasks(ctx context.Context, preemptedTimeoutMs int64, limit int) ([]domain.Task, error)
    Acquire(ctx context.Context, id, version int64, scheduleNodeID string) (domain.Task, error)
    Release(ctx context.Context, id int64, scheduleNodeID string) (domain.Task, error)
    Renew(ctx context.Context, scheduleNodeID string) error
    UpdateNextTime(ctx context.Context, id, version, nextTime int64) (domain.Task, error)
}

type taskRepository struct {
    dao dao.TaskDAO
}

func NewTaskRepository(taskDAO dao.TaskDAO) TaskRepository {
    return &taskRepository{dao: taskDAO}
}

func (r *taskRepository) Create(ctx context.Context, task domain.Task) (domain.Task, error) {
    t, err := r.dao.Create(ctx, r.toEntity(task))
    if err != nil {
        return domain.Task{}, err
    }
    return r.toDomain(t), nil
}

func (r *taskRepository) GetByID(ctx context.Context, id int64) (domain.Task, error) {
    t, err := r.dao.GetByID(ctx, id)
    if err != nil {
        return domain.Task{}, err
    }
    return r.toDomain(t), nil
}

func (r *taskRepository) SchedulableTasks(ctx context.Context,
    preemptedTimeoutMs int64, limit int) ([]domain.Task, error) {

    tasks, err := r.dao.FindSchedulableTasks(ctx, preemptedTimeoutMs, limit)
    if err != nil {
        return nil, err
    }
    result := make([]domain.Task, 0, len(tasks))
    for _, t := range tasks {
        result = append(result, r.toDomain(t))
    }
    return result, nil
}

func (r *taskRepository) Acquire(ctx context.Context,
    id, version int64, scheduleNodeID string) (domain.Task, error) {

    t, err := r.dao.Acquire(ctx, id, version, scheduleNodeID)
    if err != nil {
        return domain.Task{}, err
    }
    return r.toDomain(t), nil
}

func (r *taskRepository) Release(ctx context.Context,
    id int64, scheduleNodeID string) (domain.Task, error) {

    t, err := r.dao.Release(ctx, id, scheduleNodeID)
    if err != nil {
        return domain.Task{}, err
    }
    return r.toDomain(t), nil
}

func (r *taskRepository) Renew(ctx context.Context, scheduleNodeID string) error {
    return r.dao.Renew(ctx, scheduleNodeID)
}

func (r *taskRepository) UpdateNextTime(ctx context.Context,
    id, version, nextTime int64) (domain.Task, error) {

    t, err := r.dao.UpdateNextTime(ctx, id, version, nextTime)
    if err != nil {
        return domain.Task{}, err
    }
    return r.toDomain(t), nil
}

// toEntity domain → DAO
func (r *taskRepository) toEntity(task domain.Task) dao.Task {
    return dao.Task{
        ID:                  task.ID,
        Name:                task.Name,
        CronExpr:            task.CronExpr,
        ExecutionMethod:     string(task.ExecutionMethod),
        MaxExecutionSeconds: task.MaxExecutionSeconds,
        NextTime:            task.NextTime,
        Status:              string(task.Status),
        Version:             task.Version,
        Type:                string(task.Type),
        Ctime:               task.CTime,
        Utime:               task.UTime,
    }
}

// toDomain DAO → domain
func (r *taskRepository) toDomain(t *dao.Task) domain.Task {
    return domain.Task{
        ID:                  t.ID,
        Name:                t.Name,
        CronExpr:            t.CronExpr,
        ExecutionMethod:     domain.TaskExecutionMethod(t.ExecutionMethod),
        MaxExecutionSeconds: t.MaxExecutionSeconds,
        NextTime:            t.NextTime,
        Status:              domain.TaskStatus(t.Status),
        Version:             t.Version,
        Type:                domain.TaskType(t.Type),
        CTime:               t.Ctime,
        UTime:               t.Utime,
    }
}
```

### 4.7 Service 层

#### internal/service/task/service.go

```go
package task

import (
    "context"
    "fmt"

    "gitee.com/flycash/distributed_task_platform/internal/domain"
    "gitee.com/flycash/distributed_task_platform/internal/errs"
    "gitee.com/flycash/distributed_task_platform/internal/repository"
)

type Service interface {
    Create(ctx context.Context, task domain.Task) (domain.Task, error)
    SchedulableTasks(ctx context.Context, preemptedTimeoutMs int64, limit int) ([]domain.Task, error)
    UpdateNextTime(ctx context.Context, id int64) (domain.Task, error)
    GetByID(ctx context.Context, id int64) (domain.Task, error)
}

type service struct {
    repo repository.TaskRepository
}

func NewService(repo repository.TaskRepository) Service {
    return &service{repo: repo}
}

func (s *service) Create(ctx context.Context, task domain.Task) (domain.Task, error) {
    nextTime, err := task.CalculateNextTime()
    if err != nil {
        return domain.Task{}, fmt.Errorf("%w: %w", errs.ErrInvalidTaskCronExpr, err)
    }
    if nextTime.IsZero() {
        return domain.Task{}, errs.ErrInvalidTaskCronExpr
    }
    task.NextTime = nextTime.UnixMilli()
    return s.repo.Create(ctx, task)
}

func (s *service) SchedulableTasks(ctx context.Context,
    preemptedTimeoutMs int64, limit int) ([]domain.Task, error) {
    return s.repo.SchedulableTasks(ctx, preemptedTimeoutMs, limit)
}

func (s *service) UpdateNextTime(ctx context.Context, id int64) (domain.Task, error) {
    task, err := s.GetByID(ctx, id)
    if err != nil {
        return domain.Task{}, err
    }
    nextTime, err := task.CalculateNextTime()
    if err != nil {
        return domain.Task{}, fmt.Errorf("%w: %w", errs.ErrInvalidTaskCronExpr, err)
    }
    if nextTime.IsZero() {
        return task, nil
    }
    task.NextTime = nextTime.UnixMilli()
    return s.repo.UpdateNextTime(ctx, task.ID, task.Version, task.NextTime)
}

func (s *service) GetByID(ctx context.Context, id int64) (domain.Task, error) {
    return s.repo.GetByID(ctx, id)
}
```

#### internal/service/task/execution_service.go（Step 1 简化版）

```go
package task

import (
    "context"
    "time"

    "gitee.com/flycash/distributed_task_platform/internal/domain"
    "gitee.com/flycash/distributed_task_platform/internal/repository/dao"
)

type ExecutionService interface {
    Create(ctx context.Context, exec domain.TaskExecution) (domain.TaskExecution, error)
    UpdateState(ctx context.Context, state domain.ExecutionState) error
}

type executionService struct {
    execDAO dao.TaskExecutionDAO
}

func NewExecutionService(execDAO dao.TaskExecutionDAO) ExecutionService {
    return &executionService{execDAO: execDAO}
}

func (s *executionService) Create(ctx context.Context,
    exec domain.TaskExecution) (domain.TaskExecution, error) {

    now := time.Now().UnixMilli()
    deadline := now + exec.Task.MaxExecutionSeconds*1000
    entity := dao.TaskExecution{
        TaskID:                          exec.Task.ID,
        Status:                          string(exec.Status),
        StartTime:                       exec.StartTime,
        Deadline:                        deadline,
        TaskSnapshotName:                exec.Task.Name,
        TaskSnapshotType:                string(exec.Task.Type),
        TaskSnapshotExecutionMethod:     string(exec.Task.ExecutionMethod),
        TaskSnapshotMaxExecutionSeconds: exec.Task.MaxExecutionSeconds,
    }
    result, err := s.execDAO.Create(ctx, entity)
    if err != nil {
        return domain.TaskExecution{}, err
    }
    exec.ID = result.ID
    return exec, nil
}

func (s *executionService) UpdateState(ctx context.Context,
    state domain.ExecutionState) error {
    return s.execDAO.UpdateStatus(ctx, state.ID, string(state.Status), state.Progress)
}
```

### 4.8 Runner + Invoker

#### internal/service/invoker/types.go

```go
package invoker

import (
    "context"

    "gitee.com/flycash/distributed_task_platform/internal/domain"
)

type Invoker interface {
    Name() string
    Run(ctx context.Context, execution domain.TaskExecution) (domain.ExecutionState, error)
}
```

#### internal/service/invoker/local.go

```go
package invoker

import (
    "context"
    "fmt"
    "time"

    "gitee.com/flycash/distributed_task_platform/internal/domain"
    "github.com/gotomicro/ego/core/elog"
)

type LocalExecuteFunc func(ctx context.Context,
    execution domain.TaskExecution) (domain.ExecutionState, error)

type LocalInvoker struct {
    executeFuncs map[string]LocalExecuteFunc
    logger       *elog.Component
}

func NewLocalInvoker(executeFuncs map[string]LocalExecuteFunc) *LocalInvoker {
    return &LocalInvoker{
        executeFuncs: executeFuncs,
        logger:       elog.DefaultLogger,
    }
}

func (l *LocalInvoker) Name() string { return "LOCAL" }

func (l *LocalInvoker) Run(ctx context.Context,
    execution domain.TaskExecution) (domain.ExecutionState, error) {

    fn, ok := l.executeFuncs[execution.Task.Name]
    if !ok {
        // 没有注册自定义函数，使用默认的模拟执行
        l.logger.Info("本地执行任务（默认模拟）",
            elog.Int64("taskID", execution.Task.ID),
            elog.String("taskName", execution.Task.Name))
        time.Sleep(2 * time.Second) // 模拟执行耗时
        return domain.ExecutionState{
            ID:       execution.ID,
            Status:   domain.TaskExecutionStatusSuccess,
            Progress: 100,
        }, nil
    }
    return fn(ctx, execution)
}
```

> **关键设计**：LocalInvoker 支持注册自定义执行函数。Step 1 不注册任何函数，走默认的 `Sleep 2s + 返回 Success`。后续做测试或 demo 时可以注册自定义逻辑。

#### internal/service/invoker/dispatcher.go（Step 1 简化版）

```go
package invoker

import (
    "context"
    "fmt"

    "gitee.com/flycash/distributed_task_platform/internal/domain"
)

type Dispatcher struct {
    local *LocalInvoker
    // Step 2 加入: grpc *GRPCInvoker
    // Step 2 加入: http *HTTPInvoker
}

func NewDispatcher(local *LocalInvoker) *Dispatcher {
    return &Dispatcher{local: local}
}

func (d *Dispatcher) Name() string { return "dispatcher" }

func (d *Dispatcher) Run(ctx context.Context,
    execution domain.TaskExecution) (domain.ExecutionState, error) {

    switch execution.Task.ExecutionMethod {
    case domain.TaskExecutionMethodLocal:
        return d.local.Run(ctx, execution)
    default:
        return domain.ExecutionState{}, fmt.Errorf(
            "Step 1 仅支持 LOCAL 执行方式，收到: %s", execution.Task.ExecutionMethod)
    }
}
```

#### internal/service/runner/types.go

```go
package runner

import (
    "context"

    "gitee.com/flycash/distributed_task_platform/internal/domain"
)

type Runner interface {
    Run(ctx context.Context, task domain.Task) error
}
```

> Step 1 的 Runner 接口只有 `Run`，`Retry` 和 `Reschedule` 在 Step 4（补偿机制）加入。

#### internal/service/runner/normal_task_runner.go（Step 1 简化版）

```go
package runner

import (
    "context"
    "fmt"
    "time"

    "gitee.com/flycash/distributed_task_platform/internal/domain"
    "gitee.com/flycash/distributed_task_platform/internal/service/acquirer"
    "gitee.com/flycash/distributed_task_platform/internal/service/invoker"
    "gitee.com/flycash/distributed_task_platform/internal/service/task"
    "github.com/gotomicro/ego/core/elog"
)

type NormalTaskRunner struct {
    nodeID       string
    taskSvc      task.Service
    execSvc      task.ExecutionService
    taskAcquirer acquirer.TaskAcquirer
    invoker      invoker.Invoker
    logger       *elog.Component
}

func NewNormalTaskRunner(
    nodeID string,
    taskSvc task.Service,
    execSvc task.ExecutionService,
    taskAcquirer acquirer.TaskAcquirer,
    invoker invoker.Invoker,
) *NormalTaskRunner {
    return &NormalTaskRunner{
        nodeID:       nodeID,
        taskSvc:      taskSvc,
        execSvc:      execSvc,
        taskAcquirer: taskAcquirer,
        invoker:      invoker,
        logger:       elog.DefaultLogger,
    }
}

func (r *NormalTaskRunner) Run(ctx context.Context, task domain.Task) error {
    // 1. CAS 抢占
    acquiredTask, err := r.taskAcquirer.Acquire(ctx, task.ID, task.Version, r.nodeID)
    if err != nil {
        r.logger.Error("任务抢占失败",
            elog.Int64("taskID", task.ID),
            elog.FieldErr(err))
        return err
    }

    // 2. 创建执行记录
    execution, err := r.execSvc.Create(ctx, domain.TaskExecution{
        Task:      acquiredTask,
        StartTime: time.Now().UnixMilli(),
        Status:    domain.TaskExecutionStatusPrepare,
    })
    if err != nil {
        r.logger.Error("创建执行记录失败", elog.FieldErr(err))
        // 释放抢占
        _ = r.taskAcquirer.Release(ctx, acquiredTask.ID, r.nodeID)
        return err
    }

    // 3. 异步执行（不阻塞调度循环）
    go func() {
        state, runErr := r.invoker.Run(ctx, execution)
        if runErr != nil {
            r.logger.Error("执行任务失败", elog.FieldErr(runErr))
            return
        }

        // 更新执行状态
        if updateErr := r.execSvc.UpdateState(ctx, state); updateErr != nil {
            r.logger.Error("更新执行状态失败", elog.FieldErr(updateErr))
        }

        // 更新下次执行时间
        if _, updateErr := r.taskSvc.UpdateNextTime(ctx, acquiredTask.ID); updateErr != nil {
            r.logger.Error("更新 NextTime 失败", elog.FieldErr(updateErr))
        }
    }()

    return nil
}
```

> **为什么用 goroutine 异步执行？**
> 调度循环一次可能拉到 100 个任务。如果每个任务都同步等 Invoker 返回（哪怕只有 2 秒），100 个任务就要 200 秒。用 goroutine 异步执行后，调度循环可以立即处理下一个任务。

### 4.9 Acquirer（CAS 抢占器）

#### internal/service/acquirer/task_acquirer.go

```go
package acquirer

import (
    "context"

    "gitee.com/flycash/distributed_task_platform/internal/domain"
    "gitee.com/flycash/distributed_task_platform/internal/repository"
)

type TaskAcquirer interface {
    Acquire(ctx context.Context, taskID, version int64, scheduleNodeID string) (domain.Task, error)
    Release(ctx context.Context, taskID int64, scheduleNodeID string) error
    Renew(ctx context.Context, scheduleNodeID string) error
}

type MySQLTaskAcquirer struct {
    taskRepo repository.TaskRepository
}

func NewTaskAcquirer(taskRepo repository.TaskRepository) *MySQLTaskAcquirer {
    return &MySQLTaskAcquirer{taskRepo: taskRepo}
}

func (a *MySQLTaskAcquirer) Acquire(ctx context.Context,
    taskID, version int64, scheduleNodeID string) (domain.Task, error) {
    return a.taskRepo.Acquire(ctx, taskID, version, scheduleNodeID)
}

func (a *MySQLTaskAcquirer) Release(ctx context.Context,
    taskID int64, scheduleNodeID string) error {
    _, err := a.taskRepo.Release(ctx, taskID, scheduleNodeID)
    return err
}

func (a *MySQLTaskAcquirer) Renew(ctx context.Context, scheduleNodeID string) error {
    return a.taskRepo.Renew(ctx, scheduleNodeID)
}
```

### 4.10 Scheduler（调度器主循环）

#### internal/service/scheduler/scheduler.go（Step 1 简化版）

```go
package scheduler

import (
    "context"
    "fmt"
    "sync"
    "time"

    "gitee.com/flycash/distributed_task_platform/internal/service/acquirer"
    "gitee.com/flycash/distributed_task_platform/internal/service/runner"
    "gitee.com/flycash/distributed_task_platform/internal/service/task"
    "github.com/gotomicro/ego/core/constant"
    "github.com/gotomicro/ego/core/elog"
    "github.com/gotomicro/ego/server"
)

var _ server.Server = &Scheduler{}

type Config struct {
    BatchSize        int           `yaml:"batchSize"`
    PreemptedTimeout time.Duration `yaml:"preemptedTimeout"`
    ScheduleInterval time.Duration `yaml:"scheduleInterval"`
    RenewInterval    time.Duration `yaml:"renewInterval"`
    BatchTimeout     time.Duration `yaml:"batchTimeout"`
}

type Scheduler struct {
    nodeID   string
    runner   runner.Runner
    taskSvc  task.Service
    acquirer acquirer.TaskAcquirer
    config   Config
    ctx      context.Context
    cancel   context.CancelFunc
    wg       sync.WaitGroup
    logger   *elog.Component
}

func NewScheduler(
    nodeID string,
    runner runner.Runner,
    taskSvc task.Service,
    acquirer acquirer.TaskAcquirer,
    config Config,
) *Scheduler {
    ctx, cancel := context.WithCancel(context.Background())
    return &Scheduler{
        nodeID:   nodeID,
        runner:   runner,
        taskSvc:  taskSvc,
        acquirer: acquirer,
        config:   config,
        ctx:      ctx,
        cancel:   cancel,
        logger:   elog.DefaultLogger,
    }
}

func (s *Scheduler) Name() string     { return fmt.Sprintf("Scheduler-%s", s.nodeID) }
func (s *Scheduler) PackageName() string { return "scheduler" }
func (s *Scheduler) Init() error      { return nil }

func (s *Scheduler) Start() error {
    s.logger.Info("启动调度器", elog.String("nodeID", s.nodeID))
    s.wg.Add(2)
    go func() { defer s.wg.Done(); s.scheduleLoop() }()
    go func() { defer s.wg.Done(); s.renewLoop() }()
    return nil
}

func (s *Scheduler) scheduleLoop() {
    for {
        if s.ctx.Err() != nil {
            s.logger.Info("调度循环结束")
            return
        }

        s.logger.Info("开始一次调度")

        // 带超时查询可调度任务
        queryCtx, cancel := context.WithTimeout(s.ctx, s.config.BatchTimeout)
        tasks, err := s.taskSvc.SchedulableTasks(queryCtx,
            s.config.PreemptedTimeout.Milliseconds(), s.config.BatchSize)
        cancel()

        if err != nil {
            s.logger.Error("获取可调度任务失败", elog.FieldErr(err))
        }

        if len(tasks) == 0 {
            s.logger.Info("没有可调度的任务")
            time.Sleep(s.config.ScheduleInterval)
            continue
        }

        s.logger.Info("发现可调度任务", elog.Int("count", len(tasks)))

        successCount := 0
        for i := range tasks {
            if err := s.runner.Run(s.ctx, tasks[i]); err != nil {
                s.logger.Error("调度任务失败",
                    elog.Int64("taskID", tasks[i].ID),
                    elog.FieldErr(err))
            } else {
                successCount++
            }
        }

        s.logger.Info("本次调度完成",
            elog.Int("success", successCount),
            elog.Int("total", len(tasks)))

        // Step 1 没有 LoadChecker，固定间隔
        time.Sleep(s.config.ScheduleInterval)
    }
}

func (s *Scheduler) renewLoop() {
    ticker := time.NewTicker(s.config.RenewInterval)
    defer ticker.Stop()
    for {
        select {
        case <-s.ctx.Done():
            return
        case <-ticker.C:
            if err := s.acquirer.Renew(s.ctx, s.nodeID); err != nil {
                s.logger.Error("续约失败", elog.FieldErr(err))
            }
        }
    }
}

func (s *Scheduler) Stop() error {
    s.cancel()
    return nil
}

func (s *Scheduler) GracefulStop(_ context.Context) error {
    s.logger.Info("优雅停止调度器")
    s.cancel()
    s.wg.Wait()
    return nil
}

func (s *Scheduler) Info() *server.ServiceInfo {
    info := server.ApplyOptions(
        server.WithName(s.Name()),
        server.WithKind(constant.ServiceProvider),
    )
    info.Healthy = s.ctx.Err() == nil
    return &info
}
```

### 4.11 JSON 列工具

#### pkg/sqlx/json.go

```go
package sqlx

import (
    "database/sql/driver"
    "encoding/json"
    "fmt"
)

// JSONColumn GORM JSON 列类型封装
type JSONColumn[T any] struct {
    Val   T
    Valid bool
}

func (j JSONColumn[T]) Value() (driver.Value, error) {
    if !j.Valid {
        return nil, nil
    }
    return json.Marshal(j.Val)
}

func (j *JSONColumn[T]) Scan(src any) error {
    if src == nil {
        j.Valid = false
        return nil
    }
    var data []byte
    switch v := src.(type) {
    case []byte:
        data = v
    case string:
        data = []byte(v)
    default:
        return fmt.Errorf("unsupported type: %T", src)
    }
    if err := json.Unmarshal(data, &j.Val); err != nil {
        return err
    }
    j.Valid = true
    return nil
}
```

### 4.12 配置文件

#### config/config.yaml

```yaml
log:
  debug: true
  level: info

mysql:
  debug: true
  dsn: "root:root@tcp(localhost:13316)/task?parseTime=True&loc=Local&charset=utf8mb4"
  maxIdleConns: 10
  maxOpenConns: 100
  connMaxLifetime: "300s"

scheduler:
  batchSize: 100
  batchTimeout: "5s"
  scheduleInterval: "10s"
  renewInterval: "5s"
  preemptedTimeout: "10m"

server:
  governor:
    host: "0.0.0.0"
    port: 9003
```

### 4.13 入口 main.go + 依赖注入

#### cmd/scheduler/main.go

```go
package main

import (
    "github.com/gotomicro/ego"
    "github.com/gotomicro/ego/core/elog"
    "github.com/gotomicro/ego/server/egovernor"

    // 手动初始化（Step 1 不用 Wire，降低入门门槛）
    "gitee.com/flycash/distributed_task_platform/internal/repository"
    "gitee.com/flycash/distributed_task_platform/internal/repository/dao"
    "gitee.com/flycash/distributed_task_platform/internal/service/acquirer"
    "gitee.com/flycash/distributed_task_platform/internal/service/invoker"
    "gitee.com/flycash/distributed_task_platform/internal/service/runner"
    "gitee.com/flycash/distributed_task_platform/internal/service/scheduler"
    "gitee.com/flycash/distributed_task_platform/internal/service/task"
    "github.com/ego-component/egorm"
    "github.com/google/uuid"
)

func main() {
    egoApp := ego.New()

    // --- 手动依赖注入 ---

    // 1. 基础设施
    db := egorm.Load("mysql").Build()
    _ = dao.InitTables(db)

    // 2. DAO 层
    taskDAO := dao.NewGORMTaskDAO(db)
    execDAO := dao.NewGORMTaskExecutionDAO(db)

    // 3. Repository 层
    taskRepo := repository.NewTaskRepository(taskDAO)

    // 4. Service 层
    taskSvc := task.NewService(taskRepo)
    execSvc := task.NewExecutionService(execDAO)

    // 5. Acquirer
    taskAcquirer := acquirer.NewTaskAcquirer(taskRepo)

    // 6. Invoker（Step 1 仅 Local）
    localInvoker := invoker.NewLocalInvoker(nil) // 不注册自定义函数
    invokerDispatcher := invoker.NewDispatcher(localInvoker)

    // 7. Runner
    nodeID := uuid.New().String()
    normalRunner := runner.NewNormalTaskRunner(
        nodeID, taskSvc, execSvc, taskAcquirer, invokerDispatcher,
    )

    // 8. Scheduler
    sch := scheduler.NewScheduler(
        nodeID, normalRunner, taskSvc, taskAcquirer,
        scheduler.Config{
            BatchSize:        100,
            BatchTimeout:     5_000_000_000,   // 5s
            ScheduleInterval: 10_000_000_000,  // 10s
            RenewInterval:    5_000_000_000,   // 5s
            PreemptedTimeout: 600_000_000_000, // 10min
        },
    )

    // --- 启动 ---
    if err := egoApp.Serve(
        egovernor.Load("server.governor").Build(),
        sch,
    ).Run(); err != nil {
        elog.Panic("startup", elog.FieldErr(err))
    }
}
```

> **为什么 Step 1 不用 Wire？**
> Wire 是好东西，但作为第一步，手动注入让你看清楚每个组件之间的依赖关系。
> Step 3-4 组件变多后再引入 Wire 自动生成，体会从"痛苦手动管理"到"自动化"的过程。

### 4.14 Makefile

```makefile
.PHONY: build run tidy fmt

build:
	go build -o bin/scheduler ./cmd/scheduler/

run:
	cd cmd/scheduler && go run main.go --config=../../config/config.yaml

tidy:
	go mod tidy

fmt:
	gofmt -l -w .
```

---

## 5. 部署验证

### 5.1 启动 MySQL 并建表

```bash
# 启动 MySQL
docker compose up -d mysql

# 等待就绪（约 15s）
docker compose logs -f mysql 2>&1 | grep "ready for connections"

# 执行建表脚本
mysql -h 127.0.0.1 -P 13316 -u root -proot < scripts/mysql/step1_schema.sql
```

### 5.2 插入测试任务

```bash
mysql -h 127.0.0.1 -P 13316 -u root -proot task -e "
INSERT INTO tasks (name, cron_expr, next_time, status, version,
    execution_method, type, max_execution_seconds, ctime, utime)
VALUES (
    'test_local_task',
    '*/10 * * * * ?',
    UNIX_TIMESTAMP(NOW())*1000,
    'ACTIVE',
    1,
    'LOCAL',
    'normal',
    86400,
    UNIX_TIMESTAMP(NOW())*1000,
    UNIX_TIMESTAMP(NOW())*1000
);
"
```

> `*/10 * * * * ?` = 每 10 秒执行一次（6 位 Cron，含秒）

### 5.3 启动调度器

```bash
make run
```

### 5.4 期望看到的日志

```
[INFO]  启动调度器  nodeID=xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
[INFO]  开始一次调度
[INFO]  发现可调度任务  count=1
[INFO]  本地执行任务（默认模拟）  taskID=1  taskName=test_local_task
[INFO]  本次调度完成  success=1  total=1
... (等 ~12s)
[INFO]  开始一次调度
[INFO]  发现可调度任务  count=1
...
```

### 5.5 验证数据库状态变化

```bash
# 查看任务的 version 和 next_time 是否在递增
mysql -h 127.0.0.1 -P 13316 -u root -proot task -e "
SELECT id, name, status, version,
    FROM_UNIXTIME(next_time/1000) as next_time_human
FROM tasks;
"

# 查看执行记录
mysql -h 127.0.0.1 -P 13316 -u root -proot task -e "
SELECT id, task_id, status, running_progress,
    FROM_UNIXTIME(start_time/1000) as start_human,
    FROM_UNIXTIME(end_time/1000) as end_human
FROM task_executions ORDER BY id DESC LIMIT 5;
"
```

### 5.6 验证 CAS 抢占（启动第二个实例）

```bash
# 终端 2：修改 Governor 端口后启动
cd cmd/scheduler && go run main.go --config=../../config/config.yaml

# 观察：同一个任务只会被一个实例抢占成功
# 另一个实例会看到日志：任务抢占失败: 版本号不匹配或状态不对
```

### 5.7 验证 Governor 健康检查

```bash
curl http://localhost:9003/debug/health
# 期望: 200 OK

curl -s http://localhost:9003/debug/pprof/ | head -5
# 期望: pprof 页面内容
```

---

## 6. 验收标准

- [ ] `go build ./...` 零错误
- [ ] 任务按 Cron 表达式准时触发（每 10s 一次）
- [ ] CAS 抢占成功，数据库 version 递增
- [ ] 启动两个实例，同一任务不会被重复执行
- [ ] 续约循环正常运行（PREEMPTED 状态的 utime 持续更新）
- [ ] 执行完成后 next_time 正确更新为下一次触发时间
- [ ] task_executions 表中有状态为 SUCCESS 的记录
- [ ] Governor 健康检查返回 200

---

## 7. 关键设计决策 & 面试话术

### 7.1 为什么用 CAS 乐观锁而不是分布式锁（Redis/etcd）？

> "Step 1 只有 MySQL 一个依赖。CAS 乐观锁不需要额外的中间件，一条 UPDATE SQL 就解决了多节点竞争。
> 性能也足够——CAS 冲突只在多节点同时抢同一个任务时发生，实际上 batchSize=100 时冲突率很低。
> 后面 Step 7 做分库分表时才引入 Redis 分布式锁，用于 ShardingLoopJob 的分片级互斥。"

### 7.2 为什么调度循环用固定间隔而不是 Cron 库？

> "调度器不是一个 Cron 守护进程——它是一个调度引擎。区别在于：Cron 守护进程按表达式触发单个任务，调度引擎批量拉取所有到期任务统一调度。
> 固定间隔（10s）轮询数据库，配合 `next_time <= now` 的查询条件，天然支持'补偿'——如果上一轮耗时过长，下一轮会把漏掉的任务一起捞出来。"

### 7.3 为什么 Invoker 要走 goroutine 异步执行？

> "调度循环每轮可能拉到 100 个任务。如果同步等每个任务执行完再处理下一个，调度延迟会线性增长。
> 异步执行让调度循环专注于'抢占+创建记录'（毫秒级），实际执行在独立 goroutine 中完成。
> 代价是如果进程崩溃，正在执行的任务会丢失——但这就是 Step 4 补偿机制要解决的问题。"

---

## 8. 下一步（Step 2 预览）

Step 1 完成后，调度器只能在本地执行任务。Step 2 的目标是：

- 引入 gRPC + Protobuf，定义 `ExecutorService`
- 引入 etcd 服务发现
- 实现 `GRPCInvoker`，让调度器通过 gRPC 下发任务到独立的执行器进程
- 编写 `example/longrunning/main.go` 示例执行器

新增依赖：etcd + Protobuf + buf 代码生成
