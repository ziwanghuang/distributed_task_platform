package picker

import (
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/gotomicro/ego/core/elog"
	"github.com/prometheus/prometheus/prompb"
)

// MetricType 指标类型
type MetricType string

const (
	CPUMetricType    MetricType = "cpu"
	MemoryMetricType MetricType = "memory"
)

// MockExecutorNode 模拟一个执行节点，它能通过Remote Write API向Prometheus推送指标。
type MockExecutorNode struct {
	NodeID     string
	MetricType MetricType
	logger     *elog.Component

	// 指标数据
	mutex                sync.RWMutex
	cpuIdlePercent       float64 // CPU空闲百分比 (0-100)
	availableMemoryBytes float64 // 可用内存字节数
	isAlive              bool    // 节点是否存活

	// 推送控制
	stopChan chan struct{}
	client   *http.Client
}

// NewMockExecutorNode 创建模拟执行节点
func NewMockExecutorNode(nodeID string, metricType MetricType) *MockExecutorNode {
	node := &MockExecutorNode{
		NodeID:     nodeID,
		MetricType: metricType,
		logger:     elog.DefaultLogger.With(elog.FieldComponentName("mock-executor-" + nodeID)),
		isAlive:    true,
		stopChan:   make(chan struct{}),
		client:     &http.Client{Timeout: 5 * time.Second},
	}

	// 设置初始指标值
	node.initializeMetrics()

	return node
}

// initializeMetrics 初始化指标数据
func (n *MockExecutorNode) initializeMetrics() {
	n.mutex.Lock()
	defer n.mutex.Unlock()

	switch n.MetricType {
	case CPUMetricType:
		// CPU节点设置随机的CPU空闲率
		n.cpuIdlePercent = 50 + rand.Float64()*40 // 50-90%
		n.availableMemoryBytes = 0                // CPU节点不上报内存指标
	case MemoryMetricType:
		// 内存节点设置随机的可用内存
		n.availableMemoryBytes = float64(2+rand.Intn(6)) * 1024 * 1024 * 1024 // 2-8GB
		n.cpuIdlePercent = 0                                                  // 内存节点不上报CPU指标
	}

	n.logger.Info("初始化指标数据",
		elog.String("nodeID", n.NodeID),
		elog.String("metricType", string(n.MetricType)),
		elog.Any("cpuIdlePercent", n.cpuIdlePercent),
		elog.Any("availableMemoryBytes", n.availableMemoryBytes))
}

// StartMetricsReporting 开始指标推送
func (n *MockExecutorNode) StartMetricsReporting() error {
	// 启动定期推送goroutine
	go n.metricsReportingLoop()

	n.logger.Info("开始指标推送", elog.String("nodeID", n.NodeID))
	return nil
}

// Stop 停止节点（模拟节点下线）
func (n *MockExecutorNode) Stop() {
	n.mutex.Lock()
	n.isAlive = false
	n.mutex.Unlock()

	// 发送停止信号
	select {
	case n.stopChan <- struct{}{}:
	default:
	}

	n.logger.Info("节点已停止", elog.String("nodeID", n.NodeID))
}

// metricsReportingLoop 指标推送循环
func (n *MockExecutorNode) metricsReportingLoop() {
	ticker := time.NewTicker(5 * time.Second) // 每5秒推送一次
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := n.pushMetrics(); err != nil {
				n.logger.Error("推送指标失败", elog.FieldErr(err))
			}
		case <-n.stopChan:
			n.logger.Info("停止指标推送", elog.String("nodeID", n.NodeID))
			return
		}
	}
}

// pushMetrics 推送指标到Prometheus
func (n *MockExecutorNode) pushMetrics() error {
	n.mutex.RLock()
	isAlive := n.isAlive
	cpuIdlePercent := n.cpuIdlePercent
	availableMemoryBytes := n.availableMemoryBytes
	n.mutex.RUnlock()

	if !isAlive {
		// 节点下线，不推送指标
		return nil
	}

	now := time.Now()
	timestampMs := now.UnixNano() / int64(time.Millisecond)

	var timeSeries []prompb.TimeSeries

	// 添加up指标（节点存活状态）
	timeSeries = append(timeSeries, prompb.TimeSeries{
		Labels: []prompb.Label{
			{Name: "__name__", Value: "up"},
			{Name: "job", Value: "executors"},
			{Name: "instance", Value: n.NodeID},
			{Name: "node_id", Value: n.NodeID},
		},
		Samples: []prompb.Sample{
			{Value: 1, Timestamp: timestampMs}, // 1表示存活
		},
	})

	// 根据节点类型添加相应指标
	switch n.MetricType {
	case CPUMetricType:
		if cpuIdlePercent > 0 {
			timeSeries = append(timeSeries, prompb.TimeSeries{
				Labels: []prompb.Label{
					{Name: "__name__", Value: "executor_cpu_idle_percent"},
					{Name: "job", Value: "executors"},
					{Name: "instance", Value: n.NodeID},
					{Name: "node_id", Value: n.NodeID},
				},
				Samples: []prompb.Sample{
					{Value: cpuIdlePercent, Timestamp: timestampMs},
				},
			})
		}
	case MemoryMetricType:
		if availableMemoryBytes > 0 {
			timeSeries = append(timeSeries, prompb.TimeSeries{
				Labels: []prompb.Label{
					{Name: "__name__", Value: "executor_memory_available_bytes"},
					{Name: "job", Value: "executors"},
					{Name: "instance", Value: n.NodeID},
					{Name: "node_id", Value: n.NodeID},
				},
				Samples: []prompb.Sample{
					{Value: availableMemoryBytes, Timestamp: timestampMs},
				},
			})
		}
	}

	// 创建WriteRequest
	writeRequest := &prompb.WriteRequest{
		Timeseries: timeSeries,
	}

	// 序列化为protobuf
	data, err := proto.Marshal(writeRequest)
	if err != nil {
		return fmt.Errorf("序列化protobuf失败: %w", err)
	}

	// 使用snappy压缩
	compressed := snappy.Encode(nil, data)

	// 发送到Prometheus Remote Write API
	req, err := http.NewRequest("POST", "http://localhost:9090/api/v1/write", bytes.NewReader(compressed))
	if err != nil {
		return fmt.Errorf("创建HTTP请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("Content-Encoding", "snappy")
	req.Header.Set("X-Prometheus-Remote-Write-Version", "0.1.0")

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("发送HTTP请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Prometheus返回错误: %d, %s", resp.StatusCode, string(body))
	}

	n.logger.Debug("成功推送指标",
		elog.String("nodeID", n.NodeID),
		elog.String("metricType", string(n.MetricType)),
		elog.Int("timeSeriesCount", len(timeSeries)))

	return nil
}

// SetCPUIdlePercent 设置CPU空闲百分比（用于测试）
func (n *MockExecutorNode) SetCPUIdlePercent(percent float64) {
	n.mutex.Lock()
	defer n.mutex.Unlock()

	if n.MetricType == CPUMetricType {
		n.cpuIdlePercent = percent
		n.logger.Info("设置CPU空闲百分比",
			elog.String("nodeID", n.NodeID),
			elog.Any("cpuIdlePercent", percent))
	}
}

// SetAvailableMemoryBytes 设置可用内存字节数（用于测试）
func (n *MockExecutorNode) SetAvailableMemoryBytes(bytes float64) {
	n.mutex.Lock()
	defer n.mutex.Unlock()

	if n.MetricType == MemoryMetricType {
		n.availableMemoryBytes = bytes
		n.logger.Info("设置可用内存",
			elog.String("nodeID", n.NodeID),
			elog.Any("availableMemoryBytes", bytes))
	}
}

// GetCurrentMetrics 获取当前指标（用于测试验证）
func (n *MockExecutorNode) GetCurrentMetrics() (cpuIdlePercent, availableMemoryBytes float64, isAlive bool) {
	n.mutex.RLock()
	defer n.mutex.RUnlock()

	return n.cpuIdlePercent, n.availableMemoryBytes, n.isAlive
}
