package v2

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	executorv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/executor/v1"
	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/errs"
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
		TaskID:             task.ID,
		TaskName:           task.Name,
		TaskCronExpr:       task.CronExpr,
		TaskExecutorType:   task.ExecutorType,
		TaskGrpcConfig:     task.GrpcConfig,
		TaskHttpConfig:     task.HttpConfig,
		TaskRetryConfig:    task.RetryConfig,
		TaskVersion:        task.Version,
		TaskScheduleNodeID: task.ScheduleNodeID,
		StartTime:          time.Now().UnixMilli(), // 创建时自动添加
		EndTime:            0,                      // 结束时间何时添加？
		RetryCount:         0,
		NextRetryTime:      0,
		Status:             domain.TaskExecutionStatusPrepare,
	})
	if err != nil {
		r.logger.Error("创建任务执行记录失败", elog.FieldErr(err))
		return nil, fmt.Errorf("创建任务执行记录失败: %w", err)
	}

	// 发送执行请求并
	err = r.sendRequest(ctx, &created)
	if err != nil {
		r.logger.Error("执行任务失败", elog.FieldErr(err))
		return nil, fmt.Errorf("执行任务失败: %w", err)
	}

	// 更新为 RUNNING 状态
	err = r.svc.UpdateStatus(ctx, created.ID, domain.TaskExecutionStatusRunning)
	if err != nil {
		r.logger.Error("更新任务执行记录状态失败", elog.FieldErr(err))
		return nil, fmt.Errorf("更新任务执行记录状态失败: %w", err)
	}

	// 监控执行状态
	return &Chans{Report: reportCh, Renew: renewCh}, r.monitor(ctx, reportCh, renewCh, &created)
}

// sendRequest 发送执行请求并更新为RUNNING状态
func (r *RemoteJob) sendRequest(ctx context.Context, exec *domain.TaskExecution) error {
	// 根据配置选择通信方式
	var err error
	if exec.TaskGrpcConfig != nil {
		err = r.sendGRPCRequest(ctx, exec)
	} else if exec.TaskHttpConfig != nil {
		err = r.sendHTTPRequest(ctx, exec)
	} else {
		err = fmt.Errorf("未找到有效配置，无法发送请求")
	}
	return err
}

// sendGRPCRequest 发送gRPC执行请求
func (r *RemoteJob) sendGRPCRequest(ctx context.Context, exec *domain.TaskExecution) error {
	client := r.grpcClients.Get(exec.TaskGrpcConfig.ServiceName)
	// 发送执行请求
	resp, err := client.Execute(ctx, &executorv1.ExecuteRequest{
		Eid:      exec.ID,
		TaskName: exec.TaskName,
		Params:   nil, // TODO: 添加参数支持
	})
	if err != nil {
		return fmt.Errorf("发送GRPC请求失败: %w", err)
	}
	// TODO: warning: 更新状态为RUNNING  r.svc.UpdateStatus(ctx, created.ID, domain.TaskExecutionStatusRunning)
	// 处理初始响应状态
	return r.handleExecutionState(ctx, exec, resp.GetExecutionState())
}

// handleExecutionState 处理执行节点返回的状态
func (r *RemoteJob) handleExecutionState(ctx context.Context, exec *domain.TaskExecution, state *executorv1.ExecutionState) error {
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
		return errs.ErrExecutionRetryable
	default:
		return fmt.Errorf("未知执行状态")
	}
	if r.isTerminalStatus(domainStatus) {
		// 更新状态
		return r.svc.UpdateStatus(ctx, exec.ID, domainStatus)
	}
	return nil
}

// isTerminalStatus 判断是否为终态
func (r *RemoteJob) isTerminalStatus(status domain.TaskExecutionStatus) bool {
	switch status {
	case domain.TaskExecutionStatusSuccess,
		domain.TaskExecutionStatusFailed,
		domain.TaskExecutionStatusFailedPreempted:
		return true
	default:
		return false
	}
}

// sendHTTPRequest 发送HTTP执行请求
func (r *RemoteJob) sendHTTPRequest(_ context.Context, exec *domain.TaskExecution) error {
	// TODO: 实现HTTP客户端调用
	client := &http.Client{
		Transport:     nil,
		CheckRedirect: nil,
		Jar:           nil,
		Timeout:       0,
	}
	// POST JSON数据
	jsonData := `{"name": "张三", "age": 30}`
	resp, err := client.Post(
		exec.TaskHttpConfig.Endpoint,
		"application/json",
		strings.NewReader(jsonData),
	)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(body))
	return fmt.Errorf("HTTP execution not implemented yet")
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
			if report.TaskID != exec.TaskID || report.ExecutionID != exec.ID {
				r.logger.Warn("收到执行点上报的任务执行状态，与当前执行的任务不匹配",
					elog.Any("report", report))
				continue
			}
			// 收到进度上报
			// todo: 错误处理
			err := r.svc.UpdateStatus(ctx, exec.ID, report.Status)
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
				return r.svc.UpdateStatus(ctx, exec.ID, domain.TaskExecutionStatusFailedPreempted)
			}
		case <-ctx.Done():
			return nil
		}
	}
}

// pollExecutionStatus 主动轮询执行节点状态
func (r *RemoteJob) pollExecutionStatus(ctx context.Context, exec *domain.TaskExecution) error {
	if exec.TaskGrpcConfig != nil {
		return r.pollGRPCExecutionStatus(ctx, exec)
	} else if exec.TaskHttpConfig != nil {
		return r.pollHTTPExecutionStatus(ctx, exec)
	}
	return nil
}

// pollGRPCExecutionStatus 通过gRPC轮询状态
func (r *RemoteJob) pollGRPCExecutionStatus(ctx context.Context, exec *domain.TaskExecution) error {
	client := r.grpcClients.Get(exec.TaskGrpcConfig.ServiceName)
	resp, err := client.Query(ctx, &executorv1.QueryRequest{
		Eid: exec.ID,
	})
	if err != nil {
		return err
	}
	return r.handleExecutionState(ctx, exec, resp.ExecutionState)
}

// pollHTTPExecutionStatus 通过HTTP轮询状态
func (r *RemoteJob) pollHTTPExecutionStatus(_ context.Context, _ *domain.TaskExecution) error {
	// TODO: 实现HTTP轮询
	return nil
}
