package metrics

import (
	"context"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	requestsInFlight  prometheus.Gauge
	requestDuration   *prometheus.HistogramVec
	errorsTotal       *prometheus.CounterVec
	memoryUsage       prometheus.Gauge
	goroutineCount    prometheus.Gauge
	panicCounter      *prometheus.CounterVec
	mysqlSlowQueries  prometheus.Counter
	slowQueryDuration *prometheus.HistogramVec
	redisCacheHits    *prometheus.CounterVec
	redisCacheMisses  *prometheus.CounterVec
	redisHotKeys      *prometheus.CounterVec

	// serviceName 为当前服务名（如 constants.ServiceNameArticle），用于给
	// redis_cache_hits_total / redis_cache_misses_total 打上 service 标签，
	// 方便在 Prometheus/Grafana 中按服务维度拆分缓存命中率。由 SetServiceName 设置。
	serviceName string

	httpRequestsTotal  *prometheus.CounterVec
	rpcRequestsTotal   *prometheus.CounterVec
	rpcRequestDuration *prometheus.HistogramVec
	cacheOperations    *prometheus.CounterVec
	dbOperations       *prometheus.CounterVec
	serviceHealth      *prometheus.GaugeVec

	// 网关层指标（dashboard gateway.json 引用）
	gatewayRequestsTotal     *prometheus.CounterVec
	gatewayErrorsTotal       *prometheus.CounterVec
	gatewayActiveConnections prometheus.Gauge
	gatewayUpstreamDuration  *prometheus.HistogramVec

	// 报表生成计数（dashboard report-service.json 引用 report_generated_total）
	reportGeneratedTotal *prometheus.CounterVec

	once sync.Once
)

// Init 初始化 metrics 子系统（注册 Prometheus 指标）。serviceName 用于给
// 带 service 标签的指标（如 redis_cache_hits_total）打标，应在启动时传入当前服务名
// （如 constants.ServiceNameArticle）。
func Init(serviceName string) {
	once.Do(func() {
		initMetrics(serviceName)
	})
}

// ensureInit 保证指标已注册：服务未（或尚未）调用 Init 时，以 "unknown"
// 服务名惰性初始化。所有 Record*/Set*/Inc* 入口前置调用，
// 防止 panic 恢复等兜底路径在 Init 之前触发 nil 解引用二次 panic。
func ensureInit() {
	once.Do(func() {
		initMetrics("unknown")
	})
}

// initMetrics 实际的指标注册逻辑，仅由 Init/ensureInit 经 sync.Once 调用一次。
// name 赋给包级 serviceName（供 RecordRedisHit 等带 service 标签的指标使用）。
// 注意：参数不可命名为 serviceName —— 会遮蔽包级变量，历史实现因此从未把
// 服务名写入包级变量（redis_cache_hits_total 的 service 标签为空串）。
func initMetrics(name string) {
	serviceName = name
	{
		requestsInFlight = promauto.NewGauge(prometheus.GaugeOpts{
			Name: "requests_in_flight",
			Help: "Number of requests currently being processed",
		})

		requestDuration = promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "request_duration_seconds",
				Help:    "Request latency in seconds",
				Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
			},
			[]string{"service", "method", "endpoint"},
		)

		errorsTotal = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "errors_total",
				Help: "Total number of errors",
			},
			[]string{"type"},
		)

		memoryUsage = promauto.NewGauge(prometheus.GaugeOpts{
			Name: "memory_usage_bytes",
			Help: "Memory usage in bytes",
		})

		goroutineCount = promauto.NewGauge(prometheus.GaugeOpts{
			Name: "goroutine_count",
			Help: "Number of goroutines",
		})

		panicCounter = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "panic_counter_total",
				Help: "Total number of panics",
			},
			[]string{"service"},
		)

		mysqlSlowQueries = promauto.NewCounter(prometheus.CounterOpts{
			Name: "mysql_slow_queries_total",
			Help: "Total number of MySQL slow queries",
		})

		slowQueryDuration = promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "mysql_slow_query_duration_seconds",
				Help:    "MySQL slow query latency in seconds",
				Buckets: []float64{.1, .25, .5, 1, 2.5, 5, 10, 30, 60},
			},
			[]string{"operation"},
		)

		redisCacheHits = promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "redis_cache_hits_total",
			Help: "Total number of Redis cache hits",
		}, []string{"service"})

		redisCacheMisses = promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "redis_cache_misses_total",
			Help: "Total number of Redis cache misses",
		}, []string{"service"})

		redisHotKeys = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "redis_hot_keys_total",
				Help: "Total number of hot key accesses",
			},
			[]string{"key"},
		)

		httpRequestsTotal = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "http_requests_total",
				Help: "Total number of HTTP requests",
			},
			[]string{"service", "method", "endpoint", "status"},
		)

		rpcRequestsTotal = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "rpc_requests_total",
				Help: "Total number of RPC requests",
			},
			[]string{"service", "method", "status"},
		)

		rpcRequestDuration = promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "rpc_request_duration_seconds",
				Help:    "RPC request latency in seconds",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"service", "method"},
		)

		cacheOperations = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "cache_operations_total",
				Help: "Total number of cache operations",
			},
			[]string{"operation", "status"},
		)

		dbOperations = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "db_operations_total",
				Help: "Total number of database operations",
			},
			[]string{"operation", "status"},
		)

		serviceHealth = promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "service_health",
				Help: "Service health status (1=healthy, 0=unhealthy)",
			},
			[]string{"service"},
		)

		// 网关层指标（dashboard gateway.json 引用）
		gatewayRequestsTotal = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "gateway_requests_total",
				Help: "Total number of requests handled by the gateway",
			},
			[]string{"method", "endpoint", "status"},
		)

		gatewayErrorsTotal = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "gateway_errors_total",
				Help: "Total number of gateway errors",
			},
			[]string{"type"},
		)

		gatewayActiveConnections = promauto.NewGauge(prometheus.GaugeOpts{
			Name: "gateway_active_connections",
			Help: "Number of active upstream connections held by the gateway",
		})

		gatewayUpstreamDuration = promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "gateway_upstream_duration_seconds",
				Help:    "Gateway upstream (backend) latency in seconds",
				Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
			},
			[]string{"service", "endpoint"},
		)

		// 报表生成计数（dashboard report-service.json 引用 report_generated_total）
		reportGeneratedTotal = promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "report_generated_total",
				Help: "Total number of reports generated",
			},
			[]string{"type"},
		)
	}
}

// RecordRequest 记录一次 HTTP 请求指标（请求耗时、HTTP 请求数，并递减在途请求计数）。
func RecordRequest(service, method, endpoint string, status int, duration time.Duration) {
	ensureInit()
	requestsInFlight.Dec()
	requestDuration.WithLabelValues(service, method, endpoint).Observe(duration.Seconds())
	httpRequestsTotal.WithLabelValues(service, method, endpoint, strconv.Itoa(status)).Inc()
}

// IncrementInFlight 递增在途请求数 Gauge，表示一个新的请求开始处理。
func IncrementInFlight() {
	ensureInit()
	requestsInFlight.Inc()
}

// RecordError 记录一次错误，按错误类型累加错误总数。
func RecordError(errorType string) {
	ensureInit()
	errorsTotal.WithLabelValues(errorType).Inc()
}

// UpdateSystemMetrics 采集并上报当前内存占用与 goroutine 数量等系统指标。
func UpdateSystemMetrics() {
	ensureInit()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	memoryUsage.Set(float64(m.Alloc))
	goroutineCount.Set(float64(runtime.NumGoroutine()))
}

// RecordSlowQuery 记录一次 MySQL 慢查询，累加慢查询数并统计其耗时。
func RecordSlowQuery(sql string, duration time.Duration) {
	ensureInit()
	mysqlSlowQueries.Inc()
	slowQueryDuration.WithLabelValues(sql).Observe(duration.Seconds())
}

// SetServiceName 覆盖当前服务名（默认在 Init(serviceName) 时设置）。
// 仅在需要运行时变更服务名时调用；正常情况无需使用。
func SetServiceName(name string) {
	serviceName = name
}

// RecordRedisHit 记录一次缓存访问结果（命中/未命中）。命中率比率由
// sum(rate(redis_cache_hits_total[5m])) / (hits + misses) 计算得出，
// 比原先的 0/1 Gauge 更符合 Prometheus 语义且可跨时间聚合。指标带 service 标签。
func RecordRedisHit(hit bool) {
	ensureInit()
	if hit {
		redisCacheHits.WithLabelValues(serviceName).Inc()
	} else {
		redisCacheMisses.WithLabelValues(serviceName).Inc()
	}
}

// RecordCacheHit 显式记录一次缓存命中（带 service 标签）。
func RecordCacheHit() {
	ensureInit()
	redisCacheHits.WithLabelValues(serviceName).Inc()
}

// RecordCacheMiss 显式记录一次缓存未命中（带 service 标签）。
func RecordCacheMiss() {
	ensureInit()
	redisCacheMisses.WithLabelValues(serviceName).Inc()
}

// RecordHotKey 记录一次热点 key 访问，按 key 累加访问次数。
func RecordHotKey(key string) {
	ensureInit()
	redisHotKeys.WithLabelValues(key).Inc()
}

// RecordPanic 记录一次 panic 发生，按服务名累加 panic 总数。
func RecordPanic(service string) {
	ensureInit()
	panicCounter.WithLabelValues(service).Inc()
}

// RecordRPCRequest 记录一次 RPC 请求指标（RPC 请求数及请求耗时）。
func RecordRPCRequest(service, method, status string, duration time.Duration) {
	ensureInit()
	rpcRequestsTotal.WithLabelValues(service, method, status).Inc()
	rpcRequestDuration.WithLabelValues(service, method).Observe(duration.Seconds())
}

// RecordCacheOperation 记录一次缓存操作，按操作类型与状态累加。
func RecordCacheOperation(operation, status string) {
	ensureInit()
	cacheOperations.WithLabelValues(operation, status).Inc()
}

// RecordDBOperation 记录一次数据库操作，按操作类型与状态累加。
func RecordDBOperation(operation, status string) {
	ensureInit()
	dbOperations.WithLabelValues(operation, status).Inc()
}

// SetServiceHealth 设置指定服务的健康状态（1=健康，0=不健康）。
func SetServiceHealth(service string, healthy bool) {
	ensureInit()
	if healthy {
		serviceHealth.WithLabelValues(service).Set(1.0)
	} else {
		serviceHealth.WithLabelValues(service).Set(0.0)
	}
}

// ---- 网关层指标便捷函数（供 gateway proxy handler 埋点） ----

// RecordGatewayRequest 记录网关处理的请求数（按 method/endpoint/status）。
func RecordGatewayRequest(method, endpoint string, status int) {
	ensureInit()
	gatewayRequestsTotal.WithLabelValues(method, endpoint, strconv.Itoa(status)).Inc()
}

// RecordGatewayError 记录网关错误数（按错误类型）。
func RecordGatewayError(errType string) {
	ensureInit()
	gatewayErrorsTotal.WithLabelValues(errType).Inc()
}

// GatewayConnInc/Dec 维护网关活跃上游连接数（Gauge）。
func GatewayConnInc() {
	ensureInit()
	gatewayActiveConnections.Inc()
}

// GatewayConnDec 递减网关活跃上游连接数 Gauge，表示一条上游连接关闭。
func GatewayConnDec() {
	ensureInit()
	gatewayActiveConnections.Dec()
}

// RecordGatewayUpstreamLatency 记录网关到后端的转发延迟。
func RecordGatewayUpstreamLatency(service, endpoint string, duration time.Duration) {
	ensureInit()
	gatewayUpstreamDuration.WithLabelValues(service, endpoint).Observe(duration.Seconds())
}

// RecordReportGenerated 记录报表生成次数（供 report-service 调用）。
func RecordReportGenerated(reportType string) {
	ensureInit()
	reportGeneratedTotal.WithLabelValues(reportType).Inc()
}

// ---- P1：让"已注册但无 series"的指标真正产出数据 ----

// StartRuntimeMetrics 启动一个定时 ticker，周期性刷新内存/goroutine 指标，
// 解决 memory_usage_bytes / goroutine_count 长期为 0 的问题。各服务 main 调一次即可。
func StartRuntimeMetrics(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	UpdateSystemMetrics() // 立即采集一次
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				UpdateSystemMetrics()
			}
		}
	}()
}

// StartHealthReporter 启动一个定时 ticker，周期性探测 DB/Redis 可用性并上报
// service_health 指标，解决 dashboard 服务健康 panel 长期 No data 的问题。
// dbPing / redisPing 可为 nil（跳过对应探测），签名带 ctx 便于传递超时上下文。各服务 main 调一次即可。
func StartHealthReporter(ctx context.Context, serviceName string, interval time.Duration, dbPing func(context.Context) error, redisPing func(context.Context) error) {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	probe := func() bool {
		healthy := true
		if dbPing != nil {
			if err := dbPing(ctx); err != nil {
				healthy = false
			}
		}
		if redisPing != nil {
			if err := redisPing(ctx); err != nil {
				healthy = false
			}
		}
		return healthy
	}
	SetServiceHealth(serviceName, probe()) // 立即探测一次
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				SetServiceHealth(serviceName, probe())
			}
		}
	}()
}
