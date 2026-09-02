package cache

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/go-redis/redis/v8"

	"github.com/mysunshines/gocommon/metrics"
)

// maxShardCount 分片数上限，防止误配导致分片 key 数量爆炸。
const maxShardCount = 64

// shardKeys 返回 key 在 Redis 中的实际读写键集合。
// 命中热点策略且 ShardCount>1 时拆分为 key:0 ~ key:N-1 共 N 个分片，
// 否则保持原 key 不变。
func shardKeys(key string) []string {
	p, ok := hotKeyPolicyFor(key)
	if !ok || p.ShardCount <= 1 {
		return []string{key}
	}
	n := p.ShardCount
	if n > maxShardCount {
		n = maxShardCount
	}
	keys := make([]string, n)
	for i := 0; i < n; i++ {
		keys[i] = fmt.Sprintf("%s:%d", key, i)
	}
	return keys
}

// pickShard 随机选择一个分片用于读取。分片由 SetSmart/DeleteSmart/ExpireSmart
// 全量写入/删除，任意分片都持有相同的数据，随机读即可把单 key QPS 均摊到 N 个分片。
func pickShard(keys []string) string {
	return keys[rand.Intn(len(keys))]
}

// GetSmart 热点感知读取：
//   - 命中本地缓存策略（LocalCacheTTL>0）时先查进程内本地缓存，miss 回源 Redis 并回填；
//   - 命中分片策略（ShardCount>1）时随机读一个分片；
//   - 未命中任何策略时行为与 Get 完全一致。
func GetSmart(ctx context.Context, key string) (string, error) {
	p, hot := hotKeyPolicyFor(key)
	if hot && p.LocalCacheTTL > 0 {
		if v, ok := LocalCacheGet(key); ok {
			if s, ok := v.(string); ok {
				return s, nil
			}
		}
	}
	var (
		val string
		err error
	)
	if hot && p.ShardCount > 1 {
		if !redisReady() {
			return "", fmt.Errorf("redis not initialized")
		}
		val, err = rdb.Get(ctx, GetKey(pickShard(shardKeys(key)))).Result()
		metrics.RecordRedisHit(err == nil)
		if err != nil && err != redis.Nil {
			logRedisOp(ctx, "GET(shard)", key, err)
		} else {
			logRedisOp(ctx, "GET(shard)", key, nil)
		}
	} else {
		val, err = Get(ctx, key)
	}
	if err == nil && hot && p.LocalCacheTTL > 0 {
		LocalCacheSetWithTTL(key, val, time.Duration(p.LocalCacheTTL)*time.Second)
	}
	return val, err
}

// GetBytesSmart 同 GetSmart，返回原始字节（适合序列化对象场景）。
func GetBytesSmart(ctx context.Context, key string) ([]byte, error) {
	p, hot := hotKeyPolicyFor(key)
	if hot && p.LocalCacheTTL > 0 {
		if v, ok := LocalCacheGet(key); ok {
			switch b := v.(type) {
			case []byte:
				return b, nil
			case string:
				return []byte(b), nil
			}
		}
	}
	var (
		b   []byte
		err error
	)
	if hot && p.ShardCount > 1 {
		if !redisReady() {
			return nil, fmt.Errorf("redis not initialized")
		}
		b, err = rdb.Get(ctx, GetKey(pickShard(shardKeys(key)))).Bytes()
		metrics.RecordRedisHit(err == nil)
		if err != nil && err != redis.Nil {
			logRedisOp(ctx, "GET(shard)", key, err)
		} else {
			logRedisOp(ctx, "GET(shard)", key, nil)
		}
	} else {
		b, err = GetBytes(ctx, key)
	}
	if err == nil && hot && p.LocalCacheTTL > 0 {
		LocalCacheSetWithTTL(key, b, time.Duration(p.LocalCacheTTL)*time.Second)
	}
	return b, err
}

// SetSmart 热点感知写入：
//   - 命中分片策略时写入所有分片（保证任意分片可读，写量远低于读量可接受）；
//   - TTL 由配置中心 RedisTTL 动态覆盖（未命中配置则用 defaultTTL）；
//   - 命中本地缓存策略时同步更新本实例本地缓存并广播失效通知，
//     保证多实例最终一致（其他实例收到 pub/sub 后清除旧本地值）。
func SetSmart(ctx context.Context, key string, value interface{}, defaultTTL time.Duration) error {
	if !redisReady() {
		return fmt.Errorf("redis not initialized")
	}
	p, hot := hotKeyPolicyFor(key)
	ttl := ResolveTTL(key, defaultTTL)
	if hot && p.ShardCount > 1 {
		keys := shardKeys(key)
		pipe := rdb.Pipeline()
		for _, k := range keys {
			pipe.Set(ctx, GetKey(k), value, ttl)
		}
		_, err := pipe.Exec(ctx)
		logRedisOp(ctx, "SET(shard)", key, err)
		if err != nil {
			return err
		}
	} else {
		if err := Set(ctx, key, value, ttl); err != nil {
			return err
		}
	}
	if hot && p.LocalCacheTTL > 0 {
		LocalCacheSetWithTTL(key, value, time.Duration(p.LocalCacheTTL)*time.Second)
		PublishCacheInvalidation(ctx, key)
	}
	return nil
}

// DeleteSmart 热点感知删除：删除全部分片 + 本地缓存（含跨实例失效广播）。
func DeleteSmart(ctx context.Context, key string) error {
	if !redisReady() {
		return fmt.Errorf("redis not initialized")
	}
	keys := shardKeys(key)
	realKeys := make([]string, len(keys))
	for i, k := range keys {
		realKeys[i] = GetKey(k)
	}
	err := rdb.Del(ctx, realKeys...).Err()
	logRedisOp(ctx, "DEL(shard)", key, err)
	LocalCacheDelete(key)
	return err
}

// ExistsSmart 热点感知存在性检查：分片策略下随机读一个分片即可
//（写路径保证全分片数据一致）。
func ExistsSmart(ctx context.Context, key string) (bool, error) {
	p, hot := hotKeyPolicyFor(key)
	if hot && p.ShardCount > 1 {
		if !redisReady() {
			return false, fmt.Errorf("redis not initialized")
		}
		n, err := rdb.Exists(ctx, GetKey(pickShard(shardKeys(key)))).Result()
		logRedisOp(ctx, "EXISTS(shard)", key, err)
		return n > 0, err
	}
	return Exists(ctx, key)
}

// ExpireSmart 热点感知过期：分片策略下对所有分片设置 TTL（必须全设，
// 否则部分分片提前过期导致读取不一致）；TTL 由配置中心动态覆盖。
func ExpireSmart(ctx context.Context, key string, defaultTTL time.Duration) error {
	if !redisReady() {
		return fmt.Errorf("redis not initialized")
	}
	p, hot := hotKeyPolicyFor(key)
	ttl := ResolveTTL(key, defaultTTL)
	if hot && p.ShardCount > 1 {
		keys := shardKeys(key)
		pipe := rdb.Pipeline()
		for _, k := range keys {
			pipe.Expire(ctx, GetKey(k), ttl)
		}
		_, err := pipe.Exec(ctx)
		logRedisOp(ctx, "EXPIRE(shard)", key, err)
		return err
	}
	return Expire(ctx, key, ttl)
}
