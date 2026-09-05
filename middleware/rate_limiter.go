package middleware

import (
	"strings"
	"sync"

	"github.com/mysunshines/gocommon/config"

	"golang.org/x/time/rate"
)

// RateLimiter 基于令牌桶算法的限流器。
// 按 key（未登录通常为客户端 IP，已登录为 userID）为每个客户端维护独立的令牌桶，
// 首次访问时惰性创建。支持路由级多规则：命中规则前缀时使用该规则独立的限流阈值，
// 否则回退到全局 QPS/Burst。
type RateLimiter struct {
	// limiters 按 key 缓存各自的令牌桶限流器（全局兜底规则）。
	limiters map[string]*rate.Limiter
	// mu 保护对 limiters / ruleLimiters 的并发读写。
	mu sync.RWMutex
	// rps 全局兜底速率（每秒允许的平均请求数）。
	rps rate.Limit
	// burst 全局兜底突发容量。
	burst int

	// rules 路由级规则；命中前缀时使用对应 ruleLimiters 中独立的限流器池。
	rules []config.RateLimitRule
	// ruleLimiters 与 rules 一一对应，每条规则一个独立的限流器池。
	ruleLimiters []*RateLimiter
}

// NewRateLimiter 创建仅含全局兜底阈值的限流器。
func NewRateLimiter(rps int, burst int) *RateLimiter {
	return &RateLimiter{
		limiters: make(map[string]*rate.Limiter),
		rps:      rate.Limit(rps),
		burst:    burst,
	}
}

// NewRateLimiterWithRules 创建带路由级规则的限流器。
func NewRateLimiterWithRules(rps int, burst int, rules []config.RateLimitRule) *RateLimiter {
	rl := &RateLimiter{
		limiters:     make(map[string]*rate.Limiter),
		rps:          rate.Limit(rps),
		burst:        burst,
		rules:        rules,
		ruleLimiters: make([]*RateLimiter, len(rules)),
	}
	for i := range rules {
		rl.ruleLimiters[i] = NewRateLimiter(rules[i].QPS, rules[i].Burst)
	}
	return rl
}

func (rl *RateLimiter) getLimiterLocked(key string) *rate.Limiter {
	if limiter, ok := rl.limiters[key]; ok {
		return limiter
	}
	limiter := rate.NewLimiter(rl.rps, rl.burst)
	rl.limiters[key] = limiter
	return limiter
}

// GetLimiter 返回指定 key 对应的令牌桶限流器（按需惰性创建）。
func (rl *RateLimiter) GetLimiter(key string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return rl.getLimiterLocked(key)
}

// Allow 判断 key 当前是否放行。path 用于匹配路由级规则（命中则用该规则阈值）。
func (rl *RateLimiter) Allow(path, key string) bool {
	rl.mu.RLock()
	// 路由级规则优先：命中第一条即采用其限流器池
	for i, rule := range rl.rules {
		for _, p := range rule.MatchPaths {
			if p != "" && strings.HasPrefix(path, p) {
				limiter := rl.ruleLimiters[i].GetLimiter(key)
				rl.mu.RUnlock()
				return limiter.Allow()
			}
		}
	}
	limiter := rl.getLimiterLocked(key)
	rl.mu.RUnlock()
	return limiter.Allow()
}

// SetLimit 动态更新限流阈值：刷新全局默认值与所有已存在的全局令牌桶，
// 同时重建各规则限流器（规则集合视为配置，变更即重建）。
func (rl *RateLimiter) SetLimit(rps int, burst int, rules []config.RateLimitRule) {
	rl.mu.Lock()
	rl.rps = rate.Limit(rps)
	rl.burst = burst
	for _, limiter := range rl.limiters {
		limiter.SetLimit(rl.rps)
		limiter.SetBurst(rl.burst)
	}
	rl.rules = rules
	rl.ruleLimiters = make([]*RateLimiter, len(rules))
	for i := range rules {
		rl.ruleLimiters[i] = NewRateLimiter(rules[i].QPS, rules[i].Burst)
	}
	rl.mu.Unlock()
}

var (
	rateLimiter *RateLimiter
	limiterOnce sync.Once
)

// InitRateLimiter 按配置初始化全局限流器；未启用或已初始化则跳过。
func InitRateLimiter(cfg *config.RateLimitConfig) {
	limiterOnce.Do(func() {
		if cfg.Enabled {
			rateLimiter = NewRateLimiterWithRules(cfg.QPS, cfg.Burst, cfg.Rules)
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
	rateLimiter.SetLimit(cfg.QPS, cfg.Burst, cfg.Rules)
}
