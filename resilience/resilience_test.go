package resilience

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/mysunshines/gocommon/constants"
)

func TestPolicyExecuteSuccess(t *testing.T) {
	resilienceSet("ok", Policy{Timeout: time.Second})
	err := ForService("ok").Execute(context.Background(), func(context.Context) error { return nil }, nil)
	if err != nil {
		t.Fatalf("期望成功，got %v", err)
	}
}

func TestPolicyExecuteFallback(t *testing.T) {
	resilienceSet("fb", Policy{Timeout: time.Second})
	// Execute 的降级由第 3 个 fallback 参数驱动（而非 Policy.Fallback 字段）。
	fb := func(context.Context) (interface{}, error) { return "fb", nil }
	err := ForService("fb").Execute(context.Background(), func(context.Context) error { return errors.New("boom") }, fb)
	if err != nil {
		t.Fatalf("有 Fallback 时应返回 nil，got %v", err)
	}
}

func TestPolicyExecuteNoFallback(t *testing.T) {
	resilienceSet("err2", Policy{Timeout: time.Second})
	err := ForService("err2").Execute(context.Background(), func(context.Context) error { return errors.New("boom") }, nil)
	if err == nil {
		t.Fatal("无 Fallback 时应返回错误")
	}
}

func TestForServiceDefault(t *testing.T) {
	DeletePolicy("nope")
	p := ForService("nope")
	if p.Timeout != constants.DefaultCallTimeout*time.Second {
		t.Errorf("默认超时应为 %v，got %v", constants.DefaultCallTimeout*time.Second, p.Timeout)
	}
	if p.Circuit.Enabled || p.RateLimit.Enabled {
		t.Error("默认策略不应启用熔断/限流")
	}
}

func TestCircuitOpen(t *testing.T) {
	key := "circuit-test"
	SetPolicy(key, Policy{
		Timeout: time.Second,
		Circuit: CircuitConfig{Enabled: true, MaxRequests: 1, Interval: 50 * time.Millisecond, Timeout: 50 * time.Millisecond, ErrorThreshold: 0.5},
	})
	ctx := WithServiceKey(context.Background(), key)
	// 一次失败即触发熔断（单样本错误率 100% >= 阈值）
	_ = ForService(key).Execute(ctx, func(context.Context) error { return errors.New("x") }, nil)
	err := ForService(key).Execute(ctx, func(context.Context) error { return nil }, nil)
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("期望熔断打开 ErrCircuitOpen，got %v", err)
	}
}

func TestPolicySpecToPolicy(t *testing.T) {
	p := PolicySpec{
		Timeout:   2,
		Circuit:   CircuitSpec{Enabled: true, MaxRequests: 3, IntervalSec: 10, TimeoutSec: 30, ErrorThreshold: 0.5},
		RateLimit: RateSpec{Enabled: true, QPS: 500, Burst: 1000},
	}.ToPolicy()
	if p.Timeout != 2*time.Second {
		t.Errorf("Timeout got %v", p.Timeout)
	}
	if !p.Circuit.Enabled || p.Circuit.MaxRequests != 3 || p.Circuit.Interval != 10*time.Second {
		t.Errorf("Circuit 转换错误: %+v", p.Circuit)
	}
	if !p.RateLimit.Enabled || p.RateLimit.QPS != 500 || p.RateLimit.Burst != 1000 {
		t.Errorf("RateLimit 转换错误: %+v", p.RateLimit)
	}
}

func TestApplySpecs(t *testing.T) {
	ApplySpecs(map[string]PolicySpec{"svc.a": {Timeout: 5}})
	if p := ForService("svc.a"); p.Timeout != 5*time.Second {
		t.Errorf("ApplySpecs 未生效，Timeout got %v", p.Timeout)
	}
}

// resilienceSet 仅设置策略（测试辅助，等价于 SetPolicy 但明确语义）。
func resilienceSet(key string, p Policy) { SetPolicy(key, p) }

func ExamplePolicy_Execute() {
	SetPolicy("demo", Policy{Timeout: time.Second})
	err := ForService("demo").Execute(context.Background(), func(ctx context.Context) error { return nil }, nil)
	fmt.Println(err)
	// Output: <nil>
}

func ExamplePolicySpec_ToPolicy() {
	p := PolicySpec{Timeout: 2}.ToPolicy()
	fmt.Println(p.Timeout)
	// Output: 2s
}
