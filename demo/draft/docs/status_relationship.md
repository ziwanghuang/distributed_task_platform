# Job 和 JobExecution 状态关系详解

## 1. 命名最终方案

基于您的精准类比，我推荐使用 **Job + JobExecution**：

| 概念 | 类比 | 作用 | 状态关注点 |
|------|------|------|-----------|
| **Job** | 程序/Docker镜像 | 任务元数据模板 | 调度层面（谁在处理） |
| **JobExecution** | 进程/Docker容器 | 任务执行实例 | 执行层面（如何执行） |

## 2. 状态关系类型：**有关联但相对独立**

### 2.1 关联性体现

```go
// Job状态影响JobExecution的创建时机
if job.Status == JobStatusActive && job.NextTime <= now {
    // 可以创建新的JobExecution
    execution := CreateJobExecution(job)
}

// JobExecution完成后影响Job状态
if execution.Status == JobExecutionStatusSuccess {
    // Job释放锁，回到ACTIVE状态
    job.TransitionTo(JobStatusActive)
    job.ScheduleNode = ""
    job.NextTime = calculateNextTime(job.CronExpr)
}
```

### 2.2 独立性体现

```go
// Job被停用，但正在运行的JobExecution继续执行
job.TransitionTo(JobStatusInactive)  // Job停用
// execution仍然可以：RUNNING → SUCCESS

// JobExecution失败，但Job元数据不受影响  
execution.TransitionTo(JobExecutionStatusFailed)
// job状态保持，只是会重新回到ACTIVE等待下次调度
```

## 3. 状态协调机制

### 3.1 正常执行流程

| 时间点 | Job状态 | JobExecution状态 | 说明 |
|--------|---------|------------------|------|
| T0 | `ACTIVE` | - | 等待调度 |
| T1 | `PREEMPTED` | `PREPARE` | 被抢占，创建执行实例 |
| T2 | `PREEMPTED` | `RUNNING` | 开始执行 |
| T3 | `PREEMPTED` | `SUCCESS` | 执行完成 |
| T4 | `ACTIVE` | `SUCCESS` | 释放锁，等待下次调度 |

### 3.2 异常处理流程

| 场景 | Job状态变化 | JobExecution状态变化 | 处理逻辑 |
|------|-------------|---------------------|----------|
| **执行失败** | `PREEMPTED` → `ACTIVE` | `RUNNING` → `FAILED` | 立即释放锁，允许重新调度 |
| **可重试失败** | 保持 `PREEMPTED` | `RUNNING` → `RETRYABLE_FAILED` → `RUNNING` | 创建新的JobExecution重试 |
| **任务停用** | `PREEMPTED` → `INACTIVE` | 继续执行至终态 | Job停用，但不影响正在执行的实例 |
| **执行超时** | `PREEMPTED` → `ACTIVE` | `RUNNING` → `TIMEOUT` | 释放锁，可重新调度 |

## 4. 状态检查规则

### 4.1 创建JobExecution的前置条件

```go
func CanCreateJobExecution(job *Job) bool {
    // 1. Job必须是可调度状态
    if job.Status != JobStatusActive {
        return false
    }
    
    // 2. 到达执行时间
    if job.NextTime.After(time.Now()) {
        return false
    }
    
    // 3. 成功抢占
    if !job.CanTransitionTo(JobStatusPreempted) {
        return false
    }
    
    return true
}
```

### 4.2 释放Job锁的条件

```go
func ShouldReleaseJobLock(execution *JobExecution) bool {
    // JobExecution达到终态时释放Job锁
    return execution.IsTerminal()
}

func (je *JobExecution) IsTerminal() bool {
    switch je.Status {
    case JobExecutionStatusSuccess,    // 成功
         JobExecutionStatusFailed,     // 失败
         JobExecutionStatusCanceled,   // 取消
         JobExecutionStatusTimeout:    // 超时
        return true
    default:
        return false
    }
}
```

## 5. 并发场景处理

### 5.1 多个JobExecution实例

```go
// 场景：重试时可能存在多个JobExecution
job_id = 1 (status = PREEMPTED)
├── execution_1 (status = FAILED)
├── execution_2 (status = RETRYABLE_FAILED) 
└── execution_3 (status = RUNNING)  // 当前重试

// 只有最新的RUNNING实例完成后，才释放Job锁
```

### 5.2 分片执行场景

```go
// 场景：任务分片时的状态关系
job_id = 1 (status = PREEMPTED)
├── parent_execution (status = RUNNING)
│   ├── shard_execution_1 (status = SUCCESS)
│   ├── shard_execution_2 (status = RUNNING)
│   └── shard_execution_3 (status = FAILED)

// 所有分片完成后，parent_execution才变为终态
// parent_execution终态后，Job才释放锁
```

## 6. 状态不一致处理

### 6.1 补偿机制

```go
func ReconcileJobStatus() {
    // 查找长时间PREEMPTED但没有RUNNING的JobExecution的Job
    orphanJobs := FindOrphanPreemptedJobs()
    
    for _, job := range orphanJobs {
        // 检查是否有正在运行的执行实例
        runningExecutions := FindRunningExecutions(job.ID)
        
        if len(runningExecutions) == 0 {
            // 没有正在运行的实例，释放锁
            job.TransitionTo(JobStatusActive)
            job.ScheduleNode = ""
        }
    }
}
```

### 6.2 监控告警

```go
// 状态异常监控
func MonitorStatusInconsistency() {
    alerts := []Alert{}
    
    // 1. PREEMPTED状态超过阈值时间
    longPreemptedJobs := FindLongPreemptedJobs(1 * time.Hour)
    if len(longPreemptedJobs) > 0 {
        alerts = append(alerts, Alert{
            Type: "LongPreemptedJobs",
            Message: fmt.Sprintf("发现%d个长时间被抢占的任务", len(longPreemptedJobs)),
        })
    }
    
    // 2. RUNNING状态但Job不是PREEMPTED
    inconsistentExecutions := FindInconsistentExecutions()
    if len(inconsistentExecutions) > 0 {
        alerts = append(alerts, Alert{
            Type: "StatusInconsistency", 
            Message: fmt.Sprintf("发现%d个状态不一致的执行实例", len(inconsistentExecutions)),
        })
    }
    
    // 发送告警
    for _, alert := range alerts {
        SendAlert(alert)
    }
}
```

## 7. 总结

### 状态关系特点

1. **有关联**：
   - Job状态决定何时可以创建JobExecution
   - JobExecution终态决定何时释放Job锁
   - 抢占和释放形成闭环

2. **相对独立**：
   - Job关注调度层面：谁在处理、是否可调度
   - JobExecution关注执行层面：执行进度、结果、重试
   - 各自有独立的状态机和转换规则

3. **协调机制**：
   - 通过`schedule_node`字段协调抢占
   - 通过版本号实现乐观锁
   - 通过补偿机制处理异常情况

这种设计既保证了状态的一致性，又避免了过度耦合，支持复杂的调度和执行场景。 