package metrics

import (
	"testing"
	"time"
)

func TestInitAndRecord(t *testing.T) {
	Init("test-service")
	IncrementInFlight()
	RecordRequest("svc", "GET", "/health", 200, 10*time.Millisecond)
	RecordError("db")
	UpdateSystemMetrics()
	RecordSlowQuery("SELECT 1", 100*time.Millisecond)
	RecordRedisHit(true)
	RecordRedisHit(false)
	RecordCacheHit()
	RecordCacheMiss()
	RecordHotKey("hot-key")
	RecordPanic("svc")
	RecordRPCRequest("svc", "Method", "200", 5*time.Millisecond)
	RecordCacheOperation("get", "hit")
	RecordDBOperation("query", "ok")
	SetServiceHealth("svc", true)
	SetServiceHealth("svc", false)
}

func ExampleInit() {
	Init("test-service")
	IncrementInFlight()
	RecordRequest("demo", "GET", "/health", 200, 0)
	RecordError("demo")
}
