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
	redisCacheHits    prometheus.Counter
	redisCacheMisses  prometheus.Counter
	redisHotKeys      *prometheus.CounterVec

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

func Init() {
	once.Do(func() {
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

	redisCacheHits = promauto.NewCounter(prometheus.CounterOpts{
			Name: "redis_cache_hits_total",
			Help: "Total number of Redis cache hits",
		})

		redisCacheMisses = promauto.NewCounter(prometheus.CounterOpts{
			Name: "redis_cache_misses_total",
			Help: "Total number of Redis cache misses",
		})

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
	})
}

func RecordRequest(service, method, endpoint string, status int, duration time.Duration) {
	requestsInFlight.Dec()
	requestDuration.WithLabelValues(service, method, endpoint).Observe(duration.Seconds())
	httpRequestsTotal.WithLabelValues(service, method, endpoint, strconv.Itoa(status)).Inc()
}

func IncrementInFlight() {
	requestsInFlight.Inc()
}

func RecordError(errorType string) {
	errorsTotal.WithLabelValues(errorType).Inc()
}

func UpdateSystemMetrics() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	memoryUsage.Set(float64(m.Alloc))
	goroutineCount.Set(float64(runtime.NumGoroutine()))
}

func RecordSlowQuery(sql string, duration time.Duration) {
	mysqlSlowQueries.Inc()
	slowQueryDuration.WithLabelValues(sql).Observe(duration.Seconds())
}

// RecordRedisHit 记录一次缓存访问结果（命中/未命中）。命中率比率由
// sum(rate(redis_cache_hits_total[5m])) / (hits + misses) 计算得出，
// 比原先的 0/1 Gauge 更符合 Prometheus 语义且可跨时间聚合。
func RecordRedisHit(hit bool) {
	if hit {
		redisCacheHits.Inc()
	} else {
		redisCacheMisses.Inc()
	}
}

// RecordCacheHit 显式记录一次缓存命中。
func RecordCacheHit() {
	redisCacheHits.Inc()
}

// RecordCacheMiss 显式记录一次缓存未命中。
func RecordCacheMiss() {
	redisCacheMisses.Inc()
}

func RecordHotKey(key string) {
	redisHotKeys.WithLabelValues(key).Inc()
}

func RecordPanic(service string) {
	panicCounter.WithLabelValues(service).Inc()
}

func RecordRPCRequest(service, method, status string, duration time.Duration) {
	rpcRequestsTotal.WithLabelValues(service, method, status).Inc()
	rpcRequestDuration.WithLabelValues(service, method).Observe(duration.Seconds())
}

func RecordCacheOperation(operation, status string) {
	cacheOperations.WithLabelValues(operation, status).Inc()
}

func RecordDBOperation(operation, status string) {
	dbOperations.WithLabelValues(operation, status).Inc()
}

func SetServiceHealth(service string, healthy bool) {
	if healthy {
		serviceHealth.WithLabelValues(service).Set(1.0)
	} else {
		serviceHealth.WithLabelValues(service).Set(0.0)
	}
}

// ---- 网关层指标便捷函数（供 gateway proxy handler 埋点） ----

// RecordGatewayRequest 记录网关处理的请求数（按 method/endpoint/status）。
func RecordGatewayRequest(method, endpoint string, status int) {
	gatewayRequestsTotal.WithLabelValues(method, endpoint, strconv.Itoa(status)).Inc()
}

// RecordGatewayError 记录网关错误数（按错误类型）。
func RecordGatewayError(errType string) {
	gatewayErrorsTotal.WithLabelValues(errType).Inc()
}

// GatewayConnInc/Dec 维护网关活跃上游连接数（Gauge）。
func GatewayConnInc() { gatewayActiveConnections.Inc() }
func GatewayConnDec() { gatewayActiveConnections.Dec() }

// RecordGatewayUpstreamLatency 记录网关到后端的转发延迟。
func RecordGatewayUpstreamLatency(service, endpoint string, duration time.Duration) {
	gatewayUpstreamDuration.WithLabelValues(service, endpoint).Observe(duration.Seconds())
}

// RecordReportGenerated 记录报表生成次数（供 report-service 调用）。
func RecordReportGenerated(reportType string) {
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
