package cache

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/mysunshines/gocommon/config"
	"github.com/mysunshines/gocommon/log"

	"github.com/go-redis/redis/v8"
	"github.com/twmb/murmur3"
	"golang.org/x/sync/singleflight"
)

const (
	// cacheInvalidateChannel Redis pub/sub 频道名，用于多实例本地缓存失效通知
	cacheInvalidateChannel = "cache:local:invalidate"
)

var (
	rdb        *redis.Client
	initOnce   sync.Once
	sfGroup    singleflight.Group
	// keyPrefix 在 Init 时从配置写入，避免 GetKey 依赖 gocommon 全局配置
	// （各服务用各自的 internal/config 加载，gocommon 的 config.Get() 可能为 nil）
	keyPrefix  string
	// 多实例安全：TTL 从 10 分钟缩短到 30 秒，配合 Redis pub/sub 主动失效
	localCache = NewLocalCache(1000, 30*time.Second)
)

type RedisConfig struct {
	Host      string
	Port      int
	Password  string
	DB        int
	PoolSize  int
	KeyPrefix string
}

func Init(cfg *config.RedisConfig) error {
	var initErr error
	initOnce.Do(func() {
		keyPrefix = cfg.KeyPrefix
		rdb = redis.NewClient(&redis.Options{
			Addr:     cfg.Addr(),
			Password: cfg.Password,
			DB:       cfg.DB,
			PoolSize: cfg.PoolSize,
		})

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := rdb.Ping(ctx).Err(); err != nil {
			initErr = fmt.Errorf("failed to connect redis: %w", err)
			return
		}

		// 启动跨实例本地缓存失效监听
		go startCacheInvalidationListener()

		log.Info("Redis initialized successfully")
	})
	return initErr
}

func GetClient() *redis.Client {
	return rdb
}

// Ping 检查 Redis 连接是否正常，用于健康检查
func Ping(ctx context.Context) error {
	if rdb == nil {
		return nil
	}
	return rdb.Ping(ctx).Err()
}

func GetKey(key string) string {
	return keyPrefix + key
}

// redisReady 返回 Redis 是否已成功初始化，避免未初始化时调用导致空指针 panic。
func redisReady() bool {
	return rdb != nil
}

func Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	if !redisReady() {
		return fmt.Errorf("redis not initialized")
	}
	return rdb.Set(ctx, GetKey(key), value, expiration).Err()
}

// SetNX 仅当 key 不存在时设置值，返回 true 表示设置成功
func SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) (bool, error) {
	if !redisReady() {
		return false, fmt.Errorf("redis not initialized")
	}
	return rdb.SetNX(ctx, GetKey(key), value, expiration).Result()
}

func Get(ctx context.Context, key string) (string, error) {
	if !redisReady() {
		return "", fmt.Errorf("redis not initialized")
	}
	return rdb.Get(ctx, GetKey(key)).Result()
}

func GetBytes(ctx context.Context, key string) ([]byte, error) {
	return rdb.Get(ctx, GetKey(key)).Bytes()
}

func Delete(ctx context.Context, keys ...string) error {
	realKeys := make([]string, len(keys))
	for i, k := range keys {
		realKeys[i] = GetKey(k)
	}
	return rdb.Del(ctx, realKeys...).Err()
}

func Exists(ctx context.Context, key string) (bool, error) {
	if !redisReady() {
		return false, fmt.Errorf("redis not initialized")
	}
	n, err := rdb.Exists(ctx, GetKey(key)).Result()
	return n > 0, err
}

func Incr(ctx context.Context, key string) (int64, error) {
	return rdb.Incr(ctx, GetKey(key)).Result()
}

func Expire(ctx context.Context, key string, expiration time.Duration) error {
	return rdb.Expire(ctx, GetKey(key), expiration).Err()
}

func TTL(ctx context.Context, key string) (time.Duration, error) {
	return rdb.TTL(ctx, GetKey(key)).Result()
}

// IncrBy 指定步长自增
func IncrBy(ctx context.Context, key string, value int64) (int64, error) {
	return rdb.IncrBy(ctx, GetKey(key), value).Result()
}

func Decr(ctx context.Context, key string) (int64, error) {
	return rdb.Decr(ctx, GetKey(key)).Result()
}

// MGet 批量获取
func MGet(ctx context.Context, keys ...string) ([]interface{}, error) {
	realKeys := make([]string, len(keys))
	for i, k := range keys {
		realKeys[i] = GetKey(k)
	}
	return rdb.MGet(ctx, realKeys...).Result()
}

// MSet 批量设置
func MSet(ctx context.Context, values ...interface{}) error {
	return rdb.MSet(ctx, values...).Err()
}

// ---- List 命令 ----

func LPush(ctx context.Context, key string, values ...interface{}) (int64, error) {
	return rdb.LPush(ctx, GetKey(key), values...).Result()
}

func RPush(ctx context.Context, key string, values ...interface{}) (int64, error) {
	return rdb.RPush(ctx, GetKey(key), values...).Result()
}

func LPop(ctx context.Context, key string) (string, error) {
	return rdb.LPop(ctx, GetKey(key)).Result()
}

func RPop(ctx context.Context, key string) (string, error) {
	return rdb.RPop(ctx, GetKey(key)).Result()
}

func LRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	return rdb.LRange(ctx, GetKey(key), start, stop).Result()
}

func LLen(ctx context.Context, key string) (int64, error) {
	return rdb.LLen(ctx, GetKey(key)).Result()
}

// LTrim 裁剪列表只保留 [start, stop] 范围
func LTrim(ctx context.Context, key string, start, stop int64) error {
	return rdb.LTrim(ctx, GetKey(key), start, stop).Err()
}

// ---- Hash 命令 ----

func HSet(ctx context.Context, key string, values ...interface{}) (int64, error) {
	return rdb.HSet(ctx, GetKey(key), values...).Result()
}

func HGet(ctx context.Context, key, field string) (string, error) {
	return rdb.HGet(ctx, GetKey(key), field).Result()
}

func HGetAll(ctx context.Context, key string) (map[string]string, error) {
	return rdb.HGetAll(ctx, GetKey(key)).Result()
}

func HDel(ctx context.Context, key string, fields ...string) (int64, error) {
	return rdb.HDel(ctx, GetKey(key), fields...).Result()
}

func HExists(ctx context.Context, key, field string) (bool, error) {
	return rdb.HExists(ctx, GetKey(key), field).Result()
}

func HIncrBy(ctx context.Context, key, field string, incr int64) (int64, error) {
	return rdb.HIncrBy(ctx, GetKey(key), field, incr).Result()
}

// ---- Set 命令 ----

func SAdd(ctx context.Context, key string, members ...interface{}) (int64, error) {
	return rdb.SAdd(ctx, GetKey(key), members...).Result()
}

func SRem(ctx context.Context, key string, members ...interface{}) (int64, error) {
	return rdb.SRem(ctx, GetKey(key), members...).Result()
}

func SIsMember(ctx context.Context, key string, member interface{}) (bool, error) {
	return rdb.SIsMember(ctx, GetKey(key), member).Result()
}

func SMembers(ctx context.Context, key string) ([]string, error) {
	return rdb.SMembers(ctx, GetKey(key)).Result()
}

func SCard(ctx context.Context, key string) (int64, error) {
	return rdb.SCard(ctx, GetKey(key)).Result()
}

// ---- Sorted Set 命令 ----

type Z = redis.Z

func ZAdd(ctx context.Context, key string, members ...*redis.Z) (int64, error) {
	return rdb.ZAdd(ctx, GetKey(key), members...).Result()
}

func ZRem(ctx context.Context, key string, members ...interface{}) (int64, error) {
	return rdb.ZRem(ctx, GetKey(key), members...).Result()
}

func ZRemRangeByScore(ctx context.Context, key string, min, max string) (int64, error) {
	return rdb.ZRemRangeByScore(ctx, GetKey(key), min, max).Result()
}

func ZRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	return rdb.ZRange(ctx, GetKey(key), start, stop).Result()
}

func ZRangeWithScores(ctx context.Context, key string, start, stop int64) ([]redis.Z, error) {
	return rdb.ZRangeWithScores(ctx, GetKey(key), start, stop).Result()
}

func ZRevRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	return rdb.ZRevRange(ctx, GetKey(key), start, stop).Result()
}

func ZRevRangeWithScores(ctx context.Context, key string, start, stop int64) ([]redis.Z, error) {
	return rdb.ZRevRangeWithScores(ctx, GetKey(key), start, stop).Result()
}

func ZCard(ctx context.Context, key string) (int64, error) {
	return rdb.ZCard(ctx, GetKey(key)).Result()
}

// TryLock 尝试获取分布式锁，成功返回 true
// 用于多实例场景下保证只有一个实例执行关键操作（如数据库迁移）
func TryLock(ctx context.Context, lockKey string, instanceID string, ttl time.Duration) (bool, error) {
	return SetNX(ctx, lockKey, instanceID, ttl)
}

// Unlock 释放分布式锁（通过 Lua 脚本确保原子性：只删除自己持有的锁）
func Unlock(ctx context.Context, lockKey string, instanceID string) error {
	if !redisReady() {
		return fmt.Errorf("redis not initialized")
	}
	script := `
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("DEL", KEYS[1])
		else
			return 0
		end`
	return rdb.Eval(ctx, script, []string{GetKey(lockKey)}, instanceID).Err()
}

// RenewLock 续期锁（延长 TTL），防止长时间操作导致锁过期
func RenewLock(ctx context.Context, lockKey string, instanceID string, ttl time.Duration) error {
	script := `
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("EXPIRE", KEYS[1], ARGV[2])
		else
			return 0
		end`
	return rdb.Eval(ctx, script, []string{GetKey(lockKey)}, instanceID, int(ttl.Seconds())).Err()
}

func Close() error {
	if rdb != nil {
		return rdb.Close()
	}
	return nil
}

// PublishCacheInvalidation 向所有实例广播本地缓存失效通知
// 当某实例更新了 Redis 并回写本地缓存后调用，通知其他实例清除对应 key
func PublishCacheInvalidation(ctx context.Context, key string) {
	if rdb == nil {
		return
	}
	// 使用带 KeyPrefix 的频道名，避免不同项目冲突
	channel := GetKey(cacheInvalidateChannel)
	if err := rdb.Publish(ctx, channel, key).Err(); err != nil {
		log.Debugf("Failed to publish cache invalidation: %v", err)
	}
}

// startCacheInvalidationListener 订阅本地缓存失效频道，收到消息后删除本地缓存
// 在 Init() 中通过 goroutine 启动，运行期间持续监听
func startCacheInvalidationListener() {
	// 每个实例使用独立的消费者组，确保所有实例都能收到消息
	channel := GetKey(cacheInvalidateChannel)
	pubsub := rdb.Subscribe(context.Background(), channel)
	defer pubsub.Close()

	ch := pubsub.Channel()
	for msg := range ch {
		if msg != nil && msg.Payload != "" {
			localCache.Delete(msg.Payload)
		}
	}
}

type BloomFilter struct {
	key    string
	size   uint64
	hashes uint64
}

func NewBloomFilter(key string, size, hashes uint64) *BloomFilter {
	return &BloomFilter{
		key:    GetKey(key),
		size:   size,
		hashes: hashes,
	}
}

func (bf *BloomFilter) Add(ctx context.Context, value string) error {
	pipe := rdb.Pipeline()
	// 使用 SeedSum64 配合组合种子值
	fp := murmur3.SeedSum64(0xcc9e2d51^0x1b873593, []byte(value))
	for i := uint64(0); i < bf.hashes; i++ {
		pipe.SetBit(ctx, bf.key, int64((fp+uint64(i)*0x5bd1e995)%bf.size), 1)
	}
	_, err := pipe.Exec(ctx)
	return err
}

func (bf *BloomFilter) Exists(ctx context.Context, value string) (bool, error) {
	fp := murmur3.SeedSum64(0xcc9e2d51^0x1b873593, []byte(value))
	for i := uint64(0); i < bf.hashes; i++ {
		bit, err := rdb.GetBit(ctx, bf.key, int64((fp+uint64(i)*0x5bd1e995)%bf.size)).Result()
		if err != nil {
			return false, err
		}
		if bit == 0 {
			return false, nil
		}
	}
	return true, nil
}

func SingleFlightDo(key string, fn func() (interface{}, error)) (interface{}, error) {
	v, err, _ := sfGroup.Do(key, fn)
	return v, err
}

func SingleFlightDoChan(key string, fn func() (interface{}, error)) <-chan singleflight.Result {
	ch := sfGroup.DoChan(key, fn)
	return ch
}

type LocalCache struct {
	data      map[string]*cacheItem
	mu        sync.RWMutex
	maxSize   int
	expire    time.Duration
	cleanupCh chan struct{}
}

type cacheItem struct {
	value      interface{}
	expireTime time.Time
}

func NewLocalCache(maxSize int, expire time.Duration) *LocalCache {
	lc := &LocalCache{
		data:      make(map[string]*cacheItem),
		maxSize:   maxSize,
		expire:    expire,
		cleanupCh: make(chan struct{}),
	}
	go lc.cleanup()
	return lc
}

func (lc *LocalCache) cleanup() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			lc.mu.Lock()
			now := time.Now()
			for k, v := range lc.data {
				if now.After(v.expireTime) {
					delete(lc.data, k)
				}
			}
			lc.mu.Unlock()
		case <-lc.cleanupCh:
			return
		}
	}
}

// LocalCacheGet 获取本地缓存
func LocalCacheGet(key string) (interface{}, bool) {
	return localCache.Get(key)
}

// LocalCacheSet 设置本地缓存，并广播失效通知给其他实例
func LocalCacheSet(key string, value interface{}) {
	localCache.Set(key, value)
	// 通知其他实例清除该 key 的本地缓存，保证多实例数据一致性
	PublishCacheInvalidation(context.Background(), key)
}

// LocalCacheDelete 删除本地缓存，并广播失效通知给其他实例
func LocalCacheDelete(key string) {
	localCache.Delete(key)
	// 通知其他实例清除该 key 的本地缓存，保证多实例数据一致性
	PublishCacheInvalidation(context.Background(), key)
}

func (lc *LocalCache) Get(key string) (interface{}, bool) {
	lc.mu.RLock()
	defer lc.mu.RUnlock()
	if item, ok := lc.data[key]; ok {
		if time.Now().Before(item.expireTime) {
			return item.value, true
		}
	}
	return nil, false
}

func (lc *LocalCache) Set(key string, value interface{}) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	if len(lc.data) >= lc.maxSize {
		for k := range lc.data {
			delete(lc.data, k)
			break
		}
	}
	lc.data[key] = &cacheItem{
		value:      value,
		expireTime: time.Now().Add(lc.expire),
	}
}

func (lc *LocalCache) Delete(key string) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	delete(lc.data, key)
}

func (lc *LocalCache) Clear() {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.data = make(map[string]*cacheItem)
}
