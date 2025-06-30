package scheduler

import (
	"fmt"
	"time"
)

// ============================================================================
// 命名说明：
// Job        = 任务元数据/模板（类似程序、Docker镜像）
// JobExecution = 任务执行实例（类似进程、Docker容器）
// ============================================================================

// JobStatus 任务元数据的状态（调度层面）
type JobStatus int32

const (
	JobStatusActive    JobStatus = iota // 可调度状态
	JobStatusPreempted                  // 已被某调度器抢占
	JobStatusInactive                   // 停用状态（不再调度）
	JobStatusDeleted                    // 软删除状态
)

func (s JobStatus) String() string {
	switch s {
	case JobStatusActive:
		return "ACTIVE"
	case JobStatusPreempted:
		return "PREEMPTED"
	case JobStatusInactive:
		return "INACTIVE"
	case JobStatusDeleted:
		return "DELETED"
	default:
		return "UNKNOWN"
	}
}

// JobExecutionStatus 任务执行实例的状态（执行层面）
type JobExecutionStatus int32

const (
	JobExecutionStatusPrepare         JobExecutionStatus = iota // 已创建，准备执行
	JobExecutionStatusRunning                                   // 正在执行
	JobExecutionStatusSuccess                                   // 执行成功
	JobExecutionStatusFailed                                    // 执行失败（不可重试）
	JobExecutionStatusRetryableFailed                           // 执行失败（可重试）
	JobExecutionStatusCanceled                                  // 被取消
	JobExecutionStatusTimeout                                   // 执行超时
)

func (s JobExecutionStatus) String() string {
	switch s {
	case JobExecutionStatusPrepare:
		return "PREPARE"
	case JobExecutionStatusRunning:
		return "RUNNING"
	case JobExecutionStatusSuccess:
		return "SUCCESS"
	case JobExecutionStatusFailed:
		return "FAILED"
	case JobExecutionStatusRetryableFailed:
		return "RETRYABLE_FAILED"
	case JobExecutionStatusCanceled:
		return "CANCELED"
	case JobExecutionStatusTimeout:
		return "TIMEOUT"
	default:
		return "UNKNOWN"
	}
}

// Job 任务元数据表（类似程序/Docker镜像）
// 包含任务的静态信息：调度规则、配置、参数等
type Job struct {
	ID           int64     `json:"id" db:"id"`
	Name         string    `json:"name" db:"name"`
	CronExpr     string    `json:"cron_expr" db:"cron_expr"`         // cron表达式
	NextTime     time.Time `json:"next_time" db:"next_time"`         // 下次执行时间
	Status       JobStatus `json:"status" db:"status"`               // 任务状态
	Version      int64     `json:"version" db:"version"`             // 版本号（乐观锁）
	ScheduleNode string    `json:"schedule_node" db:"schedule_node"` // 当前抢占的调度节点
	UpdateTime   time.Time `json:"update_time" db:"utime"`
	CreateTime   time.Time `json:"create_time" db:"ctime"`

	// 执行配置
	GRPCConfig  *GRPCConfig       `json:"grpc_config,omitempty"`
	HTTPConfig  *HTTPConfig       `json:"http_config,omitempty"`
	RetryConfig *RetryConfig      `json:"retry_config"`
	ShardConfig *ShardConfig      `json:"shard_config,omitempty"`
	Params      map[string]string `json:"params,omitempty"`

	// 任务元信息
	Description string   `json:"description"`
	Priority    int32    `json:"priority"`
	Timeout     int32    `json:"timeout"` // 执行超时时间（秒）
	Creator     string   `json:"creator"`
	Tags        []string `json:"tags"` // 任务标签
}

// JobExecution 任务执行实例表（类似进程/Docker容器）
// 记录某次具体的任务执行：状态、进度、结果等
type JobExecution struct {
	ID        int64              `json:"id" db:"id"`
	JobID     int64              `json:"job_id" db:"job_id"`
	JobName   string             `json:"job_name" db:"job_name"` // 快照：执行时的任务名
	Status    JobExecutionStatus `json:"status" db:"status"`     // 执行状态
	StartTime *time.Time         `json:"start_time" db:"stime"`
	EndTime   *time.Time         `json:"end_time" db:"etime"`
	Duration  int64              `json:"duration" db:"duration"` // 执行耗时（毫秒）

	// 重试相关
	RetryCount    int32        `json:"retry_count" db:"retry_cnt"`
	RetryConfig   *RetryConfig `json:"retry_config"` // 快照：执行时的重试配置
	NextRetryTime *time.Time   `json:"next_retry_time" db:"next_retry_time"`

	// 执行信息
	Progress     int32             `json:"progress"`                         // 执行进度 0-100
	ExecutorNode string            `json:"executor_node" db:"executor_node"` // 执行节点
	Params       map[string]string `json:"params"`                           // 快照：执行时的参数
	Result       string            `json:"result" db:"result"`               // 执行结果
	ErrorMessage string            `json:"error_message" db:"error_msg"`     // 错误信息

	// 分片相关
	ShardID      *int32 `json:"shard_id" db:"shard_id"`                  // 分片ID
	ShardTotal   *int32 `json:"shard_total" db:"shard_total"`            // 分片总数
	ParentExecID *int64 `json:"parent_exec_id" db:"parent_execution_id"` // 父执行ID

	// 追踪信息
	TraceID string `json:"trace_id" db:"trace_id"` // 链路追踪ID
	SpanID  string `json:"span_id" db:"span_id"`   // Span ID

	CreateTime time.Time `json:"create_time" db:"ctime"`
	UpdateTime time.Time `json:"update_time" db:"utime"`
}

// ============================================================================
// 状态机转换规则
// ============================================================================

// JobStatusTransitions Job状态转换规则
var JobStatusTransitions = map[JobStatus][]JobStatus{
	JobStatusActive: {
		JobStatusPreempted, // 被抢占
		JobStatusInactive,  // 手动停用
		JobStatusDeleted,   // 删除
	},
	JobStatusPreempted: {
		JobStatusActive,   // 释放锁，回到可调度状态
		JobStatusInactive, // 手动停用
		JobStatusDeleted,  // 删除
	},
	JobStatusInactive: {
		JobStatusActive,  // 重新启用
		JobStatusDeleted, // 删除
	},
	JobStatusDeleted: {
		// 删除状态不可逆
	},
}

// JobExecutionStatusTransitions JobExecution状态转换规则
var JobExecutionStatusTransitions = map[JobExecutionStatus][]JobExecutionStatus{
	JobExecutionStatusPrepare: {
		JobExecutionStatusRunning,  // 开始执行
		JobExecutionStatusCanceled, // 取消执行
	},
	JobExecutionStatusRunning: {
		JobExecutionStatusSuccess,         // 执行成功
		JobExecutionStatusFailed,          // 执行失败
		JobExecutionStatusRetryableFailed, // 可重试失败
		JobExecutionStatusCanceled,        // 取消执行
		JobExecutionStatusTimeout,         // 执行超时
	},
	JobExecutionStatusRetryableFailed: {
		JobExecutionStatusRunning,  // 重试执行
		JobExecutionStatusFailed,   // 重试次数耗尽，最终失败
		JobExecutionStatusCanceled, // 取消重试
	},
	// 终态：Success、Failed、Canceled、Timeout 不可再转换
}

// ============================================================================
// 状态检查和转换方法
// ============================================================================

// CanTransitionTo 检查Job状态是否可以转换
func (j *Job) CanTransitionTo(newStatus JobStatus) bool {
	allowedTransitions, exists := JobStatusTransitions[j.Status]
	if !exists {
		return false
	}

	for _, allowed := range allowedTransitions {
		if allowed == newStatus {
			return true
		}
	}
	return false
}

// TransitionTo 转换Job状态
func (j *Job) TransitionTo(newStatus JobStatus) error {
	if !j.CanTransitionTo(newStatus) {
		return &StatusTransitionError{
			From: j.Status,
			To:   newStatus,
			Type: "Job",
		}
	}

	oldStatus := j.Status
	j.Status = newStatus
	j.UpdateTime = time.Now()
	j.Version++ // 乐观锁版本递增

	// 状态转换的副作用
	switch newStatus {
	case JobStatusActive:
		if oldStatus == JobStatusPreempted {
			j.ScheduleNode = "" // 清除调度节点
		}
	case JobStatusPreempted:
		// schedule_node 由调度器设置
	case JobStatusInactive, JobStatusDeleted:
		j.ScheduleNode = "" // 清除调度节点
	}

	return nil
}

// CanTransitionTo 检查JobExecution状态是否可以转换
func (je *JobExecution) CanTransitionTo(newStatus JobExecutionStatus) bool {
	allowedTransitions, exists := JobExecutionStatusTransitions[je.Status]
	if !exists {
		return false
	}

	for _, allowed := range allowedTransitions {
		if allowed == newStatus {
			return true
		}
	}
	return false
}

// TransitionTo 转换JobExecution状态
func (je *JobExecution) TransitionTo(newStatus JobExecutionStatus) error {
	if !je.CanTransitionTo(newStatus) {
		return &StatusTransitionError{
			From: je.Status,
			To:   newStatus,
			Type: "JobExecution",
		}
	}

	oldStatus := je.Status
	je.Status = newStatus
	je.UpdateTime = time.Now()

	// 状态转换的副作用
	switch newStatus {
	case JobExecutionStatusRunning:
		if oldStatus == JobExecutionStatusPrepare {
			now := time.Now()
			je.StartTime = &now
		}
	case JobExecutionStatusSuccess, JobExecutionStatusFailed,
		JobExecutionStatusCanceled, JobExecutionStatusTimeout:
		if je.StartTime != nil && je.EndTime == nil {
			now := time.Now()
			je.EndTime = &now
			je.Duration = now.Sub(*je.StartTime).Milliseconds()
		}
	}

	return nil
}

// IsTerminal 判断JobExecution是否处于终态
func (je *JobExecution) IsTerminal() bool {
	switch je.Status {
	case JobExecutionStatusSuccess, JobExecutionStatusFailed,
		JobExecutionStatusCanceled, JobExecutionStatusTimeout:
		return true
	default:
		return false
	}
}

// IsRetryable 判断JobExecution是否可以重试
func (je *JobExecution) IsRetryable() bool {
	return je.Status == JobExecutionStatusRetryableFailed &&
		je.RetryConfig != nil &&
		je.RetryCount < je.RetryConfig.MaxRetries
}

// StatusTransitionError 状态转换错误
type StatusTransitionError struct {
	From interface{}
	To   interface{}
	Type string
}

func (e *StatusTransitionError) Error() string {
	return fmt.Sprintf("invalid %s status transition from %v to %v", e.Type, e.From, e.To)
}

// ============================================================================
// 注意：GRPCConfig、HTTPConfig、RetryConfig、ShardConfig 已在 types.go 中定义
// ============================================================================
