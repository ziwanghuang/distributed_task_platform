// Package grpc 提供调度平台对外暴露的 gRPC 服务端实现。
//
// 本包实现了 ReporterService 接口（定义在 api/proto/reporter/v1/reporter.proto），
// 供执行节点（Executor）通过 gRPC 上报任务执行状态。
//
// 上报方式：
//   - Report: 单条上报，适用于实时状态反馈
//   - BatchReport: 批量上报，适用于高吞吐场景下的批量状态汇报
//
// 除了 gRPC 直接上报，系统还支持通过 Kafka MQ 异步上报（见 event/reportevt 包），
// 两条链路最终都汇入 ExecutionService.HandleReports 进行统一处理。
package grpc

import (
	"context"
	"fmt"

	"gitee.com/flycash/distributed_task_platform/internal/service/task"

	reporterv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/reporter/v1"
	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"github.com/ecodeclub/ekit/slice"
	"github.com/gotomicro/ego/core/elog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ReporterServer 是 ReporterService gRPC 服务端的实现。
//
// 它嵌入了 UnimplementedReporterServiceServer 以满足 gRPC 接口的前向兼容要求，
// 并通过 execSvc（ExecutionService）将收到的执行状态上报委托给业务层处理。
//
// 处理流程：
//  1. 接收 protobuf 格式的 ReportRequest / BatchReportRequest
//  2. 转换为 domain.Report 领域模型
//  3. 调用 execSvc.HandleReports 进行状态机转换、持久化、事件触发等业务逻辑
type ReporterServer struct {
	reporterv1.UnimplementedReporterServiceServer          // gRPC 前向兼容嵌入
	execSvc task.ExecutionService                          // 执行记录服务，处理上报的状态更新
	logger  *elog.Component                                // 日志组件
}

// NewReporterServer 创建 ReporterServer 实例
func NewReporterServer(
	execSvc task.ExecutionService,
) *ReporterServer {
	return &ReporterServer{
		execSvc: execSvc,
		logger:  elog.DefaultLogger.With(elog.FieldComponentName("scheduler.grpc.ReporterServer")),
	}
}

// Report 处理单条执行状态上报请求。
// 执行节点在任务状态发生变化时（如开始运行、进度更新、完成、失败等）调用此接口。
// 参数 req.ExecutionState 包含执行记录 ID、任务名、新状态、进度等信息。
// 返回空响应表示处理成功，返回 codes.Internal 表示处理失败。
func (s *ReporterServer) Report(ctx context.Context, req *reporterv1.ReportRequest) (*reporterv1.ReportResponse, error) {
	state := req.ExecutionState
	if state == nil {
		s.logger.Warn("收到空的执行状态上报请求")
		return &reporterv1.ReportResponse{}, nil
	}

	s.logger.Info("收到执行状态上报请求",
		elog.Int64("executionId", state.Id),
		elog.String("taskName", state.TaskName),
		elog.String("status", state.Status.String()),
		elog.String("requestReschedule", fmt.Sprintf("%v", state.RequestReschedule)))

	// 调用业务处理方法
	err := s.handleReports(ctx, s.toDomainReports([]*reporterv1.ReportRequest{req}))
	if err != nil {
		s.logger.Error("处理执行状态上报失败",
			elog.Int64("executionId", state.Id),
			elog.String("taskName", state.TaskName),
			elog.FieldErr(err))
		return nil, status.Error(codes.Internal, "处理失败")
	}

	s.logger.Debug("执行状态上报处理成功",
		elog.Int64("executionId", state.Id))
	return &reporterv1.ReportResponse{}, nil
}

// toDomainReports 将 protobuf 格式的 ReportRequest 切片转换为 domain.Report 领域模型切片。
// 使用 ekit/slice.Map 进行类型映射，内部调用 domain.ExecutionStateFromProto 完成
// protobuf ExecutionState → domain.ExecutionState 的转换。
func (s *ReporterServer) toDomainReports(reqs []*reporterv1.ReportRequest) []*domain.Report {
	return slice.Map(reqs, func(_ int, src *reporterv1.ReportRequest) *domain.Report {
		return &domain.Report{
			ExecutionState: domain.ExecutionStateFromProto(src.GetExecutionState()),
		}
	})
}

// handleReports 处理报告
func (s *ReporterServer) handleReports(ctx context.Context, reports []*domain.Report) error {
	s.logger.Debug("处理执行状态上报", elog.Int("count", len(reports)))
	return s.execSvc.HandleReports(ctx, reports)
}

// BatchReport 处理批量执行状态上报请求。
// 适用于执行节点累积多条状态变更后一次性上报的场景，减少网络开销。
// 内部复用 handleReports 方法，与 Report 共享相同的业务处理逻辑。
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
