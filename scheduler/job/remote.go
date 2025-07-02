package job

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	executorv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/executor/v1"
	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"gitee.com/flycash/distributed_task_platform/pkg/grpc"
	"github.com/gotomicro/ego/core/elog"
)

var _ Job = &RemoteJob{}

// RemoteJob 代表一个正在远程执行的任务实例
type RemoteJob struct {
	// 服务依赖
	svc         task.ExecutionService                           // 数据库操作服务
	grpcClients *grpc.Clients[executorv1.ExecutorServiceClient] // gRPC客户端池
	// httpClient   HttpExecutorClient                              // HTTP客户端 (暂未实现)

	pollInterval time.Duration
	logger       *elog.Component
}

// NewRemoteJob 创建 RemoteJob 实例
func NewRemoteJob(
	svc task.ExecutionService,
	grpcClients *grpc.Clients[executorv1.ExecutorServiceClient],
	pollInterval time.Duration,
) *RemoteJob {
	return &RemoteJob{
		svc:          svc,
		grpcClients:  grpcClients,
		pollInterval: pollInterval,
		logger:       elog.DefaultLogger.With(elog.FieldComponentName("scheduler.RemoteJob")),
	}
}

func (r *RemoteJob) Name() string {
	return domain.TaskExecutionMethodRemote.String()
}

func (r *RemoteJob) Run(ctx context.Context, task domain.Task) *Chans {
	reportCh := make(chan *domain.Report)
	renewFailedCh := make(chan struct{})
	errorCh := make(chan error, 1)

	// 立即返回chans
	chans := &Chans{
		Report:      reportCh,
		RenewFailed: renewFailedCh,
		Error:       errorCh,
	}

	// 在后台处理任务启动和监控
	go func() {
		defer func() {
			// 确保所有通道都会被关闭
			close(reportCh)
			close(renewFailedCh)
			close(errorCh)
		}()

		// 创建任务执行记录
		created, err := r.svc.Create(ctx, domain.TaskExecution{
			Task: domain.Task{
				ID: task.ID,
			},
			// 可以认为开始执行了，防止执行节点直接返回”终态“状态Failed，Success等
			StartTime: time.Now().UnixMilli(),
			Status:    domain.TaskExecutionStatusPrepare,
		})
		if err != nil {
			r.logger.Error("创建任务执行记录失败", elog.FieldErr(err))
			// 启动错误也通过Error通道发送
			r.sendError(ctx, errorCh, fmt.Errorf("创建任务执行记录失败: %w", err))
			return
		}

		// 发送执行请求
		needMonitoring, err := r.sendRequest(ctx, &created)
		if err != nil {
			r.logger.Error("执行任务失败", elog.FieldErr(err))
			// 启动错误也通过Error通道发送
			r.sendError(ctx, errorCh, fmt.Errorf("执行任务失败: %w", err))
			return
		}

		// 如果需要监控，继续监控
		if needMonitoring {
			err = r.monitor(ctx, reportCh, renewFailedCh, &created)
			if err != nil {
				r.logger.Error("任务监控过程中发生错误", elog.FieldErr(err))
				// 执行错误通过Error通道发送
				r.sendError(ctx, errorCh, err)
			}
		} else {
			// 如果不需要监控，任务已完成，正常结束（errorCh传递nil表示成功）
			r.sendError(ctx, errorCh, nil)
		}
	}()

	return chans
}

func (r *RemoteJob) sendError(ctx context.Context, errorCh chan error, err error) {
	select {
	case errorCh <- err:
	case <-ctx.Done():
	}
}

// sendRequest 发送执行请求，返回是否需要监控和错误
func (r *RemoteJob) sendRequest(ctx context.Context, exec *domain.TaskExecution) (needMonitoring bool, err error) {
	// 根据配置选择通信方式
	switch {
	case exec.Task.GrpcConfig != nil:
		return r.sendGRPCRequest(ctx, exec)
	case exec.Task.HTTPConfig != nil:
		return r.sendHTTPRequest(ctx, exec)
	default:
		return false, fmt.Errorf("未找到有效配置，无法发送请求")
	}
}

// sendGRPCRequest 发送gRPC执行请求
func (r *RemoteJob) sendGRPCRequest(ctx context.Context, exec *domain.TaskExecution) (needMonitoring bool, err error) {
	client := r.grpcClients.Get(exec.Task.GrpcConfig.ServiceName)
	// 发送执行请求
	resp, err := client.Execute(ctx, &executorv1.ExecuteRequest{
		Eid:      exec.ID,
		TaskId:   exec.Task.ID,
		TaskName: exec.Task.Name,
		Params:   exec.GRPCParams(),
	})
	if err != nil {
		return false, fmt.Errorf("发送gRPC请求失败: %w", err)
	}
	return r.handleExecutionState(ctx, exec, resp.GetExecutionState())
}

// handleExecutionState 处理执行节点返回的状态
func (r *RemoteJob) handleExecutionState(ctx context.Context, exec *domain.TaskExecution, state *executorv1.ExecutionState) (isRunning bool, err error) {
	// 将protobuf状态转换为domain状态
	var domainStatus domain.TaskExecutionStatus
	switch state.Status {
	case executorv1.ExecutionStatus_RUNNING:
		domainStatus = domain.TaskExecutionStatusRunning
	case executorv1.ExecutionStatus_SUCCESS:
		domainStatus = domain.TaskExecutionStatusSuccess
	case executorv1.ExecutionStatus_FAILED:
		domainStatus = domain.TaskExecutionStatusFailed
	case executorv1.ExecutionStatus_FAILED_RETRYABLE:
		domainStatus = domain.TaskExecutionStatusFailedRetryable
	default:
		return false, fmt.Errorf("未知执行状态")
	}

	// 更新状态
	err = r.updateExecutionState(ctx, exec, domainStatus, state.RunningProgress)
	if err != nil {
		return false, fmt.Errorf("更新执行状态失败: %w", err)
	}

	// 如果是终态，不需要继续监控
	return !r.isTerminalStatus(domainStatus), nil
}

// updateExecutionState 统一处理所有状态转换和更新
func (r *RemoteJob) updateExecutionState(ctx context.Context, exec *domain.TaskExecution, status domain.TaskExecutionStatus, progress int32) error {
	switch status {
	case domain.TaskExecutionStatusRunning:
		// PREPARE → RUNNING，设置状态和进度
		return r.svc.SetRunningState(ctx, exec.ID, progress)
	case domain.TaskExecutionStatusSuccess, domain.TaskExecutionStatusFailed,
		domain.TaskExecutionStatusFailedRetryable, domain.TaskExecutionStatusFailedPreempted:
		// 终态更新：设置状态和结束时间
		return r.svc.UpdateStatusAndEndTime(ctx, exec.ID, status, time.Now().UnixMilli())
	default:
		return fmt.Errorf("未知执行状态: %v", status)
	}
}

// isTerminalStatus 判断是否为终态
func (r *RemoteJob) isTerminalStatus(status domain.TaskExecutionStatus) bool {
	switch status {
	case domain.TaskExecutionStatusSuccess,
		domain.TaskExecutionStatusFailed,
		domain.TaskExecutionStatusFailedRetryable:
		return true
	default:
		return false
	}
}

// sendHTTPRequest 发送HTTP执行请求
func (r *RemoteJob) sendHTTPRequest(ctx context.Context, exec *domain.TaskExecution) (needMonitoring bool, err error) {
	// TODO: 实现HTTP客户端调用，当前为占位实现
	r.logger.Warn("HTTP执行方式尚未完全实现，使用占位逻辑",
		elog.Int64("taskId", exec.Task.ID),
		elog.String("endpoint", exec.Task.HTTPConfig.Endpoint))

	// 创建HTTP客户端，设置合理的超时时间
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// 构造请求参数 - 使用实际的任务参数而非硬编码数据
	requestData := map[string]any{
		"taskId":      exec.Task.ID,
		"taskName":    exec.Task.Name,
		"executionId": exec.ID,
		"params":      exec.Task.HTTPConfig.Params,
	}
	// 将参数转换为JSON
	jsonBytes, err := json.Marshal(requestData)
	if err != nil {
		return false, fmt.Errorf("序列化请求参数失败: %w", err)
	}
	// 发送POST请求到执行节点
	resp, err := client.Post(
		exec.Task.HTTPConfig.Endpoint,
		"application/json",
		strings.NewReader(string(jsonBytes)),
	)
	if err != nil {
		return false, fmt.Errorf("发送HTTP请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("读取HTTP响应失败: %w", err)
	}

	r.logger.Info("收到HTTP执行节点响应",
		elog.String("response", string(body)),
		elog.Int("statusCode", resp.StatusCode))

	// TODO: 解析响应，根据实际API格式处理执行状态
	// 暂时返回需要监控，由轮询机制处理后续状态更新
	return true, nil
}

// monitor 监控执行状态（轮询+上报双模式）
func (r *RemoteJob) monitor(ctx context.Context, reportCh chan *domain.Report, renewFailedCh chan struct{}, exec *domain.TaskExecution) error {
	ticker := time.NewTicker(r.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// 主动轮询执行节点
			isRunning, err := r.pollExecutionStatus(ctx, exec)
			if err != nil {
				r.logger.Warn("主动轮询任务执行状态并更新执行状态失败",
					elog.FieldErr(err))
				continue
			}
			if isRunning {
				continue
			}
			return nil
		case report := <-reportCh:
			// 收到执行点上报的任务执行状态
			if report.ExecutionState.TaskID != exec.Task.ID || report.ExecutionState.ID != exec.ID {
				r.logger.Warn("收到执行点上报的任务执行状态，与当前执行的任务不匹配",
					elog.Any("report", report))
				continue
			}
			// 更新状态
			err := r.updateExecutionState(ctx, exec, report.ExecutionState.Status, report.ExecutionState.RunningProgress)
			if err != nil {
				r.logger.Warn("执行节点上报执行状态，更新执行状态失败",
					elog.Any("report", report),
					elog.FieldErr(err))
				continue
			}

			// 如果是终态，任务完成
			if r.isTerminalStatus(report.ExecutionState.Status) {
				return nil
			}
		case <-renewFailedCh:
			// 续约失败
			err := r.svc.UpdateStatusAndEndTime(ctx, exec.ID, domain.TaskExecutionStatusFailedPreempted, time.Now().UnixMilli())
			if err != nil {
				r.logger.Error("更新续约失败状态失败", elog.FieldErr(err))
			}
			return err
		case <-ctx.Done():
			return nil
		}
	}
}

// pollExecutionStatus 主动轮询执行节点状态
func (r *RemoteJob) pollExecutionStatus(ctx context.Context, exec *domain.TaskExecution) (isRunning bool, err error) {
	if exec.Task.GrpcConfig != nil {
		return r.pollGRPCExecutionStatus(ctx, exec)
	} else if exec.Task.HTTPConfig != nil {
		return r.pollHTTPExecutionStatus(ctx, exec)
	}
	return false, fmt.Errorf("未找到有效的通信配置，无法轮询执行状态")
}

// pollGRPCExecutionStatus 通过gRPC轮询状态
func (r *RemoteJob) pollGRPCExecutionStatus(ctx context.Context, exec *domain.TaskExecution) (isRunning bool, err error) {
	client := r.grpcClients.Get(exec.Task.GrpcConfig.ServiceName)
	resp, err := client.Query(ctx, &executorv1.QueryRequest{
		Eid: exec.ID,
	})
	if err != nil {
		return false, err
	}
	return r.handleExecutionState(ctx, exec, resp.ExecutionState)
}

// pollHTTPExecutionStatus 通过HTTP轮询状态
func (r *RemoteJob) pollHTTPExecutionStatus(ctx context.Context, exec *domain.TaskExecution) (isRunning bool, err error) {
	// TODO: 实现HTTP轮询状态查询，当前为占位实现
	r.logger.Warn("HTTP状态轮询尚未实现，暂时标记为已完成",
		elog.Int64("taskId", exec.Task.ID),
		elog.Int64("executionId", exec.ID))

	// 暂时返回false表示任务已完成，避免无限轮询
	// 在实际实现中，应该向执行节点发送状态查询请求
	return false, nil
}
