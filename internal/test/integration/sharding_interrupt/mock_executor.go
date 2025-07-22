package integration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	executorv1 "gitee.com/flycash/distributed_task_platform/api/proto/gen/executor/v1"
	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"gitee.com/flycash/distributed_task_platform/internal/event/reportevt"
	"github.com/gotomicro/ego/core/elog"
)

const (
	filePermissions   = 0o755
	fileCreateMode    = 0o644
	executionInterval = 3 * time.Second
	interruptTimeout  = 10 * time.Second
	progressComplete  = 100
)

// MockExecutorService 模拟执行节点服务
type MockExecutorService struct {
	executorv1.UnimplementedExecutorServiceServer

	nodeID, testDataDir string
	logger              *elog.Component

	// 执行状态管理
	mutex       sync.RWMutex
	executions  map[int64]*executorv1.ExecutionState      // executionID -> 执行状态
	interruptCh map[int64]chan struct{}                   // executionID -> 中断信号
	resultCh    map[int64]chan *executorv1.ExecutionState // executionID -> 中断结果状态

	// 状态上报
	reportProducer reportevt.ReportEventProducer
}

// NewMockExecutorService 创建模拟执行节点服务
func NewMockExecutorService(nodeID, testDataDir string, reportProducer reportevt.ReportEventProducer) *MockExecutorService {
	return &MockExecutorService{
		nodeID:         nodeID,
		testDataDir:    testDataDir,
		logger:         elog.DefaultLogger.With(elog.FieldComponentName("mock-executor-" + nodeID)),
		executions:     make(map[int64]*executorv1.ExecutionState),
		interruptCh:    make(map[int64]chan struct{}),
		resultCh:       make(map[int64]chan *executorv1.ExecutionState),
		reportProducer: reportProducer,
	}
}

// Execute 执行任务
func (m *MockExecutorService) Execute(ctx context.Context, req *executorv1.ExecuteRequest) (*executorv1.ExecuteResponse, error) {
	m.logger.Info("开始执行任务",
		elog.Int64("eid", req.Eid),
		elog.Int64("taskId", req.TaskId),
		elog.String("taskName", req.TaskName),
		elog.Any("allParams", req.Params))

	// 解析和验证参数
	execParams, err := m.parseExecutionParams(req)
	if err != nil {
		return nil, err
	}

	// 初始化执行环境
	channels := m.initializeExecution(ctx, req, execParams)
	defer m.cleanupExecution(req.Eid)

	// 准备文件写入
	file, err := m.prepareOutputFile(req.Eid)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// 执行数据处理循环
	return m.processDataLoop(ctx, req, execParams, channels, file)
}

// ExecutionParams 执行参数
type ExecutionParams struct {
	Start, End, ActualStart, OriginalStart int
	Total                                  int
	IsReschedule                           bool
}

// ExecutionChannels 执行通道
type ExecutionChannels struct {
	InterruptCh chan struct{}
	ResultCh    chan *executorv1.ExecutionState
}

// parseExecutionParams 解析执行参数
func (m *MockExecutorService) parseExecutionParams(req *executorv1.ExecuteRequest) (*ExecutionParams, error) {
	// 检查是否包含重调度参数
	if progressStr, exists := req.Params["progress"]; exists && progressStr != "" {
		m.logger.Info("重调度参数:",
			elog.Int64("eid", req.Eid),
			elog.String("start", req.Params["start"]),
			elog.String("end", req.Params["end"]),
			elog.String("progress", progressStr))
	}

	start, err := strconv.Atoi(req.Params["start"])
	if err != nil {
		return nil, fmt.Errorf("无效的start参数: %w", err)
	}

	end, err := strconv.Atoi(req.Params["end"])
	if err != nil {
		return nil, fmt.Errorf("无效的end参数: %w", err)
	}

	originalStart := start
	actualStart := start
	isReschedule := false

	if progressStr, exists := req.Params["progress"]; exists && progressStr != "" {
		if progress, err := strconv.Atoi(progressStr); err == nil {
			actualStart = progress + 1
			isReschedule = true
			m.logger.Info("从重调度进度继续执行",
				elog.Int64("eid", req.Eid),
				elog.Int("分片start", start),
				elog.Int("分片end", end),
				elog.Int("中断进度", progress),
				elog.Int("继续位置", actualStart))
		} else {
			m.logger.Error("重调度进度参数解析失败",
				elog.Int64("eid", req.Eid),
				elog.String("progressStr", progressStr),
				elog.FieldErr(err))
		}
	}

	total := end - start
	m.logger.Info("执行参数确定",
		elog.Int64("eid", req.Eid),
		elog.Int("start", start),
		elog.Int("end", end),
		elog.Int("actualStart", actualStart),
		elog.Int("total", total))

	if total <= 0 || actualStart >= end {
		return nil, fmt.Errorf("没有工作要做: total=%d, actualStart=%d, end=%d", total, actualStart, end)
	}

	return &ExecutionParams{
		Start:         start,
		End:           end,
		ActualStart:   actualStart,
		OriginalStart: originalStart,
		Total:         total,
		IsReschedule:  isReschedule,
	}, nil
}

// initializeExecution 初始化执行环境
func (m *MockExecutorService) initializeExecution(ctx context.Context, req *executorv1.ExecuteRequest, params *ExecutionParams) *ExecutionChannels {
	// 创建通道
	m.mutex.Lock()
	interruptCh := make(chan struct{})
	resultCh := make(chan *executorv1.ExecutionState)
	m.interruptCh[req.Eid] = interruptCh
	m.resultCh[req.Eid] = resultCh

	// 初始化执行状态
	var initialProgress int32
	if params.Total > 0 {
		initialProgress = int32((params.ActualStart - params.Start) * progressComplete / params.Total)
	}

	state := &executorv1.ExecutionState{
		Id:                req.Eid,
		TaskId:            req.TaskId,
		TaskName:          req.TaskName,
		Status:            executorv1.ExecutionStatus_RUNNING,
		RunningProgress:   initialProgress,
		ExecutorNodeId:    m.nodeID,
		RescheduledParams: make(map[string]string),
	}
	m.executions[req.Eid] = state
	m.mutex.Unlock()

	// 上报开始执行状态
	m.reportExecutionState(ctx, req.Eid, state)

	return &ExecutionChannels{
		InterruptCh: interruptCh,
		ResultCh:    resultCh,
	}
}

// prepareOutputFile 准备输出文件
func (m *MockExecutorService) prepareOutputFile(executionID int64) (*os.File, error) {
	filename := fmt.Sprintf("executor-%s-%d.txt", m.nodeID, executionID)
	filePath := filepath.Join(m.testDataDir, filename)

	if err := os.MkdirAll(m.testDataDir, filePermissions); err != nil {
		return nil, fmt.Errorf("创建目录失败: %w", err)
	}

	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, fileCreateMode)
	if err != nil {
		return nil, fmt.Errorf("打开文件失败: %w", err)
	}

	return file, nil
}

// cleanupExecution 清理执行资源
func (m *MockExecutorService) cleanupExecution(executionID int64) {
	m.mutex.Lock()
	delete(m.interruptCh, executionID)
	delete(m.resultCh, executionID)
	m.mutex.Unlock()
}

// processDataLoop 执行数据处理循环
func (m *MockExecutorService) processDataLoop(ctx context.Context, req *executorv1.ExecuteRequest, params *ExecutionParams, channels *ExecutionChannels, file *os.File) (*executorv1.ExecuteResponse, error) {
	// 数据处理循环
	for i := params.ActualStart; i < params.End; i++ {
		// 先写入数据 - 数据编号基于原始分片起始位置
		dataNum := i - params.OriginalStart + 1
		data := fmt.Sprintf("shard-%d-data-%d\n", getShardID(params.OriginalStart), dataNum)
		if _, err := file.WriteString(data); err != nil {
			return nil, fmt.Errorf("写入文件失败: %w", err)
		}

		// 计算当前进度
		progress := int32((i - params.Start + 1) * progressComplete / params.Total)

		// 更新执行状态
		m.mutex.Lock()
		if execState, exists := m.executions[req.Eid]; exists {
			execState.RunningProgress = progress
		}
		m.mutex.Unlock()

		m.logger.Info("处理数据",
			elog.Int64("eid", req.Eid),
			elog.Int("index", i),
			elog.Int("originalStart", params.OriginalStart),
			elog.Int("dataNum", dataNum),
			elog.Int32("progress", progress))

		// 上报进度（每10%上报一次）
		if progress%20 == 0 || progress >= 50 {
			m.reportExecutionState(ctx, req.Eid, &executorv1.ExecutionState{
				Id:              req.Eid,
				TaskId:          req.TaskId,
				TaskName:        req.TaskName,
				Status:          executorv1.ExecutionStatus_RUNNING,
				RunningProgress: progress,
				ExecutorNodeId:  m.nodeID,
			})
		}

		// 检查中断信号
		select {
		case <-channels.InterruptCh:
			return m.handleInterruption(ctx, req, params, channels, i, progress)
		case <-time.After(executionInterval):
			// 正常执行间隔，继续下一轮
			continue
		}
	}

	// 任务正常完成
	return m.handleCompletion(ctx, req, channels)
}

// handleInterruption 处理任务中断
func (m *MockExecutorService) handleInterruption(ctx context.Context, req *executorv1.ExecuteRequest, params *ExecutionParams, channels *ExecutionChannels, currentIndex int, progress int32) (*executorv1.ExecuteResponse, error) {
	m.logger.Info("收到中断信号",
		elog.Int64("eid", req.Eid),
		elog.Int("currentIndex", currentIndex),
		elog.Int("completedDataNum", currentIndex-params.OriginalStart+1),
		elog.Int32("progress", progress))

	// 检查是否已经是最后一个元素（即将完成）
	if currentIndex+1 >= params.End {
		m.logger.Info("任务即将完成，直接标记为成功",
			elog.Int64("eid", req.Eid),
			elog.Int("currentIndex", currentIndex),
			elog.Int("end", params.End))

		return m.handleCompletion(ctx, req, channels)
	}

	// 计算重调度参数 - 保持分片信息不变
	rescheduleParams := map[string]string{
		"start":    fmt.Sprintf("%d", params.OriginalStart), // 保持原始分片start不变
		"end":      fmt.Sprintf("%d", params.End),           // 保持分片end不变
		"progress": fmt.Sprintf("%d", currentIndex),         // 中断时的进度
	}

	// 返回中断状态
	finalState := &executorv1.ExecutionState{
		Id:                req.Eid,
		TaskId:            req.TaskId,
		TaskName:          req.TaskName,
		Status:            executorv1.ExecutionStatus_FAILED_RESCHEDULABLE,
		RunningProgress:   progress,
		RequestReschedule: true,
		RescheduledParams: rescheduleParams,
		ExecutorNodeId:    m.nodeID,
	}

	m.reportExecutionState(ctx, req.Eid, finalState)

	// 发送最终状态到resultCh
	select {
	case channels.ResultCh <- finalState:
	default:
	}

	return &executorv1.ExecuteResponse{
		ExecutionState: finalState,
	}, nil
}

// handleCompletion 处理任务完成
func (m *MockExecutorService) handleCompletion(ctx context.Context, req *executorv1.ExecuteRequest, channels *ExecutionChannels) (*executorv1.ExecuteResponse, error) {
	m.logger.Info("任务执行完成", elog.Int64("eid", req.Eid))

	m.mutex.Lock()
	if execState, exists := m.executions[req.Eid]; exists {
		execState.Status = executorv1.ExecutionStatus_SUCCESS
		execState.RunningProgress = progressComplete
	}
	finalState := m.executions[req.Eid]
	m.mutex.Unlock()

	// 上报完成状态
	m.reportExecutionState(ctx, req.Eid, finalState)

	// 发送最终状态到resultCh（正常完成的情况）
	select {
	case channels.ResultCh <- finalState:
	default:
	}

	return &executorv1.ExecuteResponse{
		ExecutionState: finalState,
	}, nil
}

// Interrupt 中断任务执行
func (m *MockExecutorService) Interrupt(ctx context.Context, req *executorv1.InterruptRequest) (*executorv1.InterruptResponse, error) {
	m.logger.Info("收到中断请求", elog.Int64("eid", req.Eid))

	m.mutex.Lock()

	// 检查该execution是否在当前节点执行
	if _, exists := m.executions[req.Eid]; !exists {
		m.mutex.Unlock()
		m.logger.Info("该执行记录不在当前节点，忽略中断请求",
			elog.Int64("eid", req.Eid),
			elog.String("nodeID", m.nodeID))
		return &executorv1.InterruptResponse{
			Success: false,
		}, nil
	}

	// 检查是否有中断通道和结果通道
	interruptCh, hasInterruptCh := m.interruptCh[req.Eid]
	resultCh, hasResultCh := m.resultCh[req.Eid]
	m.mutex.Unlock()

	if hasInterruptCh && hasResultCh {
		m.logger.Info("发送中断信号到执行任务", elog.Int64("eid", req.Eid))

		// 发送中断信号
		select {
		case interruptCh <- struct{}{}:
			m.logger.Info("中断信号发送成功", elog.Int64("eid", req.Eid))
		case <-ctx.Done():
			return &executorv1.InterruptResponse{Success: false}, ctx.Err()
		default:
			m.logger.Info("中断信号通道已满，可能已被中断", elog.Int64("eid", req.Eid))
		}

		// 等待Execute方法处理完中断并返回最终状态
		m.logger.Info("等待执行任务处理中断...", elog.Int64("eid", req.Eid))
		select {
		case finalState := <-resultCh:
			m.logger.Info("收到执行任务的最终状态",
				elog.Int64("eid", req.Eid),
				elog.String("status", finalState.Status.String()),
				elog.Int32("progress", finalState.RunningProgress))
			return &executorv1.InterruptResponse{
				Success:        true,
				ExecutionState: finalState,
			}, nil
		case <-ctx.Done():
			return &executorv1.InterruptResponse{Success: false}, ctx.Err()
		case <-time.After(interruptTimeout): // 超时保护
			m.logger.Warn("等待中断结果超时", elog.Int64("eid", req.Eid))
			return &executorv1.InterruptResponse{Success: false}, nil
		}
	}

	m.logger.Warn("未找到对应的中断通道或结果通道", elog.Int64("eid", req.Eid))
	return &executorv1.InterruptResponse{
		Success: false,
	}, nil
}

// Query 查询任务状态
func (m *MockExecutorService) Query(_ context.Context, req *executorv1.QueryRequest) (*executorv1.QueryResponse, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	state, exists := m.executions[req.Eid]
	if !exists {
		return &executorv1.QueryResponse{
			ExecutionState: &executorv1.ExecutionState{
				Id:     req.Eid,
				Status: executorv1.ExecutionStatus_UNKNOWN,
			},
		}, nil
	}

	return &executorv1.QueryResponse{
		ExecutionState: state,
	}, nil
}

// reportExecutionState 上报执行状态到消息队列
func (m *MockExecutorService) reportExecutionState(ctx context.Context, executionID int64, state *executorv1.ExecutionState) {
	domainState := domain.ExecutionStateFromProto(state)
	report := domain.Report{
		ExecutionState: domainState,
	}

	if err := m.reportProducer.Produce(ctx, report); err != nil {
		m.logger.Error("上报执行状态失败",
			elog.Int64("eid", executionID),
			elog.FieldErr(err))
	} else {
		m.logger.Info("上报执行状态成功",
			elog.Int64("eid", executionID),
			elog.String("status", state.Status.String()),
			elog.Int32("progress", state.RunningProgress))
	}
}

// getShardID 根据start值计算分片ID
func getShardID(start int) int {
	// 假设每个分片步长为10，分片ID从1开始
	return start/10 + 1
}
