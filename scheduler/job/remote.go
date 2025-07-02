package job

import (
	"context"
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
	return domain.TaskExecutorTypeRemote.String()
}

func (r *RemoteJob) Run(ctx context.Context, task domain.Task) (*Chans, error) {
	reportCh := make(chan *domain.Report)
	renewCh := make(chan bool)

	// 创建任务执行记录
	created, err := r.svc.Create(ctx, domain.TaskExecution{
		Task: domain.Task{
			ID: task.ID,
		},
		StartTime: time.Now().UnixMilli(), // 创建时自动添加
		EndTime:   0,                      // 结束时间何时添加？
		Status:    domain.TaskExecutionStatusPrepare,
	})
	if err != nil {
		r.logger.Error("创建任务执行记录失败", elog.FieldErr(err))
		return nil, fmt.Errorf("创建任务执行记录失败: %w", err)
	}

	// 发送执行请求
	needMonitoring, err := r.sendRequest(ctx, &created)
	if err != nil {
		r.logger.Error("执行任务失败", elog.FieldErr(err))
		return nil, fmt.Errorf("执行任务失败: %w", err)
	}

	// 如果任务已经完成，不需要监控
	if !needMonitoring {
		return &Chans{Report: reportCh, Renew: renewCh}, nil
	}

	// 监控执行状态
	return &Chans{Report: reportCh, Renew: renewCh}, r.monitor(ctx, reportCh, renewCh, &created)
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

// sendGRPCRequest 发送gRPC执行请求
func (r *RemoteJob) sendGRPCRequest(ctx context.Context, exec *domain.TaskExecution) (needMonitoring bool, err error) {
	client := r.grpcClients.Get(exec.Task.GrpcConfig.ServiceName)
	// 发送执行请求
	resp, err := client.Execute(ctx, &executorv1.ExecuteRequest{
		Eid:      exec.ID,
		TaskName: exec.Task.Name,
		Params:   exec.GRPCParams(),
	})
	if err != nil {
		return false, fmt.Errorf("发送gRPC请求失败: %w", err)
	}
	return r.handleExecutionState(ctx, exec, resp.GetExecutionState())
}

// handleExecutionState 处理执行节点返回的状态
func (r *RemoteJob) handleExecutionState(ctx context.Context, exec *domain.TaskExecution, state *executorv1.ExecutionState) (needMonitoring bool, err error) {
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

	// 使用统一方法更新状态
	err = r.updateExecutionState(ctx, exec, domainStatus, state.RunningProgress)
	if err != nil {
		return false, fmt.Errorf("更新执行状态失败: %w", err)
	}

	// 如果是终态，不需要继续监控
	needMonitoring = !r.isTerminalStatus(domainStatus)
	return needMonitoring, nil
}

// sendHTTPRequest 发送HTTP执行请求
func (r *RemoteJob) sendHTTPRequest(_ context.Context, exec *domain.TaskExecution) (needMonitoring bool, err error) {
	// TODO: 实现HTTP客户端调用
	client := &http.Client{
		Transport:     nil,
		CheckRedirect: nil,
		Jar:           nil,
		Timeout:       0,
	}
	// 发送JSON格式数据
	jsonData := `{"name": "张三", "age": 30}`
	resp, err := client.Post(
		exec.Task.HTTPConfig.Endpoint,
		"application/json",
		strings.NewReader(jsonData),
	)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}
	fmt.Println(string(body))
	return false, fmt.Errorf("HTTP执行方式尚未实现")
}

// monitor 监控执行状态（轮询+上报双模式）
func (r *RemoteJob) monitor(ctx context.Context, reportCh chan *domain.Report, renewCh chan bool, exec *domain.TaskExecution) error {
	ticker := time.NewTicker(r.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// 主动轮询执行节点
			err := r.pollExecutionStatus(ctx, exec)
			if err != nil {
				r.logger.Warn("主动轮询任务执行状态，并更新执行状态失败",
					elog.FieldErr(err))
				continue
			}
			return nil
		case report := <-reportCh:
			// 收到执行点上报的任务执行状态
			if report.TaskID != exec.Task.ID || report.ExecutionID != exec.ID {
				r.logger.Warn("收到执行点上报的任务执行状态，与当前执行的任务不匹配",
					elog.Any("report", report))
				continue
			}
			// 使用统一方法处理状态更新
			err := r.updateExecutionState(ctx, exec, report.Status, report.RunningProgress)
			if err != nil {
				r.logger.Warn("执行节点上报执行状态，更新执行状态失败",
					elog.Any("report", report),
					elog.FieldErr(err))
				continue
			}
			// 如果是终态，任务完成
			if r.isTerminalStatus(report.Status) {
				return nil
			}
		case ok := <-renewCh:
			if !ok {
				// 续约失败
				err := r.svc.UpdateStatusAndEndTime(ctx, exec.ID, domain.TaskExecutionStatusFailedPreempted, time.Now().UnixMilli())
				if err != nil {
					r.logger.Error("更新续约失败状态失败", elog.FieldErr(err))
				}
				return err
			}
		case <-ctx.Done():
			return nil
		}
	}
}

// pollExecutionStatus 主动轮询执行节点状态
func (r *RemoteJob) pollExecutionStatus(ctx context.Context, exec *domain.TaskExecution) error {
	if exec.Task.GrpcConfig != nil {
		return r.pollGRPCExecutionStatus(ctx, exec)
	} else if exec.Task.HTTPConfig != nil {
		return r.pollHTTPExecutionStatus(ctx, exec)
	}
	return nil
}

// pollGRPCExecutionStatus 通过gRPC轮询状态
func (r *RemoteJob) pollGRPCExecutionStatus(ctx context.Context, exec *domain.TaskExecution) error {
	client := r.grpcClients.Get(exec.Task.GrpcConfig.ServiceName)
	resp, err := client.Query(ctx, &executorv1.QueryRequest{
		Eid: exec.ID,
	})
	if err != nil {
		return err
	}
	_, err = r.handleExecutionState(ctx, exec, resp.ExecutionState)
	return err
}

// pollHTTPExecutionStatus 通过HTTP轮询状态
func (r *RemoteJob) pollHTTPExecutionStatus(_ context.Context, _ *domain.TaskExecution) error {
	// TODO: 实现HTTP轮询
	return nil
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
