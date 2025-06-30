# 第一周业务流程分析

基于老板沟通笔记，梳理出的核心业务流程和技术要点：

## 1. 两种调度执行机制

### 1.1 本机抢占，本机执行（初级模式）
```go
// 调度节点和执行节点是同一个进程
type LocalScheduler struct {
    taskRepo TaskRepository
    executor Executor
}

func (s *LocalScheduler) ScheduleAndExecute() {
    // 1. 抢占任务
    task := s.PreemptTask()
    // 2. 本地执行
    result := s.executor.Execute(task)
    // 3. 更新状态
    s.UpdateTaskResult(result)
}
```

### 1.2 本机抢占，异地执行（分布式模式）
```go
// 调度中心 + 远程执行节点
type DistributedScheduler struct {
    taskRepo TaskRepository
    executorClients map[string]ExecutorClient // gRPC客户端池
    loadBalancer LoadBalancer
}

func (s *DistributedScheduler) ScheduleAndExecute() {
    // 1. 调度中心抢占任务
    task := s.PreemptTask()
    // 2. 选择执行节点
    node := s.loadBalancer.Select(task)
    // 3. 远程调用执行
    client := s.executorClients[node]
    result := client.Execute(task)
    // 4. 更新状态
    s.UpdateTaskResult(result)
}
```

## 2. 抢占机制实现

### 2.1 基于数据库CAS操作
```sql
-- 正常调度查询
SELECT * FROM task 
WHERE status = 'ACTIVE' AND next_time <= NOW() 
LIMIT 10;

-- 补偿查询（处理超时任务）
SELECT * FROM task 
WHERE status = 'PREEMPTED' 
  AND next_time <= NOW() 
  AND utime <= NOW() - INTERVAL 10 MINUTE 
LIMIT 10;

-- 抢占操作（CAS）
UPDATE task 
SET status = 'PREEMPTED', 
    version = version + 1, 
    schedule_node = 'scheduler-001',
    utime = NOW()
WHERE id = 1 
  AND version = 123 
  AND status = 'ACTIVE';
  
-- 续约操作
UPDATE task 
SET version = version + 1, 
    utime = NOW()
WHERE id = 1 
  AND version = 124 
  AND schedule_node = 'scheduler-001';
```

### 2.2 基于Redis分布式锁（备选方案）
```go
func (s *Scheduler) PreemptTaskWithRedis(taskID int64) (*Task, error) {
    lockKey := fmt.Sprintf("task_lock:%d", taskID)
    
    // 获取分布式锁
    lock := s.redisClient.SetNX(lockKey, s.nodeID, 10*time.Minute)
    if !lock {
        return nil, ErrTaskAlreadyLocked
    }
    
    // 执行任务抢占逻辑
    task, err := s.taskRepo.GetAndPreempt(taskID, s.nodeID)
    if err != nil {
        s.redisClient.Del(lockKey) // 释放锁
        return nil, err
    }
    
    return task, nil
}
```

## 3. 长任务处理机制

### 3.1 上报进度机制

#### A. 调度中心轮询（Pull模式）
```go
// 调度中心定期查询执行状态
func (s *Scheduler) PollExecutionStatus() {
    for {
        executions := s.GetRunningExecutions()
        for _, exec := range executions {
            // gRPC调用查询状态
            state, err := s.executorClient.Query(exec.ID)
            if err == nil {
                s.UpdateExecutionProgress(exec.ID, state.Progress)
            }
        }
        time.Sleep(30 * time.Second)
    }
}
```

#### B. 业务方回调调度中心（Push模式）
```go
// 执行节点主动上报进度
func (e *Executor) ReportProgress(executionID int64, progress int32) {
    state := &ExecutionState{
        ID:              executionID,
        Status:          ExecutionStatusRunning,
        RunningProgress: progress,
    }
    
    // gRPC回调调度中心
    _, err := e.reporterClient.Report(context.Background(), &ReportRequest{
        ExecutionState: state,
    })
    if err != nil {
        log.Errorf("Failed to report progress: %v", err)
    }
}
```

### 3.2 异步上报进度（Kafka消息）
```go
// 执行节点发送Kafka消息
func (e *Executor) AsyncReportProgress(state *ExecutionState) {
    message := &kafka.Message{
        Topic: "execution_report",
        Value: json.Marshal(state),
    }
    e.kafkaProducer.SendMessage(message)
}

// 调度中心消费Kafka消息
func (s *Scheduler) ConsumeProgressReports() {
    consumer := s.kafkaConsumer.Subscribe("execution_report")
    for message := range consumer.Messages() {
        var state ExecutionState
        json.Unmarshal(message.Value, &state)
        s.UpdateExecutionProgress(state.ID, state.RunningProgress)
    }
}
```

### 3.3 聚合上报进度
```go
// 批量上报所有任务进度
func (e *Executor) BatchReportProgress() {
    states := e.GetAllRunningTaskStates()
    
    // Kafka批量消息
    message := &kafka.Message{
        Topic: "execution_batch_report",
        Value: json.Marshal(&BatchReportRequest{
            Reports: states,
        }),
    }
    e.kafkaProducer.SendMessage(message)
}
```

## 4. 任务中断机制

```go
// 中断接口实现
func (e *Executor) Interrupt(ctx context.Context, req *InterruptRequest) (*InterruptResponse, error) {
    execution := e.GetExecution(req.EID)
    if execution == nil {
        return nil, ErrExecutionNotFound
    }
    
    // 1. 停止任务执行
    e.StopExecution(req.EID)
    
    // 2. 获取当前进度和恢复参数
    progress := e.GetCurrentProgress(req.EID)
    resumeParams := e.GetResumeParams(req.EID)
    
    // 3. 返回中断状态
    return &InterruptResponse{
        Succeed: true,
        RescheduledParams: resumeParams, // 用于恢复的参数
        ExecutionState: &ExecutionState{
            ID:              req.EID,
            Status:          ExecutionStatusInterrupted,
            RunningProgress: progress,
        },
    }, nil
}
```

## 5. 任务分片机制

```go
// 按offset和limit分片
func (s *Sharder) CalculateOffsetLimitShards(task *Task, totalRecords int64) ([]*ShardInfo, error) {
    shardSize := task.ShardConfig.ShardSize
    if shardSize <= 0 {
        shardSize = 1000 // 默认分片大小
    }
    
    var shards []*ShardInfo
    for offset := int64(0); offset < totalRecords; offset += int64(shardSize) {
        limit := int64(shardSize)
        if offset+limit > totalRecords {
            limit = totalRecords - offset
        }
        
        shard := &ShardInfo{
            ShardID: int32(len(shards)),
            Offset:  offset,
            Limit:   limit,
            Params: map[string]string{
                "offset": strconv.FormatInt(offset, 10),
                "limit":  strconv.FormatInt(limit, 10),
            },
        }
        shards = append(shards, shard)
    }
    
    return shards, nil
}

// 并行调度分片到不同节点
func (s *Scheduler) ExecuteShardedTask(task *Task) error {
    // 1. 计算分片
    shards, err := s.sharder.CalculateShards(task)
    if err != nil {
        return err
    }
    
    // 2. 并行执行分片
    var wg sync.WaitGroup
    results := make([]*ShardResult, len(shards))
    
    for i, shard := range shards {
        wg.Add(1)
        go func(idx int, s *ShardInfo) {
            defer wg.Done()
            
            // 选择执行节点
            node := s.loadBalancer.SelectNode(task)
            client := s.executorClients[node]
            
            // 执行分片
            req := &ExecuteRequest{
                TaskName:  task.Name,
                Params:    s.Params,
                ShardInfo: s,
            }
            
            resp, err := client.Execute(context.Background(), req)
            if err != nil {
                results[idx] = &ShardResult{
                    ShardID: s.ShardID,
                    Status:  ExecutionStatusFailed,
                    Error:   err.Error(),
                }
            } else {
                results[idx] = &ShardResult{
                    ShardID: s.ShardID,
                    Status:  resp.ExecutionState.Status,
                    Result:  "success",
                }
            }
        }(i, shard)
    }
    
    wg.Wait()
    
    // 3. 合并分片结果
    finalResult := s.sharder.MergeShardResults(results)
    s.UpdateTaskResult(task.ID, finalResult)
    
    return nil
}
```

## 6. 核心SQL操作流程

基于沟通笔记整理的完整调度流程：

```go
func (s *Scheduler) ScheduleLoop() {
    for {
        // 1. 查询可调度任务
        tasks := s.GetSchedulableTasks()
        
        for _, task := range tasks {
            // 2. 抢占任务
            success := s.PreemptTask(task)
            if !success {
                continue // 抢占失败，跳过
            }
            
            // 3. 创建执行记录
            execution := &Execution{
                TaskID:   task.ID,
                Status:   ExecutionStatusPrepare,
                TaskName: task.Name,
                Params:   task.Params,
            }
            s.executionRepo.Create(execution)
            
            // 4. 发起HTTP/gRPC调用
            go func(exec *Execution) {
                // 更新状态为RUNNING
                s.executionRepo.UpdateStatus(exec.ID, ExecutionStatusRunning)
                
                // 执行任务
                result := s.ExecuteTask(exec)
                
                // 5. 处理执行结果
                if result.Success {
                    // 计算下次执行时间
                    nextTime := s.cronParser.Next(task.CronExpr)
                    
                    // 释放任务锁，更新next_time
                    s.taskRepo.ReleaseTask(task.ID, nextTime, task.Version+1)
                    
                    // 更新执行状态
                    s.executionRepo.UpdateStatus(exec.ID, ExecutionStatusSuccess)
                } else {
                    // 处理失败情况
                    s.HandleExecutionFailure(exec, result.Error)
                }
            }(execution)
        }
        
        // 6. 续约已抢占的任务
        s.RenewPreemptedTasks()
        
        time.Sleep(10 * time.Second) // 调度间隔
    }
}

func (s *Scheduler) GetSchedulableTasks() []*Task {
    // 正常调度
    normalTasks := s.taskRepo.GetSchedulableTasks(10)
    
    // 补偿调度（超时任务）  
    timeoutTasks := s.taskRepo.GetTimeoutPreemptedTasks(10)
    
    return append(normalTasks, timeoutTasks...)
}
```

## 7. 虚拟的业务方模拟

```go
// 模拟数据生成器
func (g *DataGenerator) GenerateTestData() {
    ticker := time.NewTicker(10 * time.Millisecond) // 每秒100条
    defer ticker.Stop()
    
    for range ticker.C {
        record := &UserRecord{
            ID:       g.generateID(),
            Name:     g.generateName(),
            Email:    g.generateEmail(),
            CreateAt: time.Now(),
        }
        
        err := g.db.Insert(record)
        if err != nil {
            log.Errorf("Failed to insert record: %v", err)
        }
    }
}
```

这些业务流程构成了第一周需要实现的核心功能，为后续三周的功能扩展奠定了坚实的基础。 