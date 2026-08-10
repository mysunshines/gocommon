package configcenter

import (
	"time"

	"github.com/mysunshines/gocommon/log"
	"github.com/mysunshines/gocommon/resilience"
)

// ResilienceConfig 是经配置中心下发的、按 serviceKey 区分的出站韧性策略集合。
// 它与 HotConfig 解耦，使用独立 KV key（resilience/<service>/<env>），
// 便于把"各下游超时/熔断/限流"集中在一处热更，而不污染业务热更配置。
//
// YAML 示例：
//
//	services:
//	  user.v1:
//	    timeout_sec: 2
//	    circuit:
//	      enabled: true
//	      max_requests: 3
//	      interval_sec: 10
//	      timeout_sec: 30
//	      error_threshold: 0.5
//	    rate_limit:
//	      enabled: true
//	      qps: 500
//	      burst: 1000
//	  "http://sms-api":
//	    timeout_sec: 5
//	    circuit:
//	      enabled: true
type ResilienceConfig struct {
	Services map[string]PolicySpec `yaml:"services" json:"services"`
}

// PolicySpec 是单条策略的可序列化描述（time.Duration 以秒为单位）。
type PolicySpec struct {
	Timeout   int          `yaml:"timeout_sec" json:"timeout_sec"`
	Circuit   CircuitSpec  `yaml:"circuit" json:"circuit"`
	RateLimit RateSpec     `yaml:"rate_limit" json:"rate_limit"`
}

// CircuitSpec 熔断策略可序列化描述。
type CircuitSpec struct {
	Enabled         bool    `yaml:"enabled" json:"enabled"`
	MaxRequests     int     `yaml:"max_requests" json:"max_requests"`
	IntervalSec     int     `yaml:"interval_sec" json:"interval_sec"`
	TimeoutSec      int     `yaml:"timeout_sec" json:"timeout_sec"`
	ErrorThreshold  float64 `yaml:"error_threshold" json:"error_threshold"`
}

// RateSpec 限流策略可序列化描述。
type RateSpec struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
	QPS     int  `yaml:"qps" json:"qps"`
	Burst   int  `yaml:"burst" json:"burst"`
}

// ResilienceKey 拼接 resilience 策略在 Consul KV 中的完整 key。
// 形如 resilience/<service>/<env>，例如 resilience/article-service/production。
func ResilienceKey(service, env string) string {
	return "resilience/" + service + "/" + env
}

// ApplyResilience 将 ResilienceConfig 中的每条策略转换为 resilience.Policy 并
// 调用 resilience.SetPolicy 即时生效（热更回调里直接复用即可）。
func ApplyResilience(cfg *ResilienceConfig) {
	if cfg == nil || len(cfg.Services) == 0 {
		return
	}
	for key, spec := range cfg.Services {
		p := resilience.Policy{
			Timeout: time.Duration(spec.Timeout) * time.Second,
			Circuit: resilience.CircuitConfig{
				Enabled:        spec.Circuit.Enabled,
				MaxRequests:    spec.Circuit.MaxRequests,
				Interval:       time.Duration(spec.Circuit.IntervalSec) * time.Second,
				Timeout:        time.Duration(spec.Circuit.TimeoutSec) * time.Second,
				ErrorThreshold: spec.Circuit.ErrorThreshold,
			},
			RateLimit: resilience.RateLimitConfig{
				Enabled: spec.RateLimit.Enabled,
				QPS:     spec.RateLimit.QPS,
				Burst:   spec.RateLimit.Burst,
			},
		}
		resilience.SetPolicy(key, p)
		log.Infof("configcenter: resilience policy applied for %q (timeout=%v circuit=%v ratelimit=%v)",
			key, p.Timeout, p.Circuit.Enabled, p.RateLimit.Enabled)
	}
}

// LoadResilience 从 Consul KV 拉取并应用 resilience 策略。key 不存在时返回 ErrNotFound，
// 调用方可忽略（保留进程内已 SetPolicy 的值或回落默认策略）。
func (sc *ServiceConfig) LoadResilience() error {
	return sc.client.LoadResilience(sc.service, sc.env)
}

// WatchResilience 在后台长轮询 resilience KV key，变更时即时刷新策略。
// 阻塞式，应在 goroutine 中调用（如 go sc.WatchResilience()）。
func (sc *ServiceConfig) WatchResilience() {
	sc.client.WatchResilience(sc.service, sc.env)
}

// LoadResilience 从 Consul KV 拉取并应用 resilience 策略。key 不存在时返回 ErrNotFound。
// service/env 仅用于拼接 KV key（resilience/<service>/<env>），与具体业务无关，
// 因此 *Client 也暴露此方法，便于未使用 ServiceConfig 的服务（如 report-service）直接调用。
func (c *Client) LoadResilience(service, env string) error {
	key := ResilienceKey(service, env)
	var rc ResilienceConfig
	if err := c.Load(key, &rc); err != nil {
		return err
	}
	ApplyResilience(&rc)
	return nil
}

// WatchResilience 在后台长轮询 resilience KV key，变更时即时刷新策略。
// 阻塞式，应在 goroutine 中调用（如 go client.WatchResilience(...)）。
func (c *Client) WatchResilience(service, env string) {
	key := ResilienceKey(service, env)
	var rc ResilienceConfig
	_ = c.Watch(key, &rc, func() {
		ApplyResilience(&rc)
	})
}
