package domain

import (
	"strconv"
	"time"

	"gitee.com/flycash/distributed_task_platform/pkg/grpc/registry"
	"gitee.com/flycash/distributed_task_platform/pkg/retry"
	"github.com/robfig/cron/v3"
)

// TaskStatus 任务状态
type TaskStatus string

const (
	TaskStatusActive    TaskStatus = "ACTIVE"    // 可调度
	TaskStatusPreempted TaskStatus = "PREEMPTED" // 已抢占
	TaskStatusInactive  TaskStatus = "INACTIVE"  // 停止执行
)

func (t TaskStatus) String() string {
	return string(t)
}

type TaskExecutionMethod string

const (
	TaskExecutionMethodLocal  TaskExecutionMethod = "LOCAL"
	TaskExecutionMethodRemote TaskExecutionMethod = "REMOTE"
)

func (t TaskExecutionMethod) String() string {
	return string(t)
}

func (t TaskExecutionMethod) IsRemote() bool {
	return t == TaskExecutionMethodRemote
}

func (t TaskExecutionMethod) IsLocal() bool {
	return t == TaskExecutionMethodLocal
}

type TaskType string

const (
	NormalTaskType TaskType = "normal"
	PlanTaskType   TaskType = "plan"
)

func (t TaskType) String() string {
	return string(t)
}

type ShardingRuleType string

const (
	ShardingRuleTypeRange                ShardingRuleType = "range"
	ShardingRuleTypeWeightedDynamicRange ShardingRuleType = "weighted-dynamic-range"
)

func (t ShardingRuleType) String() string {
	return string(t)
}

func (t ShardingRuleType) IsRange() bool {
	return t == ShardingRuleTypeRange
}

func (t ShardingRuleType) IsWeightedDynamicRange() bool {
	return t == ShardingRuleTypeWeightedDynamicRange
}

type SchedulingStrategy string

const (
	SchedulingStrategyCPUPriority    SchedulingStrategy = "CPU_PRIORITY"
	SchedulingStrategyMemoryPriority SchedulingStrategy = "MEMORY_PRIORITY"
)

func (t SchedulingStrategy) String() string {
	return string(t)
}

func (t SchedulingStrategy) IsCPUPriority() bool {
	return t == SchedulingStrategyCPUPriority
}

func (t SchedulingStrategy) IsMemoryPriority() bool {
	return t == SchedulingStrategyMemoryPriority
}

// Task 任务领域模型
type Task struct {
	ID       int64
	Name     string
	CronExpr string
	ExecExpr string

	Type TaskType
	// 为了方便测试，这里额外引入了一种本地运行的任务
	ExecutionMethod     TaskExecutionMethod
	SchedulingStrategy  SchedulingStrategy
	GrpcConfig          *GrpcConfig
	HTTPConfig          *HTTPConfig
	RetryConfig         *RetryConfig
	MaxExecutionSeconds int64             // 最大执行秒数，默认24小时
	ScheduleNodeID      string            // 调度节点ID
	ScheduleParams      map[string]string // 调度参数（如分页偏移量、处理进度等）
	ShardingRule        *ShardingRule     // 分片规则
	NextTime            int64             // 下次执行时间戳
	Status              TaskStatus        // 任务状态
	Version             int64             // 版本号，用于乐观锁
	PlanID              int64
	PlanExecID          int64
	CTime               int64 // 创建时间戳
	UTime               int64 // 更新时间戳
}

func (t *Task) CalculateNextTime() (time.Time, error) {
	p := cron.NewParser(
		cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
	)
	s, err := p.Parse(t.CronExpr)
	if err != nil {
		return time.Time{}, err
	}
	return s.Next(time.Now()), nil
}

// UpdateScheduleParams 在领域模型上定义了“如何更新调度参数”的业务规则
func (t *Task) UpdateScheduleParams(params map[string]string) {
	// 如果传入的 params 是 nil，代表调用者无意进行任何修改。
	if params == nil {
		return // 无操作
	}

	// 如果传入的 params 是一个空的 map，代表业务意图是“重置/清空”。
	if len(params) == 0 {
		t.ScheduleParams = make(map[string]string) // 重置为空
		return
	}
	// 否则，执行“智能合并”逻辑。 如果原始参数是 nil，先初始化
	if t.ScheduleParams == nil {
		t.ScheduleParams = make(map[string]string)
	}
	for k, v := range params {
		t.ScheduleParams[k] = v
	}
}

// RetryConfig 重试配置
type RetryConfig struct {
	MaxRetries      int32 `json:"maxRetries"`
	InitialInterval int64 `json:"initialInterval"` // 毫秒
	MaxInterval     int64 `json:"maxInterval"`     // 毫秒
}

func (r *RetryConfig) ToRetryComponentConfig() retry.Config {
	return retry.Config{
		Type: "exponential",
		ExponentialBackoff: &retry.ExponentialBackoffConfig{
			InitialInterval: time.Duration(r.InitialInterval) * time.Millisecond,
			MaxInterval:     time.Duration(r.MaxInterval) * time.Millisecond,
			MaxRetries:      r.MaxRetries,
		},
	}
}

// GrpcConfig gRPC配置
type GrpcConfig struct {
	ServiceName string            `json:"serviceName"`
	Params      map[string]string `json:"params"`
}

// HTTPConfig HTTP配置
type HTTPConfig struct {
	Endpoint string            `json:"endpoint"`
	Params   map[string]string `json:"params"`
}

type ShardingRule struct {
	Type                  ShardingRuleType           `json:"type"`
	Params                map[string]string          `json:"params"`
	ExecutorNodeInstances []registry.ServiceInstance `json:"-"`
}

// ToScheduleParams 根据分片规则计算出分片任务所需要的调度参数
func (s *ShardingRule) ToScheduleParams() []map[string]string {
	// 在后台创建该任务时，应该严格校验下面的参数，此处不要再校验
	switch {
	case s.Type.IsRange():
		return (*RangeShardingRule)(s).ToScheduleParams()
	case s.Type.IsWeightedDynamicRange():
		return (*WeightedDynamicRangeShardingRule)(s).ToScheduleParams()
	default:
		return nil
	}
}

type RangeShardingRule ShardingRule

func (s *RangeShardingRule) ToScheduleParams() []map[string]string {
	scheduleParams := make([]map[string]string, 0)
	step, _ := strconv.ParseInt(s.Params["step"], 10, 64)
	totalNums, _ := strconv.ParseInt(s.Params["totalNums"], 10, 64)
	for i := range totalNums {
		mp := make(map[string]string)
		mp["start"] = strconv.FormatInt(i*step, 10)
		mp["end"] = strconv.FormatInt((i+1)*step, 10)
		scheduleParams = append(scheduleParams, mp)
	}
	return scheduleParams
}

type WeightedDynamicRangeShardingRule ShardingRule

func (w *WeightedDynamicRangeShardingRule) ToScheduleParams() []map[string]string {
	// 解析业务方总任务数
	totalTasks, err := strconv.ParseInt(w.Params["total_tasks"], 10, 64)
	if err != nil {
		return nil
	}

	// 计算总权重
	var totalWeight int64
	for i := range w.ExecutorNodeInstances {
		totalWeight += w.ExecutorNodeInstances[i].Weight
	}
	if totalWeight == 0 {
		// 所有执行节点的总权重为0，无法进行分片
		return nil
	}

	// 计算每个执行节点对应的分片区w
	scheduleParams := make([]map[string]string, 0, len(w.ExecutorNodeInstances))

	var start, step, end int64
	// 遍历 N-1 个节点，计算它们的分片
	for i := 0; i < len(w.ExecutorNodeInstances)-1; i++ {
		// 根据权重计算当前节点应处理的任务数
		// 使用浮点数保证精度，然后转换为整数
		share := float64(totalTasks) * (float64(w.ExecutorNodeInstances[i].Weight) / float64(totalWeight))
		step = int64(share)
		end = start + step

		scheduleParams = append(scheduleParams, map[string]string{
			"start": strconv.FormatInt(start, 10),
			"end":   strconv.FormatInt(end, 10),
		})
		start = end // 下一个分片的起点是当前分片的终点
	}

	// 最后一个节点获得所有剩余的任务，以避免舍入误差导致任务丢失
	scheduleParams = append(scheduleParams, map[string]string{
		"start": strconv.FormatInt(start, 10),
		"end":   strconv.FormatInt(totalTasks, 10), // 终点即为任务总数
	})
	return scheduleParams
}
