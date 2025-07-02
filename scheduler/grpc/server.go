package grpc

import (
	"context"
	"fmt"

	executorv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/executor/v1"
	reporterv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/reporter/v1"
	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/scheduler"
	"github.com/ecodeclub/ekit/slice"
	"github.com/gotomicro/ego/core/elog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ReporterServer ReporterService gRPC服务实现
type ReporterServer struct {
	reporterv1.UnimplementedReporterServiceServer
	scheduler *scheduler.Scheduler
	logger    *elog.Component
}

// NewReporterServer 创建ReporterServer实例
func NewReporterServer(
	scheduler *scheduler.Scheduler,
) *ReporterServer {
	return &ReporterServer{
		scheduler: scheduler,
		logger:    elog.DefaultLogger.With(elog.FieldComponentName("scheduler.grpc.ReporterServer")),
	}
}

// Report 单个上报进度
func (s *ReporterServer) Report(ctx context.Context, req *reporterv1.ReportRequest) (*reporterv1.ReportResponse, error) {
	if req.ExecutionState == nil {
		s.logger.Warn("收到空的执行状态上报请求")
		return &reporterv1.ReportResponse{}, nil
	}

	s.logger.Info("收到执行状态上报请求",
		elog.Int64("executionId", req.ExecutionState.Id),
		elog.String("taskName", req.ExecutionState.TaskName),
		elog.String("status", req.ExecutionState.Status.String()),
		elog.String("requestReschedule", fmt.Sprintf("%v", req.RequestReschedule)))

	// 调用业务处理方法
	err := s.handleReports(ctx, s.toDomainReports([]*reporterv1.ReportRequest{req}))
	if err != nil {
		s.logger.Error("处理执行状态上报失败",
			elog.Int64("executionId", req.ExecutionState.Id),
			elog.String("taskName", req.ExecutionState.TaskName),
			elog.FieldErr(err))
		return nil, status.Error(codes.Internal, "处理失败")
	}

	s.logger.Debug("执行状态上报处理成功",
		elog.Int64("executionId", req.ExecutionState.Id))
	return &reporterv1.ReportResponse{}, nil
}

// toDomainReports 将protobuf ExecutionState转换为domain.Report
func (s *ReporterServer) toDomainReports(reqs []*reporterv1.ReportRequest) []*domain.Report {
	return slice.Map(reqs, func(_ int, src *reporterv1.ReportRequest) *domain.Report {
		state := src.GetExecutionState()
		return &domain.Report{
			ExecutionState: domain.ExecutionState{
				ID:              state.GetId(),
				TaskID:          state.GetTaskId(),
				TaskName:        state.GetTaskName(),
				Status:          s.toTaskExecutionStatus(state.GetStatus()),
				RunningProgress: state.GetRunningProgress(),
			},
			RequestReschedule: src.GetRequestReschedule(),
			RescheduleParams:  src.GetRescheduledParams(),
		}
	})
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
		// 记录未知状态的警告
		s.logger.Warn("收到未知执行状态，默认设置为PREPARE",
			elog.String("unknownStatus", status.String()))
		return domain.TaskExecutionStatusPrepare
	}
}

// handleReports 处理报告
func (s *ReporterServer) handleReports(ctx context.Context, reports []*domain.Report) error {
	s.logger.Debug("处理执行状态上报", elog.Int("count", len(reports)))
	return s.scheduler.HandleReports(ctx, reports)
}

// BatchReport 批量上报进度
func (s *ReporterServer) BatchReport(ctx context.Context, req *reporterv1.BatchReportRequest) (*reporterv1.BatchReportResponse, error) {
	if len(req.Reports) == 0 {
		s.logger.Debug("收到空的批量执行状态上报请求")
		return &reporterv1.BatchReportResponse{}, nil
	}

	s.logger.Info("收到批量执行状态上报请求", elog.Int("count", len(req.Reports)))

	err := s.handleReports(ctx, s.toDomainReports(req.GetReports()))
	if err != nil {
		s.logger.Error("处理批量执行状态上报失败",
			elog.Int("count", len(req.Reports)),
			elog.FieldErr(err))
		return nil, status.Error(codes.Internal, "处理失败")
	}

	s.logger.Debug("批量执行状态上报处理成功", elog.Int("count", len(req.Reports)))
	return &reporterv1.BatchReportResponse{}, nil
}
