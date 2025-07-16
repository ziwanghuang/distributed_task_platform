package prometheus

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"gorm.io/gorm"
)

const (
	// callbackStartTimeKey 是存储在gorm.DB实例中的回调开始时间的键
	callbackStartTimeKey = "prometheus:start_time"
	// callbackOperationKey 是存储在gorm.DB实例中的数据库操作类型的键
	callbackOperationKey = "prometheus:operation"
)

// GormMetricsPlugin 是一个为GORM实现Prometheus指标监控的插件。
// 它会自动记录所有CRUD操作的执行耗时，并按操作类型和表名进行分类。
type GormMetricsPlugin struct {
	// name 是插件的名称，必须实现 gorm.Plugin 接口。
	name string
	// queryDuration 是一个Prometheus直方图向量，用于存储查询耗时。
	// 使用Histogram可以同时获得总次数、总耗时以及响应时间的分位数（如p99, p95）。
	// Labels:
	// - "operation": 数据库操作类型 (e.g., "query", "create", "update", "delete").
	// - "table": 操作对应的数据库表名.
	queryDuration *prometheus.HistogramVec
}

// NewGormMetricsPlugin 创建并初始化一个新的GORM指标插件。
func NewGormMetricsPlugin() *GormMetricsPlugin {
	return &GormMetricsPlugin{
		name: "prometheus_gorm_metrics",
		queryDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				// 使用默认的 buckets，适用于大多数Web服务的响应时间分布。
				// 可以根据实际情况自定义buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5}
				Name:    "gorm_query_duration_seconds",
				Help:    "GORM 数据库操作耗时（秒）",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"operation", "table"},
		),
	}
}

// Name 返回插件的名称，以满足 gorm.Plugin 接口的要求。
func (p *GormMetricsPlugin) Name() string {
	return p.name
}

// Initialize 是插件的入口，它向GORM注册所有必要的before和after回调。
func (p *GormMetricsPlugin) Initialize(db *gorm.DB) error {
	var err error

	// 注册Query操作的回调
	err = db.Callback().Query().Before("gorm:query").Register("prometheus:before_query", p.before("query"))
	if err != nil {
		return err
	}
	err = db.Callback().Query().After("gorm:query").Register("prometheus:after_query", p.after)
	if err != nil {
		return err
	}

	// 注册Create操作的回调
	err = db.Callback().Create().Before("gorm:create").Register("prometheus:before_create", p.before("create"))
	if err != nil {
		return err
	}
	err = db.Callback().Create().After("gorm:create").Register("prometheus:after_create", p.after)
	if err != nil {
		return err
	}

	// 注册Update操作的回调
	err = db.Callback().Update().Before("gorm:update").Register("prometheus:before_update", p.before("update"))
	if err != nil {
		return err
	}
	err = db.Callback().Update().After("gorm:update").Register("prometheus:after_update", p.after)
	if err != nil {
		return err
	}

	// 注册Delete操作的回调
	err = db.Callback().Delete().Before("gorm:delete").Register("prometheus:before_delete", p.before("delete"))
	if err != nil {
		return err
	}
	err = db.Callback().Delete().After("gorm:delete").Register("prometheus:after_delete", p.after)
	if err != nil {
		return err
	}

	// 注册Row操作的回调
	err = db.Callback().Row().Before("gorm:row").Register("prometheus:before_row", p.before("row"))
	if err != nil {
		return err
	}
	err = db.Callback().Row().After("gorm:row").Register("prometheus:after_row", p.after)
	if err != nil {
		return err
	}

	// 注册Raw操作的回调
	err = db.Callback().Raw().Before("gorm:raw").Register("prometheus:before_raw", p.before("raw"))
	if err != nil {
		return err
	}
	err = db.Callback().Raw().After("gorm:raw").Register("prometheus:after_raw", p.after)
	if err != nil {
		return err
	}

	return nil
}

// before 是一个回调工厂函数。它返回一个gorm.Callback函数，用于在数据库操作执行前被调用。
// 它接收操作类型作为参数，并通过闭包将其捕获。
func (p *GormMetricsPlugin) before(operation string) func(*gorm.DB) {
	return func(db *gorm.DB) {
		// 使用 db.Set 在当前事务/操作的上下文中存储开始时间和操作类型。
		// GORM保证这些值在对应的 "after" 回调中是可用的。
		db.Set(callbackStartTimeKey, time.Now())
		db.Set(callbackOperationKey, operation)
	}
}

// after 是一个通用的 "after" 回调函数，在数据库操作执行后被调用。
// 所有操作都可以复用此函数。
func (p *GormMetricsPlugin) after(db *gorm.DB) {
	// 从DB实例上下文中安全地获取操作类型和开始时间。
	opVal, _ := db.Get(callbackOperationKey)
	operation, ok := opVal.(string)
	if !ok {
		// 如果没有找到操作类型，说明这不是由我们的 "before" 回调启动的，直接返回。
		return
	}
	// 调用通用的指标记录函数。
	p.recordMetrics(db, operation)
}

// recordMetrics 负责计算操作耗时并记录到Prometheus。
// 所有操作都可以复用此函数。
func (p *GormMetricsPlugin) recordMetrics(db *gorm.DB, operation string) {
	// 从DB实例中获取由 "before" 回调设置的开始时间。
	startTimeVal, exists := db.Get(callbackStartTimeKey)
	if !exists {
		// 如果开始时间不存在，无法计算耗时，直接返回。
		return
	}

	startTime, ok := startTimeVal.(time.Time)
	if !ok {
		// 如果存储的值类型不正确，也直接返回。
		return
	}

	// 计算操作耗时。
	duration := time.Since(startTime)

	// 获取表名。db.Statement 是GORM的核心，包含了当前操作的所有元数据。
	table := db.Statement.Table
	if table == "" {
		// 对于某些原生SQL查询，GORM可能无法自动推断表名。
		// 在这种情况下，我们提供一个默认值以避免空标签。
		table = "unknown"
	}

	// 使用获取到的`operation`和`table`作为标签，记录指标。
	// Observe方法会将本次操作的耗时（秒）添加到直方图中。
	p.queryDuration.WithLabelValues(operation, table).Observe(duration.Seconds())
}
