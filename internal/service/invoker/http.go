package invoker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"gitee.com/flycash/distributed_task_platform/internal/domain"
	"github.com/gotomicro/ego/core/elog"
)

var _ Invoker = &HTTPInvoker{}

// HTTPInvoker 基于 HTTP 协议的远程任务执行器。
// 通过向执行节点发送 HTTP POST 请求触发任务执行，适用于非 gRPC 的执行节点场景。
// 执行节点需要实现标准的 JSON 响应格式，包含 ExecutionState 信息。
type HTTPInvoker struct {
	logger *elog.Component
	client *http.Client
}

// NewHTTPInvoker 创建 HTTPInvoker 实例。
// 内部创建一个带 30s 超时的 http.Client，防止执行节点无响应时长时间阻塞。
func NewHTTPInvoker() *HTTPInvoker {
	const timeout = 30 * time.Second
	return &HTTPInvoker{
		logger: elog.DefaultLogger.With(elog.FieldComponentName("invoker.HTTP")),
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

func (i *HTTPInvoker) Name() string {
	return "HTTP"
}

// Run 通过 HTTP POST 请求执行远程任务。
// 处理流程：
//  1. 将任务参数序列化为 JSON 请求体
//  2. 构造带 context 的 HTTP 请求（支持超时取消）
//  3. 发送请求到执行节点的 Endpoint 地址
//  4. 解析响应体中的 ExecutionState 并返回
//
// 如果执行节点返回非 2xx 状态码，视为执行失败。
func (i *HTTPInvoker) Run(ctx context.Context, exec domain.TaskExecution) (domain.ExecutionState, error) {
	// 构造请求参数 - 包含任务标识和用户自定义参数
	requestData := map[string]any{
		"taskId":      exec.Task.ID,
		"taskName":    exec.Task.Name,
		"executionId": exec.ID,
		"params":      exec.Task.HTTPConfig.Params,
	}
	jsonBytes, err := json.Marshal(requestData)
	if err != nil {
		return domain.ExecutionState{}, fmt.Errorf("序列化请求参数失败: %w", err)
	}

	// 构造带 context 的请求，确保调度器取消 ctx 时能中止 HTTP 请求
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, exec.Task.HTTPConfig.Endpoint, strings.NewReader(string(jsonBytes)))
	if err != nil {
		return domain.ExecutionState{}, fmt.Errorf("创建HTTP请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := i.client.Do(req)
	if err != nil {
		return domain.ExecutionState{}, fmt.Errorf("发送HTTP请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return domain.ExecutionState{}, fmt.Errorf("读取HTTP响应失败: %w", err)
	}

	// 非 2xx 响应视为执行节点返回错误
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return domain.ExecutionState{}, fmt.Errorf("HTTP执行节点返回异常状态码: %d, body: %s", resp.StatusCode, string(body))
	}

	// 解析响应体中的执行状态
	var state domain.ExecutionState
	if err = json.Unmarshal(body, &state); err != nil {
		i.logger.Error("解析HTTP响应体失败，返回原始响应",
			elog.String("body", string(body)),
			elog.FieldErr(err))
		return domain.ExecutionState{}, fmt.Errorf("解析HTTP响应体失败: %w", err)
	}

	i.logger.Info("HTTP执行完成",
		elog.Int64("executionId", exec.ID),
		elog.String("status", state.Status.String()),
		elog.Int("statusCode", resp.StatusCode))

	return state, nil
}

// Prepare 向执行节点发送 HTTP Prepare 请求，获取分片任务的预处理参数。
// 执行节点需要在 HTTPConfig.PrepareEndpoint 地址实现 Prepare 接口，
// 返回 JSON 格式的 map[string]string 参数。
// 如果任务不需要 Prepare 阶段（PrepareEndpoint 为空），返回空 map。
func (i *HTTPInvoker) Prepare(ctx context.Context, exec domain.TaskExecution) (map[string]string, error) {
	// 如果没有配置 Prepare 端点，说明该任务不需要预处理阶段
	if exec.Task.HTTPConfig == nil || exec.Task.HTTPConfig.Endpoint == "" {
		return map[string]string{}, nil
	}

	requestData := map[string]any{
		"taskId":      exec.Task.ID,
		"taskName":    exec.Task.Name,
		"executionId": exec.ID,
		"params":      exec.Task.HTTPConfig.Params,
	}
	jsonBytes, err := json.Marshal(requestData)
	if err != nil {
		return nil, fmt.Errorf("序列化Prepare请求参数失败: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, exec.Task.HTTPConfig.Endpoint, strings.NewReader(string(jsonBytes)))
	if err != nil {
		return nil, fmt.Errorf("创建Prepare HTTP请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := i.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("发送Prepare HTTP请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取Prepare HTTP响应失败: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Prepare HTTP请求返回异常状态码: %d, body: %s", resp.StatusCode, string(body))
	}

	var params map[string]string
	if err = json.Unmarshal(body, &params); err != nil {
		return nil, fmt.Errorf("解析Prepare HTTP响应体失败: %w", err)
	}

	return params, nil
}
