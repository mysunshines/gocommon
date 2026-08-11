package middleware

import (
	"sync"

	"github.com/mysunshines/gocommon/config"

	"golang.org/x/time/rate"
)

// RateLimiter 基于令牌桶算法的限流器。
// 按 key（通常为客户端 IP）为每个客户端维护独立的令牌桶，首次访问时惰性创建。
type RateLimiter struct {
	// limiters 按 key（通常为客户端 IP）缓存各自的令牌桶限流器。
	// 每个 key 独立限流，首次访问时惰性创建（见 GetLimiter）。
	limiters map[string]*rate.Limiter
	// mu 保护对 limiters map 的并发读写（新建/查询限流器时会加锁）。
	mu sync.RWMutex
	// rps 新建限流器时使用的速率（每秒允许的平均请求数，rate.Limit 即 float64）。
	rps rate.Limit
	// burst 新建限流器时允许的突发容量（令牌桶可积攒的最大令牌数，即瞬时最大放行请求数）。
	burst int
}

func NewRateLimiter(rps int, burst int) *RateLimiter {
	return &RateLimiter{
		limiters: make(map[string]*rate.Limiter),
		rps:      rate.Limit(rps),
		burst:    burst,
	}
}

func (rl *RateLimiter) GetLimiter(key string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if limiter, ok := rl.limiters[key]; ok {
		return limiter
	}
	limiter := rate.NewLimiter(rl.rps, rl.burst)
	rl.limiters[key] = limiter
	return limiter
}

func (rl *RateLimiter) Allow(key string) bool {
	limiter := rl.GetLimiter(key)
	return limiter.Allow()
}

// SetLimit 动态更新限流阈值：既更新后续新建限流器的默认值，也即时刷新
// 所有已存在的限流器（已缓存的令牌桶），实现阈值热更无需重启。
func (rl *RateLimiter) SetLimit(rps int, burst int) {
	rl.mu.Lock()
	rl.rps = rate.Limit(rps)
	rl.burst = burst
	for _, limiter := range rl.limiters {
		limiter.SetLimit(rl.rps)
		limiter.SetBurst(rl.burst)
	}
	rl.mu.Unlock()
}

var (
	rateLimiter *RateLimiter
	limiterOnce sync.Once
)

func InitRateLimiter(cfg *config.RateLimitConfig) {
	limiterOnce.Do(func() {
		if cfg.Enabled {
			rateLimiter = NewRateLimiter(cfg.QPS, cfg.Burst)
		}
	})
}

// UpdateRateLimiter 动态调整已初始化限流器的阈值（供配置中心热更即时生效）。
// 仅在 InitRateLimiter 已初始化限流器后有效；若尚未初始化或配置关闭，则忽略。
func UpdateRateLimiter(cfg *config.RateLimitConfig) {
	if rateLimiter == nil {
		return
	}
	if !cfg.Enabled {
		return
	}
	rateLimiter.SetLimit(cfg.QPS, cfg.Burst)
}
