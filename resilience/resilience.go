// Package resilience 为 gocommon 的所有出站网络调用（gRPC / HTTP / TCP / UDP）
// 提供统一、可动态调的韧性控制：超时（Timeout）、熔断（Circuit Breaker）、
// 限流（Rate Limit）与降级（Fallback）。
//
// 设计要点：
//   - 策略以 "serviceKey"（如 "user.v1"、"http://sms-api"）为粒度聚合在 Policy 中；
//   - 策略存放于进程内并发安全的注册表，可经 configcenter 热更即时刷新（无需重启）；
//   - 当某 serviceKey 没有显式注册策略时，返回内置默认策略（基于 constants 默认值），
//     保证零配置也能安全运行；
//   - 熔断为轻量状态机实现（closed/open/half-open），不引入额外第三方依赖；
//   - 限流复用 golang.org/x/time/rate 令牌桶；
//   - 降级通过 Fallback func 在熔断打开或下游不可用时返回兜底结果。
//
// 典型用法：
//
//	// 1) 启动时为每个下游声明策略（或交给 configcenter 热更下发）
//	resilience.SetPolicy("user.v1", resilience.Policy{
//	    Timeout: 2 * time.Second,
//	    Circuit: resilience.CircuitConfig{Enabled: true},
//	    RateLimit: resilience.RateLimitConfig{Enabled: true, QPS: 500, Burst: 1000},
//	})
//	// 2) 出站调用侧直接拿策略包裹：
//	policy := resilience.ForService("user.v1")
//	err := policy.Execute(ctx, func(c context.Context) error {
//	    return grpcclient.SendRequest(c, user.UserService_xxx_FullMethodName, req, resp)
//	}, fallbackFn)
package resilience

import (
	"context"
	"sync"
	"time"

	"github.com/mysunshines/gocommon/constants"
)

// Policy 是单个下游服务的韧性策略聚合。
// 任一子策略的 Enabled=false 或零值即表示不启用该控制（走默认行为）。
type Policy struct {
	// Timeout 单次出站调用的最长允许耗时。0 表示使用默认超时。
	Timeout time.Duration
	// Circuit 熔断器配置。
	Circuit CircuitConfig
	// RateLimit 限流配置。
	RateLimit RateLimitConfig
	// Fallback 降级函数：当熔断打开或调用返回致命错误时调用，返回兜底结果与 error。
	// 可为 nil（不降级，直接向上游返回错误）。
	Fallback func(ctx context.Context) (interface{}, error)
}

// CircuitConfig 熔断器配置。
type CircuitConfig struct {
	// Enabled 是否启用熔断。
	Enabled bool
	// MaxRequests 半开状态下允许放行的探测请求数（<=0 取 constants.DefaultCBMaxRequests）。
	MaxRequests int
	// Interval 闭合态统计窗口（<=0 取 constants.DefaultCBInterval 秒）：窗口内错误率超阈值则打开。
	Interval time.Duration
	// Timeout 熔断打开后持续拒绝请求的时长（<=0 取 constants.DefaultCBTimeout 秒）。
	Timeout time.Duration
	// ErrorThreshold 打开熔断所需的错误率阈值（0~1，<=0 取 0.5）。
	ErrorThreshold float64
}

// RateLimitConfig 限流配置（令牌桶）。
type RateLimitConfig struct {
	// Enabled 是否启用限流。
	Enabled bool
	// QPS 令牌桶 refill 速率（<=0 且 Enabled 时取 1000）。
	QPS int
	// Burst 令牌桶突发容量（<=0 且 Enabled 时取 QPS*2）。
	Burst int
}

// policies 是 serviceKey -> Policy 的并发安全注册表。
var (
	policyMu sync.RWMutex
	policies = make(map[string]Policy)
)

// SetPolicy 为指定 serviceKey 设置/覆盖韧性策略。通常由启动初始化或
// configcenter 热更回调调用，即时生效。
func SetPolicy(serviceKey string, p Policy) {
	policyMu.Lock()
	policies[serviceKey] = p
	policyMu.Unlock()
}

// DeletePolicy 移除指定 serviceKey 的策略，之后该 key 回落到默认策略。
func DeletePolicy(serviceKey string) {
	policyMu.Lock()
	delete(policies, serviceKey)
	policyMu.Unlock()
}

// ForService 返回指定 serviceKey 的生效策略：优先返回显式 SetPolicy 的策略，
// 否则返回默认策略（基于 constants 默认值，保证零配置可用）。
func ForService(serviceKey string) Policy {
	policyMu.RLock()
	if p, ok := policies[serviceKey]; ok {
		policyMu.RUnlock()
		return normalize(p)
	}
	policyMu.RUnlock()
	return defaultPolicy()
}

// defaultPolicy 返回零配置下的兜底策略：仅超时（沿用 gRPC/HTTP 既有默认值），
// 不启用熔断/限流，避免无配置时行为突变。
func defaultPolicy() Policy {
	return Policy{
		Timeout: constants.DefaultReadTimeout * time.Second,
	}
}

// normalize 补全策略中缺失的默认值，使后续读取者拿到即可用的完整配置。
func normalize(p Policy) Policy {
	if p.Timeout <= 0 {
		p.Timeout = constants.DefaultReadTimeout * time.Second
	}
	if p.Circuit.Enabled {
		if p.Circuit.MaxRequests <= 0 {
			p.Circuit.MaxRequests = constants.DefaultCBMaxRequests
		}
		if p.Circuit.Interval <= 0 {
			p.Circuit.Interval = constants.DefaultCBInterval * time.Second
		}
		if p.Circuit.Timeout <= 0 {
			p.Circuit.Timeout = constants.DefaultCBTimeout * time.Second
		}
		if p.Circuit.ErrorThreshold <= 0 {
			p.Circuit.ErrorThreshold = 0.5
		}
	}
	if p.RateLimit.Enabled {
		if p.RateLimit.QPS <= 0 {
			p.RateLimit.QPS = 1000
		}
		if p.RateLimit.Burst <= 0 {
			p.RateLimit.Burst = p.RateLimit.QPS * 2
		}
	}
	return p
}
