package resilience

import "time"

// PolicySpec 是单条韧性策略的 YAML/JSON 可序列化描述。
// 与运行时的 Policy 分离，原因是 Policy.Timeout 等是 time.Duration（int64 纳秒），
// 在 YAML 中写作字符串 "2s" 易错且运维不友好；PolicySpec 统一用"秒"为单位的整型，
// 由 ToPolicy 转换为 Policy。这是配置中心热更下发的标准形态。
//
// YAML 示例：
//
//	user.v1:
//	  timeout_sec: 2
//	  circuit:
//	    enabled: true
//	    max_requests: 3
//	    interval_sec: 10
//	    timeout_sec: 30
//	    error_threshold: 0.5
//	  rate_limit:
//	    enabled: true
//	    qps: 500
//	    burst: 1000
//	http://sms-api:
//	  timeout_sec: 5
//	  circuit:
//	    enabled: true
type PolicySpec struct {
	Timeout   int         `yaml:"timeout_sec" json:"timeout_sec"`
	Circuit   CircuitSpec `yaml:"circuit" json:"circuit"`
	RateLimit RateSpec    `yaml:"rate_limit" json:"rate_limit"`
}

// CircuitSpec 熔断策略可序列化描述（秒为单位）。
type CircuitSpec struct {
	Enabled        bool    `yaml:"enabled" json:"enabled"`
	MaxRequests    int     `yaml:"max_requests" json:"max_requests"`
	IntervalSec    int     `yaml:"interval_sec" json:"interval_sec"`
	TimeoutSec     int     `yaml:"timeout_sec" json:"timeout_sec"`
	ErrorThreshold float64 `yaml:"error_threshold" json:"error_threshold"`
}

// RateSpec 限流策略可序列化描述。
type RateSpec struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
	QPS     int  `yaml:"qps" json:"qps"`
	Burst   int  `yaml:"burst" json:"burst"`
}

// ToPolicy 将 PolicySpec 转换为运行时的 Policy（秒 -> Duration）。
func (s PolicySpec) ToPolicy() Policy {
	return Policy{
		Timeout: time.Duration(s.Timeout) * time.Second,
		Circuit: CircuitConfig{
			Enabled:        s.Circuit.Enabled,
			MaxRequests:    s.Circuit.MaxRequests,
			Interval:       time.Duration(s.Circuit.IntervalSec) * time.Second,
			Timeout:        time.Duration(s.Circuit.TimeoutSec) * time.Second,
			ErrorThreshold: s.Circuit.ErrorThreshold,
		},
		RateLimit: RateLimitConfig{
			Enabled: s.RateLimit.Enabled,
			QPS:     s.RateLimit.QPS,
			Burst:   s.RateLimit.Burst,
		},
	}
}

// ApplySpecs 将一组以 serviceKey 为键的策略规格转换为 Policy 并批量 SetPolicy，
// 供配置中心热更回调直接调用（即时生效）。
func ApplySpecs(specs map[string]PolicySpec) {
	for key, spec := range specs {
		SetPolicy(key, spec.ToPolicy())
	}
}
