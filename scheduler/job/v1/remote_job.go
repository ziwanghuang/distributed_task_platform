package v1

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
	exec domain.TaskExecution
	// 服务依赖
	svc         task.ExecutionService                           // 数据库操作服务
	grpcClients *grpc.Clients[executorv1.ExecutorServiceClient] // gRPC客户端池
	// httpClient   HttpExecutorClient                              // HTTP客户端 (暂未实现)

	reportCh     chan *domain.Report // 进度上报channel
	pollInterval time.Duration
	ctx          context.Context
	cancel       context.CancelFunc
	logger       *elog.Component
}

// NewRemoteJob 创建 RemoteJob 实例
func NewRemoteJob(
	svc task.ExecutionService,
	grpcClients *grpc.Clients[executorv1.ExecutorServiceClient],
	pollInterval time.Duration,
) *RemoteJob {
	ctx, cancel := context.WithCancel(context.Background())
	return &RemoteJob{
		svc:          svc,
		grpcClients:  grpcClients,
		reportCh:     make(chan *domain.Report, 10),
		pollInterval: pollInterval,
		ctx:          ctx,
		cancel:       cancel,
		logger:       elog.DefaultLogger.With(elog.FieldComponentName("scheduler.RemoteJob")),
	}
}

func (j *RemoteJob) Type() string {
	return domain.TaskExecutorTypeRemote.String()
}

// Run 执行完整的任务流程
func (j *RemoteJob) Run(ctx context.Context, task domain.Task) error {
	created, err := j.svc.Create(ctx, domain.TaskExecution{
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
		return fmt.Errorf("创建任务执行记录失败: %w", err)
	}
	j.exec = created

	// 发送执行请求并更新为RUNNING状态
	err = j.sendRequest(ctx)
	if err != nil {
		return fmt.Errorf("执行任务失败: %w", err)
	}
	// 监控执行状态
	return j.wait(ctx)
}

// sendRequest 发送执行请求并更新为RUNNING状态
func (j *RemoteJob) sendRequest(ctx context.Context) error {
	// 根据配置选择通信方式
	var err error
	if j.exec.TaskGrpcConfig != nil {
		err = j.sendGRPCRequest(ctx, j.exec.TaskGrpcConfig)
	} else if j.exec.TaskHttpConfig != nil {
		err = j.sendHTTPRequest(j.exec.TaskHttpConfig)
	} else {
		err = fmt.Errorf("未找到有效配置，无法发送请求")
	}
	return err
}

// sendGRPCRequest 发送gRPC执行请求
func (j *RemoteJob) sendGRPCRequest(ctx context.Context, config *domain.GrpcConfig) error {
	client := j.grpcClients.Get(config.ServiceName)
	// 发送执行请求
	resp, err := client.Execute(ctx, &executorv1.ExecuteRequest{
		Eid:      j.exec.ID,
		TaskName: j.exec.TaskName,
		Params:   nil, // TODO: 添加参数支持
	})
	if err != nil {
		return fmt.Errorf("发送GRPC请求失败: %w", err)
	}
	// 更新状态为RUNNING
	// todo: 	resp.GetExecutionState().GetStatus()
	err = j.svc.UpdateStatus(ctx, j.exec.ID, domain.TaskExecutionStatusRunning)
	if err != nil {
		return fmt.Errorf("更新任务执行记录状态失败: %w", err)
	}
	// 处理初始响应状态
	return j.handleExecutionState(ctx, resp.ExecutionState)
}

// sendHTTPRequest 发送HTTP执行请求
func (j *RemoteJob) sendHTTPRequest(config *domain.HttpConfig) error {
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
		config.Endpoint,
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

// wait 监控执行状态（轮询+上报双模式）
func (j *RemoteJob) wait(ctx context.Context) error {
	pollTicker := time.NewTicker(j.pollInterval)
	defer pollTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil

		case <-j.ctx.Done():
			// 收到停止信号
			return nil

		case report := <-j.reportCh:
			if report.TaskID != j.exec.TaskID || report.ExecutionID != j.exec.ID {
				j.logger.Warn("执行节点上报执行状态，收到不属于当前job的上报",
					elog.Any("report", report))
				continue
			}
			// 收到进度上报
			// todo: 错误处理
			err := j.svc.UpdateStatus(ctx, j.exec.ID, report.Status)
			if err != nil {
				j.logger.Warn("执行节点上报执行状态，更新执行状态失败",
					elog.Any("report", report),
					elog.FieldErr(err))
				continue
			}
			// 如果是终态，任务完成
			if j.isTerminalStatus(report.Status) {
				return nil
			}
		case <-pollTicker.C:
			// 主动轮询执行节点
			err := j.pollExecutionStatus(ctx)
			if err != nil {
				j.logger.Warn("主动拉取任务执行状态，更新执行状态失败",
					elog.FieldErr(err))
				continue
			}
			return nil
		}
	}
}

// pollExecutionStatus 主动轮询执行节点状态
func (j *RemoteJob) pollExecutionStatus(ctx context.Context) error {
	if j.exec.TaskGrpcConfig != nil {
		return j.pollGrpcStatus(ctx, j.exec.TaskGrpcConfig)
	} else if j.exec.TaskHttpConfig != nil {
		return j.pollHttpStatus(ctx)
	}
	return nil
}

// pollGrpcStatus 通过gRPC轮询状态
func (j *RemoteJob) pollGrpcStatus(ctx context.Context, config *domain.GrpcConfig) error {
	if j.grpcClients == nil {
		return fmt.Errorf("grpc clients not initialized")
	}
	client := j.grpcClients.Get(config.ServiceName)
	resp, err := client.Query(ctx, &executorv1.QueryRequest{
		Eid: j.exec.ID,
	})
	if err != nil {
		return err
	}
	return j.handleExecutionState(ctx, resp.ExecutionState)
}

// handleExecutionState 处理执行节点返回的状态
func (j *RemoteJob) handleExecutionState(ctx context.Context, state *executorv1.ExecutionState) error {
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
	if j.isTerminalStatus(domainStatus) {
		// 更新状态
		return j.svc.UpdateStatus(ctx, j.exec.ID, domainStatus)
	}
	return nil
}

// isTerminalStatus 判断是否为终态
func (j *RemoteJob) isTerminalStatus(status domain.TaskExecutionStatus) bool {
	switch status {
	case domain.TaskExecutionStatusSuccess,
		domain.TaskExecutionStatusFailed,
		domain.TaskExecutionStatusFailedPreempted:
		return true
	default:
		return false
	}
}

// pollHttpStatus 通过HTTP轮询状态
func (j *RemoteJob) pollHttpStatus(ctx context.Context) error {
	// TODO: 实现HTTP轮询
	return nil
}

// HandleReport 处理执行节点主动上报的进度信息
func (j *RemoteJob) HandleReport(ctx context.Context, progress *domain.Report) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-j.ctx.Done():
		return fmt.Errorf("job已停止")
	case j.reportCh <- progress:
		return nil
	}
}

func (j *RemoteJob) HandleRenewFailure(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-j.ctx.Done():
		return fmt.Errorf("job已停止")
	default:
		_ = j.svc.UpdateStatus(j.ctx, j.exec.ID, domain.TaskExecutionStatusFailedPreempted)
		return j.Stop(j.ctx)
	}
}

// Stop 停止任务执行
func (j *RemoteJob) Stop(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		j.cancel()
		return nil
	}
}
