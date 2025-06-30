package scheduler

import (
	"context"
	"time"
)

// TaskStatus 任务状态
type TaskStatus int32

const (
	TaskStatusActive    TaskStatus = iota // 可调度
	TaskStatusPreempted                   // 已抢占
	TaskStatusInactive                    // 停止执行
)

// ExecutionStatus 执行状态
type ExecutionStatus int32

const (
	ExecutionStatusPrepare         ExecutionStatus = iota // 初始化，但还没节点执行
	ExecutionStatusRunning                                // 有节点在执行
	ExecutionStatusRetryableFailed                        // 失败，但可以重试
	ExecutionStatusFailed                                 // 失败
	ExecutionStatusSuccess                                // 成功
	ExecutionStatusFailedPreempted                        // 因抢占失败
)

// Task 任务表结构
type Task struct {
	ID           int64             `json:"id" db:"id"`
	Name         string            `json:"name" db:"name"`
	CronExpr     string            `json:"cron_expr" db:"cron_expr"`
	NextTime     time.Time         `json:"next_time" db:"next_time"`
	Status       TaskStatus        `json:"status" db:"status"`
	Version      int64             `json:"version" db:"version"`
	ScheduleNode string            `json:"schedule_node" db:"schedule_node"`
	UpdateTime   time.Time         `json:"update_time" db:"utime"`
	CreateTime   time.Time         `json:"create_time" db:"ctime"`
	GRPCConfig   *GRPCConfig       `json:"grpc_config,omitempty"`
	HTTPConfig   *HTTPConfig       `json:"http_config,omitempty"`
	RetryConfig  *RetryConfig      `json:"retry_config"`
	ShardConfig  *ShardConfig      `json:"shard_config,omitempty"`
	Params       map[string]string `json:"params,omitempty"`
}

// Execution 执行记录表结构
type Execution struct {
	ID            int64             `json:"id" db:"id"`
	TaskID        int64             `json:"task_id" db:"task_id"`
	TaskName      string            `json:"task_name" db:"task_name"`
	Status        ExecutionStatus   `json:"status" db:"status"`
	StartTime     *time.Time        `json:"start_time" db:"stime"`
	EndTime       *time.Time        `json:"end_time" db:"etime"`
	RetryCount    int32             `json:"retry_count" db:"retry_cnt"`
	RetryConfig   *RetryConfig      `json:"retry_config"`
	NextRetryTime *time.Time        `json:"next_retry_time" db:"next_retry_time"`
	Progress      int32             `json:"progress"`
	ExecutorNode  string            `json:"executor_node" db:"executor_node"`
	Params        map[string]string `json:"params"`
	Result        string            `json:"result" db:"result"`
	ErrorMessage  string            `json:"error_message" db:"error_msg"`
	CreateTime    time.Time         `json:"create_time" db:"ctime"`
	UpdateTime    time.Time         `json:"update_time" db:"utime"`
}

// GRPCConfig gRPC服务配置
type GRPCConfig struct {
	ServiceName string `json:"service_name"`
	Address     string `json:"address"`
	Timeout     int32  `json:"timeout"` // 秒
}

// HTTPConfig HTTP服务配置
type HTTPConfig struct {
	Endpoint string `json:"endpoint"`
	Method   string `json:"method"`
	Timeout  int32  `json:"timeout"` // 秒
}

// RetryConfig 重试配置
type RetryConfig struct {
	MaxRetries      int32         `json:"max_retries"`
	InitialInterval time.Duration `json:"initial_interval"`
	MaxInterval     time.Duration `json:"max_interval"`
	Multiplier      float64       `json:"multiplier"`
}

// ShardConfig 分片配置
type ShardConfig struct {
	Enable     bool   `json:"enable"`
	ShardCount int32  `json:"shard_count"`
	ShardSize  int32  `json:"shard_size"`
	Strategy   string `json:"strategy"` // offset_limit, hash, range
}

// ExecutionState 执行状态（对应proto定义）
type ExecutionState struct {
	ID              int64           `json:"id"`
	TaskName        string          `json:"task_name"`
	Status          ExecutionStatus `json:"status"`
	RunningProgress int32           `json:"running_progress"`
}

// 调度器接口
type Scheduler interface {
	// 启动调度器
	Start(ctx context.Context) error
	// 停止调度器
	Stop(ctx context.Context) error
	// 抢占任务
	PreemptTask(ctx context.Context, task *Task) (*Execution, error)
	// 续约任务
	RenewTask(ctx context.Context, taskID int64) error
	// 执行任务
	ExecuteTask(ctx context.Context, execution *Execution) error
	// 查询执行状态
	QueryExecution(ctx context.Context, executionID int64) (*ExecutionState, error)
	// 中断任务
	InterruptTask(ctx context.Context, executionID int64) error
}

// 执行器接口
type Executor interface {
	// 执行任务
	Execute(ctx context.Context, req *ExecuteRequest) (*ExecuteResponse, error)
	// 中断任务
	Interrupt(ctx context.Context, req *InterruptRequest) (*InterruptResponse, error)
	// 查询状态
	Query(ctx context.Context, req *QueryRequest) (*QueryResponse, error)
}

// 任务仓库接口
type TaskRepository interface {
	// 获取可调度的任务
	GetSchedulableTasks(ctx context.Context, limit int) ([]*Task, error)
	// 获取超时的抢占任务
	GetTimeoutPreemptedTasks(ctx context.Context, limit int) ([]*Task, error)
	// 抢占任务
	PreemptTask(ctx context.Context, taskID int64, scheduleNode string, version int64) error
	// 续约任务
	RenewTask(ctx context.Context, taskID int64, scheduleNode string, version int64) error
	// 释放任务并更新下次执行时间
	ReleaseTask(ctx context.Context, taskID int64, nextTime time.Time, version int64) error
}

// 执行记录仓库接口
type ExecutionRepository interface {
	// 创建执行记录
	Create(ctx context.Context, execution *Execution) error
	// 更新执行状态
	UpdateStatus(ctx context.Context, executionID int64, status ExecutionStatus) error
	// 更新执行进度
	UpdateProgress(ctx context.Context, executionID int64, progress int32) error
	// 获取执行记录
	GetByID(ctx context.Context, executionID int64) (*Execution, error)
	// 批量获取执行记录
	BatchGetByIDs(ctx context.Context, executionIDs []int64) ([]*Execution, error)
}

// 负载均衡器接口
type LoadBalancer interface {
	// 选择执行节点
	SelectNode(ctx context.Context, task *Task) (string, error)
	// 获取节点负载信息
	GetNodeLoad(ctx context.Context, nodeID string) (*NodeLoad, error)
	// 触发再均衡
	Rebalance(ctx context.Context) error
}

// 节点负载信息
type NodeLoad struct {
	NodeID        string    `json:"node_id"`
	CPUUsage      float64   `json:"cpu_usage"`
	MemoryUsage   float64   `json:"memory_usage"`
	TaskCount     int32     `json:"task_count"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
}

// 进度上报器接口
type ProgressReporter interface {
	// 单个上报
	Report(ctx context.Context, state *ExecutionState) error
	// 批量上报
	BatchReport(ctx context.Context, states []*ExecutionState) error
}

// 分片器接口
type Sharder interface {
	// 计算分片
	CalculateShards(ctx context.Context, task *Task) ([]*ShardInfo, error)
	// 合并分片结果
	MergeShardResults(ctx context.Context, results []*ShardResult) (*ExecutionResult, error)
}

// 分片信息
type ShardInfo struct {
	ShardID int32             `json:"shard_id"`
	Offset  int64             `json:"offset"`
	Limit   int64             `json:"limit"`
	Params  map[string]string `json:"params"`
}

// 分片结果
type ShardResult struct {
	ShardID int32           `json:"shard_id"`
	Status  ExecutionStatus `json:"status"`
	Result  string          `json:"result"`
	Error   string          `json:"error,omitempty"`
}

// 执行结果
type ExecutionResult struct {
	Status       ExecutionStatus `json:"status"`
	Result       string          `json:"result"`
	ErrorMessage string          `json:"error_message,omitempty"`
}

// 请求和响应结构体
type ExecuteRequest struct {
	EID       int64             `json:"eid"`
	TaskName  string            `json:"task_name"`
	Params    map[string]string `json:"params"`
	ShardInfo *ShardInfo        `json:"shard_info,omitempty"`
}

type ExecuteResponse struct {
	ExecutionState *ExecutionState `json:"execution_state"`
}

type InterruptRequest struct {
	EID int64 `json:"eid"`
}

type InterruptResponse struct {
	Succeed           bool              `json:"succeed"`
	RescheduledParams map[string]string `json:"rescheduled_params"`
	ExecutionState    *ExecutionState   `json:"execution_state"`
}

type QueryRequest struct {
	EID int64 `json:"eid"`
}

type QueryResponse struct {
	ExecutionState *ExecutionState `json:"execution_state"`
}
