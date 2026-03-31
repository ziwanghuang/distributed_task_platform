// Package errs 定义了分布式任务调度平台内部使用的所有业务错误码。
//
// 错误码按功能模块分组，命名规范为 Err + 模块名 + 操作描述。
// 这些错误用于业务逻辑的错误判定和传播，不直接暴露给外部 API 调用方
// （外部错误通过 gRPC status code 映射）。
//
// 错误码分组：
//   - Task 相关：任务抢占、续约、释放、配置校验等
//   - Execution 相关：执行记录状态转换、查询等
//   - TaskConfig 相关：Cron 表达式、调度参数、分片规则校验等
//   - ExecutionState 相关：状态更新、进度更新、重试结果更新等
//   - Plan 相关：DAG 工作流初始化校验
//   - Resource 相关：资源信号量限制
package errs

import "errors"

var (
	// === Task 相关错误 ===
	// 以下错误发生在任务的 CAS 抢占-续约-释放生命周期中

	ErrTaskPreemptFailed              = errors.New("任务抢占失败")               // CAS 抢占竞争失败，其他节点已抢占该任务
	ErrTaskRenewFailed                = errors.New("任务续约失败")               // 续约时发现任务已被其他节点接管或版本号不匹配
	ErrTaskReleaseFailed              = errors.New("任务释放失败")               // 释放抢占时 CAS 校验失败
	ErrTaskUpdateNextTimeFailed       = errors.New("任务更新下次执行时间失败")     // 通常因乐观锁版本号冲突
	ErrTaskUpdateScheduleParamsFailed = errors.New("任务更新调度参数失败")         // 同上

	// === Execution 相关错误 ===
	// 以下错误发生在执行记录的生命周期管理中

	ErrExecutionNotFound            = errors.New("执行记录不存在")               // 查询执行记录时 ID 不存在
	ErrInvalidTaskExecutionStatus   = errors.New("执行记录状态非法")             // 状态转换不合法（如从 Success 转到 Running）
	ErrInterruptTaskExecutionFailed = errors.New("中断任务执行失败")             // 向执行节点发送中断请求后，节点响应失败

	// === TaskConfig 相关错误 ===
	// 以下错误发生在任务创建或更新时的配置校验阶段

	ErrInvalidTaskCronExpr        = errors.New("无效的cron表达式")              // Cron 表达式解析失败
	ErrInvalidTaskScheduleNodeID  = errors.New("无效的调度节点ID")              // 指定的调度节点不存在
	ErrInvalidTaskExecutionMethod = errors.New("任务执行方式非法")              // 执行方式不在 gRPC/HTTP/Local 范围内
	ErrInvalidTaskShardingRule    = errors.New("分片规则非法")                  // 分片规则 JSON 解析失败或格式不合法
	ErrTaskShardingRuleNotFound   = errors.New("分片规则未找到")                // 分片任务缺少必要的分片规则配置

	// === ExecutionState 相关错误 ===
	// 以下错误发生在执行状态更新操作中

	ErrSetExecutionStateRunningFailed        = errors.New("设置运行状态失败")           // 将执行记录从 Pending 转为 Running 时失败
	ErrUpdateExecutionStatusFailed           = errors.New("更新任务执行记录状态失败")     // 通用状态更新失败
	ErrUpdateExecutionStatusAndEndTimeFailed = errors.New("更新任务执行记录状态和结束时间失败") // 终态（Success/Failed）更新失败
	ErrUpdateExecutionRunningProgressFailed  = errors.New("更新任务执行记录的运行状态失败")   // Running 中的进度更新失败
	ErrUpdateExecutionRetryResultFailed      = errors.New("更新任务执行记录的重试结果失败")   // 重试后的状态写回失败

	// === 重试与状态处理错误 ===

	ErrExecutionMaxRetriesExceeded   = errors.New("超过最大重试次数")             // 执行记录已达到配置的最大重试上限
	ErrExecutionStateHandlerNotFound = errors.New("执行状态处理器未找到")         // HandleReports 中状态对应的处理器未注册

	// === Plan 与资源错误 ===

	ErrInitPlanFailed = errors.New("plan和实际创建的任务不符")                    // DAG 工作流中的任务名与数据库中实际创建的任务不一致
	ErrExceedLimit    = errors.New("抢资源超出限制")                             // 资源信号量已满，无法获取更多补偿任务执行槽位
)
