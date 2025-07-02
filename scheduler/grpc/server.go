package grpc

import (
	"context"
	"fmt"

	executorv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/executor/v1"
	reporterv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/reporter/v1"
	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/service/task"
	"gitee.com/flycash/distributed_task_platform/scheduler"
	"github.com/gotomicro/ego/core/elog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ReporterServer ReporterService gRPC服务实现
type ReporterServer struct {
	reporterv1.UnimplementedReporterServiceServer
	taskSvc   task.Service
	execSvc   task.ExecutionService
	scheduler *scheduler.Scheduler
	logger    *elog.Component
}

// NewReporterServer 创建ReporterServer实例
func NewReporterServer(
	taskSvc task.Service,
	execSvc task.ExecutionService,
	scheduler *scheduler.Scheduler,
) *ReporterServer {
	return &ReporterServer{
		taskSvc:   taskSvc,
		execSvc:   execSvc,
		scheduler: scheduler,
		logger:    elog.DefaultLogger.With(elog.FieldComponentName("scheduler.grpc.ReporterServer")),
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
	reportData, err := s.toDomainReport(req)
	if err != nil {
		s.logger.Error("转换执行状态失败", elog.FieldErr(err))
		return nil, status.Error(codes.Internal, "转换执行状态失败")
	}

	// 调用业务处理方法
	err = s.report(ctx, reportData)
	if err != nil {
		s.logger.Error("处理执行状态上报失败", elog.FieldErr(err))
		return nil, status.Error(codes.Internal, "处理失败")
	}

	return &reporterv1.ReportResponse{}, nil
}

// toDomainReport 将protobuf ExecutionState转换为domain.Report
func (s *ReporterServer) toDomainReport(req *reporterv1.ReportRequest) (domain.Report, error) {
	state := req.GetExecutionState()
	return domain.Report{
		ExecutionID:       state.Id,
		TaskName:          state.TaskName,
		Status:            s.toTaskExecutionStatus(state.Status),
		RunningProgress:   state.RunningProgress,
		RequestReschedule: req.GetRequestReschedule(),
		RescheduleParams:  req.GetRescheduledParams(),
	}, nil
}

// report 单次报告处理
func (s *ReporterServer) report(ctx context.Context, report domain.Report) error {
	// TODO: 实现具体业务逻辑

	// 通过ExecutionID查询获取TaskID
	execution, err := s.execSvc.GetByID(ctx, report.ExecutionID)
	if err != nil {
		return err
	}

	// 要求重新调度，优先执行重调度。
	if report.RequestReschedule {
		tk, err1 := s.taskSvc.GetByID(ctx, execution.Task.ID)
		if err1 != nil {
			s.logger.Error("查找Task失败", elog.FieldErr(err1))
			return err1
		}
		// 更新调度参数
		err1 = s.taskSvc.UpdateScheduleParams(ctx, tk.ID, tk.Version, report.RescheduleParams)
		if err1 != nil {
			s.logger.Error("更新调度信息失败", elog.FieldErr(err1))
			return err1
		}
		return s.scheduler.Reschedule(tk)
	}

	// 1. 更新ExecutionService中的状态
	// 2. 如果requestReschedule=true，触发重调度逻辑
	// 3. 根据Status做相应处理（成功、失败、重试等）

	s.logger.Info("处理执行状态上报",
		elog.Any("report", report))

	return nil
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
		reportData, err := s.toDomainReport(report)
		if err != nil {
			s.logger.Error("转换执行状态失败",
				elog.Int("index", i),
				elog.FieldErr(err))
			continue
		}

		// 调用业务处理方法
		err = s.report(ctx, reportData)
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
