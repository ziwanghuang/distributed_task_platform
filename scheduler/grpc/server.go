package grpc

import (
	"context"
	"fmt"

	executorv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/executor/v1"
	reporterv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/reporter/v1"
	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"github.com/gotomicro/ego/core/elog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ReporterServer ReporterService gRPC服务实现
type ReporterServer struct {
	reporterv1.UnimplementedReporterServiceServer
	execSvc task.ExecutionService
	logger  *elog.Component
}

// NewReporterServer 创建ReporterServer实例
func NewReporterServer(execSvc task.ExecutionService) *ReporterServer {
	return &ReporterServer{
		execSvc: execSvc,
		logger:  elog.DefaultLogger.With(elog.FieldComponentName("scheduler.grpc.ReporterServer")),
	}
}

// Report 单个上报进度
func (s *ReporterServer) Report(ctx context.Context, req *reporterv1.ReportRequest) (*reporterv1.ReportResponse, error) {
	// 参数验证
	if req.ExecutionState == nil {
		return nil, status.Error(codes.InvalidArgument, "execution_state 不能为空")
	}

	s.logger.Info("收到执行状态上报请求",
		elog.Int64("executionId", req.ExecutionState.Id),
		elog.String("taskName", req.ExecutionState.TaskName),
		elog.String("requestReschedule", fmt.Sprintf("%v", req.RequestReschedule)))

	// 转换为Report对象
	reportData, err := s.toDomainReport(ctx, req.ExecutionState)
	if err != nil {
		s.logger.Error("转换执行状态失败", elog.FieldErr(err))
		return nil, status.Error(codes.Internal, "转换执行状态失败")
	}

	// 调用业务处理方法
	err = s.report(ctx, reportData, req.RequestReschedule, req.RescheduledParams)
	if err != nil {
		s.logger.Error("处理执行状态上报失败", elog.FieldErr(err))
		return nil, status.Error(codes.Internal, "处理失败")
	}

	return &reporterv1.ReportResponse{}, nil
}

// toDomainReport 将protobuf ExecutionState转换为domain.Report
func (s *ReporterServer) toDomainReport(ctx context.Context, state *executorv1.ExecutionState) (domain.Report, error) {
	// 通过ExecutionID查询获取TaskID
	execution, err := s.execSvc.GetByID(ctx, state.Id)
	if err != nil {
		return domain.Report{}, err
	}

	return domain.Report{
		TaskID:      execution.TaskID,
		TaskName:    state.TaskName,
		ExecutionID: state.Id,
		Status:      s.toTaskExecutionStatus(state.Status),
		Progress:    state.RunningProgress,
	}, nil
}

// report 单次报告处理
func (s *ReporterServer) report(ctx context.Context, reportData domain.Report, requestReschedule bool, params map[string]string) error {
	// TODO: 实现具体业务逻辑
	// 1. 更新ExecutionService中的状态
	// 2. 如果requestReschedule=true，触发重调度逻辑
	// 3. 根据Status做相应处理（成功、失败、重试等）

	s.logger.Info("处理执行状态上报",
		elog.Int64("taskId", reportData.TaskID),
		elog.Int64("executionId", reportData.ExecutionID),
		elog.String("taskName", reportData.TaskName),
		elog.String("status", reportData.Status.String()),
		elog.Int32("progress", reportData.Progress),
		elog.String("requestReschedule", fmt.Sprintf("%v", requestReschedule)))

	return nil // 暂时返回成功
}

// toTaskExecutionStatus 将protobuf ExecutionStatus转换为domain.TaskExecutionStatus
func (s *ReporterServer) toTaskExecutionStatus(status executorv1.ExecutionStatus) domain.TaskExecutionStatus {
	switch status {
	case executorv1.ExecutionStatus_RUNNING:
		return domain.TaskExecutionStatusRunning
	case executorv1.ExecutionStatus_SUCCESS:
		return domain.TaskExecutionStatusSuccess
	case executorv1.ExecutionStatus_FAILED:
		return domain.TaskExecutionStatusFailed
	case executorv1.ExecutionStatus_FAILED_RETRYABLE:
		return domain.TaskExecutionStatusFailedRetryable
	default:
		// 未知状态默认为PREPARE
		return domain.TaskExecutionStatusPrepare
	}
}

// BatchReport 批量上报进度
func (s *ReporterServer) BatchReport(ctx context.Context, req *reporterv1.BatchReportRequest) (*reporterv1.BatchReportResponse, error) {
	if len(req.Reports) == 0 {
		return &reporterv1.BatchReportResponse{}, nil
	}

	s.logger.Info("收到批量执行状态上报请求", elog.Int("count", len(req.Reports)))

	// 批量处理每个上报
	for i, report := range req.Reports {
		if report.ExecutionState == nil {
			s.logger.Warn("跳过无效的执行状态", elog.Int("index", i))
			continue
		}

		// 转换为Report对象
		reportData, err := s.toDomainReport(ctx, report.ExecutionState)
		if err != nil {
			s.logger.Error("转换执行状态失败",
				elog.Int("index", i),
				elog.FieldErr(err))
			continue
		}

		// 调用业务处理方法
		err = s.report(ctx, reportData, report.RequestReschedule, report.RescheduledParams)
		if err != nil {
			s.logger.Error("处理执行状态上报失败",
				elog.Int("index", i),
				elog.Int64("executionId", reportData.ExecutionID),
				elog.FieldErr(err))
			continue
		}
	}

	return &reporterv1.BatchReportResponse{}, nil
}
