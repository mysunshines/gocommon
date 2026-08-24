package cache

import (
	"fmt"
	"testing"
	"time"
)

func TestSimpleGlob(t *testing.T) {
	cases := []struct {
		pattern, s string
		want       bool
	}{
		{"article:*", "article:42", true},
		{"article:*", "article:42:3", true},
		{"article:*", "user:42", false},
		{"*", "anything", true},
		{"verify_code:*", "verify_code:abc", true},
		{"verify_code:*", "verify_code", false}, // 模式中 ':' 需匹配，'*' 只匹配其后的空串
		{"a*b*c", "aXbYc", true},
		{"a*b*c", "aXbY", false},
		{"exact", "exact", true},
		{"exact", "exactx", false},
		{"*:200", "article:200", true},
		{"*:200", "article:200:1", false}, // 后缀必须完全匹配
	}
	for _, c := range cases {
		if got := simpleGlob(c.pattern, c.s); got != c.want {
			t.Errorf("simpleGlob(%q, %q) = %v, want %v", c.pattern, c.s, got, c.want)
		}
	}
}

func TestResolveTTL(t *testing.T) {
	defer SetPolicy(nil) // 清理全局策略，避免污染其他测试

	// 无策略：返回默认
	if d := ResolveTTL("article:1", 5*time.Minute); d != 5*time.Minute {
		t.Fatalf("no policy: got %v, want 5m", d)
	}

	SetPolicy(&PolicyConfig{
		RedisTTL: map[string]int64{
			"verify_code:*": 300,
			"article:*":     3600,
			"article:42":    7200,
		},
	})

	// 通配符命中
	if d := ResolveTTL("article:7", time.Minute); d != time.Hour {
		t.Fatalf("wildcard: got %v, want 1h", d)
	}
	if d := ResolveTTL("verify_code:abc", 60*time.Second); d != 5*time.Minute {
		t.Fatalf("wildcard2: got %v, want 5m", d)
	}
	// 精确优先（更长模式）
	if d := ResolveTTL("article:42", time.Minute); d != 2*time.Hour {
		t.Fatalf("exact priority: got %v, want 2h", d)
	}
	// 未命中：返回默认
	if d := ResolveTTL("user:1", 90*time.Second); d != 90*time.Second {
		t.Fatalf("no match: got %v, want 90s", d)
	}

	// 清空策略后恢复默认
	SetPolicy(nil)
	if d := ResolveTTL("article:42", time.Minute); d != time.Minute {
		t.Fatalf("after clear: got %v, want 1m", d)
	}
}

func TestHotKeyPolicyFor(t *testing.T) {
	defer SetPolicy(nil)
	SetPolicy(&PolicyConfig{
		HotKeys: map[string]HotKeyPolicy{
			"article:*": {LocalCacheTTL: 10},
			"user:1":    {ShardCount: 4},
		},
	})
	if p, ok := hotKeyPolicyFor("article:9"); !ok || p.LocalCacheTTL != 10 {
		t.Fatalf("wildcard hot policy: %+v ok=%v", p, ok)
	}
	if p, ok := hotKeyPolicyFor("user:1"); !ok || p.ShardCount != 4 {
		t.Fatalf("exact hot policy: %+v ok=%v", p, ok)
	}
	if _, ok := hotKeyPolicyFor("user:2"); ok {
		t.Fatal("user:2 should not match any policy")
	}
}

func TestShardKeys(t *testing.T) {
	defer SetPolicy(nil)

	// 无策略：原 key 不变
	if ks := shardKeys("article:42"); len(ks) != 1 || ks[0] != "article:42" {
		t.Fatalf("no policy shards: %v", ks)
	}

	SetPolicy(&PolicyConfig{
		HotKeys: map[string]HotKeyPolicy{
			"article:42": {ShardCount: 8},
			"user:*":     {LocalCacheTTL: 30},
		},
	})

	// 分片：N 个子 key
	ks := shardKeys("article:42")
	if len(ks) != 8 {
		t.Fatalf("want 8 shards, got %d", len(ks))
	}
	for i, k := range ks {
		if want := fmt.Sprintf("article:42:%d", i); k != want {
			t.Errorf("shard %d = %q, want %q", i, k, want)
		}
	}

	// 分片数上限保护
	SetPolicy(&PolicyConfig{HotKeys: map[string]HotKeyPolicy{"x": {ShardCount: 999}}})
	if ks := shardKeys("x"); len(ks) != maxShardCount {
		t.Fatalf("cap shards: got %d, want %d", len(ks), maxShardCount)
	}

	// 仅本地缓存策略不产生分片
	SetPolicy(&PolicyConfig{HotKeys: map[string]HotKeyPolicy{"user:1": {LocalCacheTTL: 30}}})
	if ks := shardKeys("user:1"); len(ks) != 1 || ks[0] != "user:1" {
		t.Fatalf("local-only shards: %v", ks)
	}
}
