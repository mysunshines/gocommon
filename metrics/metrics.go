package metrics

import (
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
	redisHitRate      prometheus.Gauge
	redisHotKeys      *prometheus.CounterVec

	httpRequestsTotal  *prometheus.CounterVec
	rpcRequestsTotal   *prometheus.CounterVec
	rpcRequestDuration *prometheus.HistogramVec
	cacheOperations    *prometheus.CounterVec
	dbOperations       *prometheus.CounterVec
	serviceHealth      *prometheus.GaugeVec

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

		redisHitRate = promauto.NewGauge(prometheus.GaugeOpts{
			Name: "redis_hit_rate",
			Help: "Redis cache hit rate",
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

func RecordRedisHit(hit bool) {
	if hit {
		redisHitRate.Set(1.0)
	} else {
		redisHitRate.Set(0.0)
	}
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
