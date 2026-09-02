package cache

import (
	"reflect"
	"sync/atomic"
	"time"
)

// HotKeyPolicy 描述某个 key（支持 * 通配符）的热点缓存策略。
// 通过 Consul 配置中心（HotConfig.Cache.HotKeys）热更下发，即时生效，无需发版。
type HotKeyPolicy struct {
	// ShardCount 分片数。>1 时将该 key 在 Redis 中拆分为 N 个子 key
	//（key:0 ~ key:N-1），写入全量、读取随机命中一个分片，
	// 从而把单 key 的 QPS 均摊到 N 个分片（应对 Redis 单 key 热点）。
	ShardCount int `yaml:"shard_count" json:"shard_count"`
	// LocalCacheTTL 本地缓存 TTL（秒）。>0 时读取先查进程内本地缓存，
	// miss 再回源 Redis 并回填，把热点读从 Redis 卸载到本实例内存。
	LocalCacheTTL int `yaml:"local_cache_ttl" json:"local_cache_ttl"`
}

// PolicyConfig 是从配置中心热更下发的缓存策略聚合。
// 通过 configcenter.HotConfig.Cache 下发，并在配置变更时自动同步到本包
//（见 configcenter.apply 中的 cache.SetPolicy）。
type PolicyConfig struct {
	// RedisTTL 按 key 模式（支持 * 通配符）动态覆盖写入 TTL（秒）。
	// 例如 "verify_code:*": 300 会使 Set(ctx, "verify_code:1", ...) 使用 300s
	// 而非调用方传入的默认 TTL。多个模式命中时取模式字符串最长者（精确优先）。
	RedisTTL map[string]int64 `yaml:"redis_ttl" json:"redis_ttl"`
	// HotKeys 热点 key 策略，key 为模式（支持 * 通配符）。
	HotKeys map[string]HotKeyPolicy `yaml:"hot_keys" json:"hot_keys"`
}

// 全局缓存策略快照，由 SetPolicy 原子更新，并发读取无锁。
var cachePolicy atomic.Value // *PolicyConfig

// SetPolicy 应用一份新的缓存策略（通常来自配置中心热更）。
// 幂等：仅当热点 key 策略集合发生变化时才清空本地缓存，
// 避免旧策略 LocalCacheTTL 残留的脏值继续被命中。
// 传入 nil 等价于清空策略（&PolicyConfig{}）。
func SetPolicy(p *PolicyConfig) {
	if p == nil {
		p = &PolicyConfig{}
	}
	old, _ := cachePolicy.Load().(*PolicyConfig)
	cachePolicy.Store(p)
	if old == nil || !reflect.DeepEqual(old.HotKeys, p.HotKeys) {
		localCache.Clear()
	}
}

// ResolveTTL 解析某个 key 的最终 TTL：若配置中心 RedisTTL 中命中该 key
//（含通配符，精确模式优先），返回配置值；否则返回 defaultTTL。
// Set / SetNX / Expire 等写操作内部均调用此函数，实现 TTL 的动态配置。
func ResolveTTL(key string, defaultTTL time.Duration) time.Duration {
	p, _ := cachePolicy.Load().(*PolicyConfig)
	if p == nil || len(p.RedisTTL) == 0 {
		return defaultTTL
	}
	if secs, ok := matchPatterns(p.RedisTTL, key); ok {
		return time.Duration(secs) * time.Second
	}
	return defaultTTL
}

// hotKeyPolicyFor 返回 key 命中的热点策略（通配符匹配，最长模式优先）。
func hotKeyPolicyFor(key string) (HotKeyPolicy, bool) {
	p, _ := cachePolicy.Load().(*PolicyConfig)
	if p == nil || len(p.HotKeys) == 0 {
		return HotKeyPolicy{}, false
	}
	return matchPatterns(p.HotKeys, key)
}

// matchPatterns 在 patterns（key 模式 → value）中查找匹配 key 的条目并返回其 value。
// 多个模式同时命中时取模式字符串最长者（更精确的优先，如 "article:42" 优先于 "article:*"）。
func matchPatterns[T any](patterns map[string]T, key string) (T, bool) {
	var best T
	var bestPattern string
	found := false
	for pat, v := range patterns {
		if simpleGlob(pat, key) {
			if !found || len(pat) > len(bestPattern) {
				bestPattern = pat
				best = v
				found = true
			}
		}
	}
	return best, found
}

// simpleGlob 仅支持 '*' 通配符（匹配任意字符序列，含空串）的匹配。
// 自实现避免 filepath/path 对 '/' 的特殊语义（Redis key 中 '/' 是普通字符）。
func simpleGlob(pattern, s string) bool {
	for len(pattern) > 0 {
		if pattern[0] == '*' {
			for len(pattern) > 0 && pattern[0] == '*' {
				pattern = pattern[1:]
			}
			if len(pattern) == 0 {
				return true
			}
			for i := 0; i <= len(s); i++ {
				if simpleGlob(pattern, s[i:]) {
					return true
				}
			}
			return false
		}
		if len(s) == 0 || pattern[0] != s[0] {
			return false
		}
		pattern = pattern[1:]
		s = s[1:]
	}
	return len(s) == 0
}
