package cache

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/mysunshines/gocommon/config"
	"github.com/mysunshines/gocommon/constants"
	"github.com/mysunshines/gocommon/log"
	"github.com/mysunshines/gocommon/metrics"
	"github.com/mysunshines/gocommon/middleware"

	"github.com/go-redis/redis/v8"
	"github.com/sirupsen/logrus"
	"github.com/twmb/murmur3"
	"golang.org/x/sync/singleflight"
)

const (
	// cacheInvalidateChannel Redis pub/sub 频道名，用于多实例本地缓存失效通知
	cacheInvalidateChannel = "cache:local:invalidate"
)

var (
	rdb      *redis.Client
	initOnce sync.Once
	sfGroup  singleflight.Group
	// keyPrefix 在 Init 时从配置写入，避免 GetKey 依赖 gocommon 全局配置
	// （各服务用各自的 internal/config 加载，gocommon 的 config.Get() 可能为 nil）
	keyPrefix string
	// 多实例安全：TTL 从 10 分钟缩短到 30 秒，配合 Redis pub/sub 主动失效
	localCache = NewLocalCache(1000, 30*time.Second)
)

// RedisConfig 定义 Redis 客户端初始化所需的连接配置（地址、密码、库号、连接池、键前缀）。
type RedisConfig struct {
	Host      string // Redis 主机
	Port      int    // Redis 端口
	Password  string // 密码（无则空）
	DB        int    // 逻辑库编号
	PoolSize  int    // 连接池大小
	KeyPrefix string // 键前缀（避免多服务键冲突）
}

// Init 初始化全局 Redis 客户端（仅执行一次），校验连通性并启动跨实例缓存失效监听。
func Init(cfg *config.RedisConfig) error {
	var initErr error
	initOnce.Do(func() {
		keyPrefix = cfg.KeyPrefix
		// 显式设置短超时并禁用自动重试，实现 fail fast：
		// go-redis 默认 DialTimeout=5s/ReadTimeout=3s/WriteTimeout=3s 且 MaxRetries=3，
		// Redis 故障时单次操作最长可空等 10s+，会拖垮依赖链路。
		// 改为 1s 超时 + 不重试：正常内网延迟毫秒级无影响，故障时快速失败由上层降级。
		rdb = redis.NewClient(&redis.Options{
			Addr:         cfg.Addr(),
			Password:     cfg.Password,
			DB:           cfg.DB,
			PoolSize:     cfg.PoolSize,
			DialTimeout:  1 * time.Second,
			ReadTimeout:  1 * time.Second,
			WriteTimeout: 1 * time.Second,
			MaxRetries:   -1, // 禁用自动重试
		})

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := rdb.Ping(ctx).Err(); err != nil {
			initErr = fmt.Errorf("failed to connect redis: %w", err)
			return
		}

		// 启动跨实例本地缓存失效监听
		go startCacheInvalidationListener()

		// 明确标记多实例缓存失效通道已就绪：这是「多实例状态一致性」的关键保障。
		// LocalCacheDelete 会通过 Redis pub/sub 广播失效，其他实例监听后删除本地旧值，
		// 从而保证多实例最终一致（配合 localCache 30s TTL 兜底）。
		log.Info("Redis initialized; multi-instance local-cache invalidation (pub/sub) is ACTIVE")
	})
	return initErr
}

// InvalidationActive 返回跨实例缓存失效广播是否就绪（Redis 已初始化即视为就绪）。
// 可供 /health 等健康检查在响应中声明，便于运维确认多实例一致性保障已生效。
func InvalidationActive() bool {
	return redisReady()
}

// GetClient 返回已初始化的全局 Redis 客户端实例。
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

// GetKey 返回拼接全局键前缀后的完整 Redis 键名。
func GetKey(key string) string {
	return keyPrefix + key
}

// redisReady 返回 Redis 是否已成功初始化，避免未初始化时调用导致空指针 panic。
func redisReady() bool {
	return rdb != nil
}

// logRedisOp 记录 Redis 操作日志，成功时以 Debug 级别输出避免噪音，
// 失败时以 Error 级别输出。traceID 以独立字段输出，便于日志系统按 traceID 检索串联。
func logRedisOp(ctx context.Context, op, key string, err error) {
	traceID := middleware.GetTraceIDFromContext(ctx)
	if err != nil {
		log.WithFields(logrus.Fields{
			constants.LogFieldTraceID: traceID,
			"op":                      op,
			"key":                     key,
			"err":                     err.Error(),
		}).Errorf("[Redis] op failed")
	} else {
		log.WithFields(logrus.Fields{
			constants.LogFieldTraceID: traceID,
			"op":                      op,
			"key":                     key,
		}).Debugf("[Redis] op")
	}
}

// Set 写入缓存。expiration 为默认 TTL：若配置中心 RedisTTL 中配置了该 key 的
// 动态 TTL，则优先使用配置值（见 ResolveTTL）。
func Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	if !redisReady() {
		return fmt.Errorf("redis not initialized")
	}
	expiration = ResolveTTL(key, expiration)
	err := rdb.Set(ctx, GetKey(key), value, expiration).Err()
	logRedisOp(ctx, "SET", key, err)
	return err
}

// SetNX 仅当 key 不存在时设置值，返回 true 表示设置成功
func SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) (bool, error) {
	if !redisReady() {
		return false, fmt.Errorf("redis not initialized")
	}
	expiration = ResolveTTL(key, expiration)
	ok, err := rdb.SetNX(ctx, GetKey(key), value, expiration).Result()
	logRedisOp(ctx, "SETNX", key, err)
	return ok, err
}

// Get 获取字符串值；key 不存在返回 redis.Nil（不视为错误）并上报命中统计。
func Get(ctx context.Context, key string) (string, error) {
	if !redisReady() {
		return "", fmt.Errorf("redis not initialized")
	}
	val, err := rdb.Get(ctx, GetKey(key)).Result()
	// Get 的 key miss (redis.Nil) 不算错误；命中与否上报 redis_cache_hits_total / redis_cache_misses_total。
	hit := err == nil
	if err != nil && err != redis.Nil {
		logRedisOp(ctx, "GET", key, err)
	} else {
		logRedisOp(ctx, "GET", key, nil)
	}
	metrics.RecordRedisHit(hit)
	return val, err
}

// GetBytes 获取值并以字节切片返回，适用于原始二进制数据。
func GetBytes(ctx context.Context, key string) ([]byte, error) {
	b, err := rdb.Get(ctx, GetKey(key)).Bytes()
	if err != nil && err != redis.Nil {
		logRedisOp(ctx, "GET", key, err)
	} else {
		logRedisOp(ctx, "GET", key, nil)
	}
	return b, err
}

// Delete 删除一个或多个 key（自动拼接键前缀）。
func Delete(ctx context.Context, keys ...string) error {
	realKeys := make([]string, len(keys))
	for i, k := range keys {
		realKeys[i] = GetKey(k)
	}
	err := rdb.Del(ctx, realKeys...).Err()
	for _, k := range keys {
		logRedisOp(ctx, "DEL", k, err)
	}
	return err
}

// Exists 判断指定 key 是否存在。
func Exists(ctx context.Context, key string) (bool, error) {
	if !redisReady() {
		return false, fmt.Errorf("redis not initialized")
	}
	n, err := rdb.Exists(ctx, GetKey(key)).Result()
	logRedisOp(ctx, "EXISTS", key, err)
	return n > 0, err
}

// Incr 将 key 存储的整数原子自增 1，返回自增后的值。
func Incr(ctx context.Context, key string) (int64, error) {
	val, err := rdb.Incr(ctx, GetKey(key)).Result()
	logRedisOp(ctx, "INCR", key, err)
	return val, err
}

// Expire 设置过期时间。expiration 为默认值，若配置中心 RedisTTL 配置了该 key
// 的动态 TTL 则优先使用配置值（见 ResolveTTL）。
func Expire(ctx context.Context, key string, expiration time.Duration) error {
	expiration = ResolveTTL(key, expiration)
	err := rdb.Expire(ctx, GetKey(key), expiration).Err()
	logRedisOp(ctx, "EXPIRE", key, err)
	return err
}

// TTL 返回 key 的剩余生存时间。
func TTL(ctx context.Context, key string) (time.Duration, error) {
	d, err := rdb.TTL(ctx, GetKey(key)).Result()
	logRedisOp(ctx, "TTL", key, err)
	return d, err
}

// IncrBy 指定步长自增
func IncrBy(ctx context.Context, key string, value int64) (int64, error) {
	val, err := rdb.IncrBy(ctx, GetKey(key), value).Result()
	logRedisOp(ctx, "INCRBY", key, err)
	return val, err
}

// Decr 将 key 存储的整数原子自减 1，返回自减后的值。
func Decr(ctx context.Context, key string) (int64, error) {
	val, err := rdb.Decr(ctx, GetKey(key)).Result()
	logRedisOp(ctx, "DECR", key, err)
	return val, err
}

// MGet 批量获取
func MGet(ctx context.Context, keys ...string) ([]interface{}, error) {
	realKeys := make([]string, len(keys))
	for i, k := range keys {
		realKeys[i] = GetKey(k)
	}
	val, err := rdb.MGet(ctx, realKeys...).Result()
	logRedisOp(ctx, "MGET", fmt.Sprintf("%v", keys), err)
	return val, err
}

// MSet 批量设置
func MSet(ctx context.Context, values ...interface{}) error {
	err := rdb.MSet(ctx, values...).Err()
	logRedisOp(ctx, "MSET", "", err)
	return err
}

// ---- List 命令 ----

// LPush 将一个或多个值从左侧（表头）推入列表，返回列表长度。
func LPush(ctx context.Context, key string, values ...interface{}) (int64, error) {
	val, err := rdb.LPush(ctx, GetKey(key), values...).Result()
	logRedisOp(ctx, "LPUSH", key, err)
	return val, err
}

// RPush 将一个或多个值从右侧（表尾）推入列表，返回列表长度。
func RPush(ctx context.Context, key string, values ...interface{}) (int64, error) {
	val, err := rdb.RPush(ctx, GetKey(key), values...).Result()
	logRedisOp(ctx, "RPUSH", key, err)
	return val, err
}

// LPop 弹出并返回列表左侧（表头）第一个元素。
func LPop(ctx context.Context, key string) (string, error) {
	val, err := rdb.LPop(ctx, GetKey(key)).Result()
	logRedisOp(ctx, "LPOP", key, err)
	return val, err
}

// RPop 弹出并返回列表右侧（表尾）最后一个元素。
func RPop(ctx context.Context, key string) (string, error) {
	val, err := rdb.RPop(ctx, GetKey(key)).Result()
	logRedisOp(ctx, "RPOP", key, err)
	return val, err
}

// LRange 返回列表下标范围 [start, stop] 内的元素（含两端）。
func LRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	val, err := rdb.LRange(ctx, GetKey(key), start, stop).Result()
	logRedisOp(ctx, "LRANGE", key, err)
	return val, err
}

// LLen 返回列表的元素个数。
func LLen(ctx context.Context, key string) (int64, error) {
	val, err := rdb.LLen(ctx, GetKey(key)).Result()
	logRedisOp(ctx, "LLEN", key, err)
	return val, err
}

// LTrim 裁剪列表只保留 [start, stop] 范围
func LTrim(ctx context.Context, key string, start, stop int64) error {
	err := rdb.LTrim(ctx, GetKey(key), start, stop).Err()
	logRedisOp(ctx, "LTRIM", key, err)
	return err
}

// ---- Hash 命令 ----

// HSet 为哈希字段赋值，返回新增字段个数。
func HSet(ctx context.Context, key string, values ...interface{}) (int64, error) {
	val, err := rdb.HSet(ctx, GetKey(key), values...).Result()
	logRedisOp(ctx, "HSET", key, err)
	return val, err
}

// HGet 返回哈希表中指定字段的值。
func HGet(ctx context.Context, key, field string) (string, error) {
	val, err := rdb.HGet(ctx, GetKey(key), field).Result()
	logRedisOp(ctx, "HGET", key, err)
	return val, err
}

// HGetAll 返回哈希表中所有字段及其值。
func HGetAll(ctx context.Context, key string) (map[string]string, error) {
	val, err := rdb.HGetAll(ctx, GetKey(key)).Result()
	logRedisOp(ctx, "HGETALL", key, err)
	return val, err
}

// HDel 删除哈希表中的一个或多个字段，返回成功删除个数。
func HDel(ctx context.Context, key string, fields ...string) (int64, error) {
	val, err := rdb.HDel(ctx, GetKey(key), fields...).Result()
	logRedisOp(ctx, "HDEL", key, err)
	return val, err
}

// HExists 判断哈希表中是否存在指定字段。
func HExists(ctx context.Context, key, field string) (bool, error) {
	val, err := rdb.HExists(ctx, GetKey(key), field).Result()
	logRedisOp(ctx, "HEXISTS", key, err)
	return val, err
}

// HIncrBy 将哈希字段的整数值按步长自增，返回自增后的值。
func HIncrBy(ctx context.Context, key, field string, incr int64) (int64, error) {
	val, err := rdb.HIncrBy(ctx, GetKey(key), field, incr).Result()
	logRedisOp(ctx, "HINCRBY", key, err)
	return val, err
}

// ---- Set 命令 ----

// SAdd 向集合添加一个或多个成员，返回新增成员个数。
func SAdd(ctx context.Context, key string, members ...interface{}) (int64, error) {
	val, err := rdb.SAdd(ctx, GetKey(key), members...).Result()
	logRedisOp(ctx, "SADD", key, err)
	return val, err
}

// SRem 从集合移除一个或多个成员，返回成功移除个数。
func SRem(ctx context.Context, key string, members ...interface{}) (int64, error) {
	val, err := rdb.SRem(ctx, GetKey(key), members...).Result()
	logRedisOp(ctx, "SREM", key, err)
	return val, err
}

// SIsMember 判断成员是否属于该集合。
func SIsMember(ctx context.Context, key string, member interface{}) (bool, error) {
	val, err := rdb.SIsMember(ctx, GetKey(key), member).Result()
	logRedisOp(ctx, "SISMEMBER", key, err)
	return val, err
}

// SMembers 返回集合中的所有成员。
func SMembers(ctx context.Context, key string) ([]string, error) {
	val, err := rdb.SMembers(ctx, GetKey(key)).Result()
	logRedisOp(ctx, "SMEMBERS", key, err)
	return val, err
}

// SCard 返回集合的成员个数。
func SCard(ctx context.Context, key string) (int64, error) {
	val, err := rdb.SCard(ctx, GetKey(key)).Result()
	logRedisOp(ctx, "SCARD", key, err)
	return val, err
}

// ---- Sorted Set 命令 ----

// Z 表示有序集合元素，包含成员值与排序分值 score（等价于 go-redis 的 redis.Z）。
type Z = redis.Z

// ZAdd 向有序集合添加一个或多个带分值的成员，返回新增成员个数。
func ZAdd(ctx context.Context, key string, members ...*redis.Z) (int64, error) {
	val, err := rdb.ZAdd(ctx, GetKey(key), members...).Result()
	logRedisOp(ctx, "ZADD", key, err)
	return val, err
}

// ZRem 从有序集合移除一个或多个成员，返回成功移除个数。
func ZRem(ctx context.Context, key string, members ...interface{}) (int64, error) {
	val, err := rdb.ZRem(ctx, GetKey(key), members...).Result()
	logRedisOp(ctx, "ZREM", key, err)
	return val, err
}

// ZRemRangeByScore 移除有序集合中分值落在 [min, max] 范围内的所有成员。
func ZRemRangeByScore(ctx context.Context, key string, min, max string) (int64, error) {
	val, err := rdb.ZRemRangeByScore(ctx, GetKey(key), min, max).Result()
	logRedisOp(ctx, "ZREMRANGEBYSCORE", key, err)
	return val, err
}

// ZRange 按分值升序返回有序集合下标范围 [start, stop] 内的成员。
func ZRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	val, err := rdb.ZRange(ctx, GetKey(key), start, stop).Result()
	logRedisOp(ctx, "ZRANGE", key, err)
	return val, err
}

// ZRangeWithScores 按分值升序返回指定范围成员及其分值。
func ZRangeWithScores(ctx context.Context, key string, start, stop int64) ([]redis.Z, error) {
	val, err := rdb.ZRangeWithScores(ctx, GetKey(key), start, stop).Result()
	logRedisOp(ctx, "ZRANGEWITHSCORES", key, err)
	return val, err
}

// ZRevRange 按分值降序返回有序集合下标范围 [start, stop] 内的成员。
func ZRevRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	val, err := rdb.ZRevRange(ctx, GetKey(key), start, stop).Result()
	logRedisOp(ctx, "ZREVRANGE", key, err)
	return val, err
}

// ZRevRangeWithScores 按分值降序返回指定范围成员及其分值。
func ZRevRangeWithScores(ctx context.Context, key string, start, stop int64) ([]redis.Z, error) {
	val, err := rdb.ZRevRangeWithScores(ctx, GetKey(key), start, stop).Result()
	logRedisOp(ctx, "ZREVRANGEWITHSCORES", key, err)
	return val, err
}

// ZCard 返回有序集合的成员个数。
func ZCard(ctx context.Context, key string) (int64, error) {
	val, err := rdb.ZCard(ctx, GetKey(key)).Result()
	logRedisOp(ctx, "ZCARD", key, err)
	return val, err
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
	err := rdb.Eval(ctx, script, []string{GetKey(lockKey)}, instanceID).Err()
	logRedisOp(ctx, "UNLOCK", lockKey, err)
	return err
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

// Close 关闭全局 Redis 客户端连接，未初始化时安全返回。
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

// BloomFilter 哈希常量（双哈希 double hashing 实现，见 Add/Exists）：
//   - bloomHashSeed：murmur3 的固定种子。取 0xcc9e2d51 ^ 0x1b873593（两个经典
//     魔数异或）而非直接用 0——murmur3 在 0 种子下对空串/短串区分度差。
//     固定种子保证同一 value 在多实例、多次重启后哈希结果一致
//     （Redis bitmap 持久化在服务端，哈希必须可复现）。
//   - bloomHashIncrement：双哈希步长。只需算一次主哈希 fp，第 i 个哈希位
//     = (fp + i*increment) % size；0x5bd1e995 是与 2^32 互质的奇数
//     （MurmurHash 黄金比例乘数），保证 i 个哈希位均匀铺满位空间、不聚集。
//
// 用法约束：Add 与 Exists 必须使用完全相同的 seed/increment，
// 否则同一 value 算出的位集合不一致，会出现「已加入的元素查不到」。
const (
	bloomHashSeed      = 0xcc9e2d51 ^ 0x1b873593
	bloomHashIncrement = 0x5bd1e995
)

// BloomFilter 是基于 Redis bitmap 的布隆过滤器，用于判断元素「可能存在/一定不存在」。
type BloomFilter struct {
	key    string // Redis 中位数组键名（已带 KeyPrefix）
	size   uint64 // 位数组大小（bit 数）
	hashes uint64 // 哈希函数个数
}

// NewBloomFilter 创建布隆过滤器，需指定键名、位数组大小与哈希函数个数。
func NewBloomFilter(key string, size, hashes uint64) *BloomFilter {
	return &BloomFilter{
		key:    GetKey(key),
		size:   size,
		hashes: hashes,
	}
}

// Add 通过双哈希将 value 映射到多个位并置位，标记其可能存在。
func (bf *BloomFilter) Add(ctx context.Context, value string) error {
	pipe := rdb.Pipeline()
	// 主哈希 + 双哈希增量生成 hashes 个位，seed/increment 见 bloomHashSeed/bloomHashIncrement
	fp := murmur3.SeedSum64(bloomHashSeed, []byte(value))
	for i := uint64(0); i < bf.hashes; i++ {
		pipe.SetBit(ctx, bf.key, int64((fp+uint64(i)*bloomHashIncrement)%bf.size), 1)
	}
	_, err := pipe.Exec(ctx)
	logRedisOp(ctx, "BFADD", bf.key, err)
	return err
}

// AddBulk 批量添加元素（单次 pipeline 一次往返写入），用于启动预热等海量灌入场景。
// 逐条 Add 每条一次 RTT，千万级用户预热会退化为千万次往返；批量写入按批收敛为
// 一次往返（Redis 侧命令仍逐条执行，本实现以内存换吞吐，注意控制每批 size）。
func (bf *BloomFilter) AddBulk(ctx context.Context, values []string) error {
	if !redisReady() {
		return fmt.Errorf("bloom filter AddBulk: redis not initialized")
	}
	pipe := rdb.Pipeline()
	for _, value := range values {
		fp := murmur3.SeedSum64(bloomHashSeed, []byte(value))
		for i := uint64(0); i < bf.hashes; i++ {
			pipe.SetBit(ctx, bf.key, int64((fp+uint64(i)*bloomHashIncrement)%bf.size), 1)
		}
	}
	_, err := pipe.Exec(ctx)
	logRedisOp(ctx, "BFADDBULK", bf.key, err)
	return err
}

// Exists 检查 value 对应的多个位是否均置位，全命中返回 true（可能误判）。
func (bf *BloomFilter) Exists(ctx context.Context, value string) (bool, error) {
	// 与 Add 使用完全相同的 seed/increment，保证对同一 value 算出的位一致
	fp := murmur3.SeedSum64(bloomHashSeed, []byte(value))
	for i := uint64(0); i < bf.hashes; i++ {
		bit, err := rdb.GetBit(ctx, bf.key, int64((fp+uint64(i)*bloomHashIncrement)%bf.size)).Result()
		if err != nil {
			return false, err
		}
		if bit == 0 {
			return false, nil
		}
	}
	return true, nil
}

// SingleFlightDo 合并相同 key 的并发调用为一次执行，避免缓存击穿。
func SingleFlightDo(key string, fn func() (interface{}, error)) (interface{}, error) {
	v, err, _ := sfGroup.Do(key, fn)
	return v, err
}

// SingleFlightDoChan 同 SingleFlightDo，但立即返回结果通道以便异步等待。
func SingleFlightDoChan(key string, fn func() (interface{}, error)) <-chan singleflight.Result {
	ch := sfGroup.DoChan(key, fn)
	return ch
}

// LocalCache 是带 TTL 与容量上限的本地内存缓存，按 FIFO 淘汰以应对热点访问。
type LocalCache struct {
	data      map[string]*cacheItem // 缓存键值存储
	mu        sync.RWMutex          // 保护 data 的并发读写
	maxSize   int                   // 最大缓存条目数（超出按 FIFO 淘汰）
	expire    time.Duration         // 条目默认过期时间
	cleanupCh chan struct{}         // 关闭信号，用于停止后台清理 goroutine
}

type cacheItem struct {
	value      interface{} // 缓存值
	expireTime time.Time   // 过期时间（绝对时间）
}

// NewLocalCache 创建本地缓存，指定最大条目数与默认过期时间并启动后台清理。
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

// LocalCacheSet 设置本地缓存。
//
// 注意：写入本地缓存【不】广播失效通知。原因：多个实例可能在几乎同时回源并各自
// Set 同一 key，若此处也广播失效，会互相删除对方刚写入的缓存，造成缓存抖动、命中率
// 下降。正确的失效时机是「数据发生变更」的 LocalCacheDelete（见下文），由它在更新
// DB 后广播，通知其他实例清除旧值。配合 localCache 的 30s TTL 兜底，多实例最终一致。
func LocalCacheSet(key string, value interface{}) {
	localCache.Set(key, value)
	// 通知其他实例清除该 key 的本地缓存，保证多实例数据一致性
	PublishCacheInvalidation(context.Background(), key)
}

// LocalCacheSetWithTTL 以自定义 TTL 写入本地缓存，用于 hot key 本地缓存策略。
// 注意同样遵循「写入不广播」约定：需要使其他实例失效时请调用 LocalCacheDelete。
func LocalCacheSetWithTTL(key string, value interface{}, ttl time.Duration) {
	localCache.SetWithTTL(key, value, ttl)
}

// LocalCacheDelete 删除本地缓存，并广播失效通知给其他实例
func LocalCacheDelete(key string) {
	localCache.Delete(key)
	// 通知其他实例清除该 key 的本地缓存，保证多实例数据一致性
	PublishCacheInvalidation(context.Background(), key)
}

// Get 读取本地缓存，命中且未过期返回 (值, true)，否则返回 (nil, false)。
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

// Set 以默认 TTL 写入本地缓存，达到上限时按 FIFO 淘汰最旧条目。
func (lc *LocalCache) Set(key string, value interface{}) {
	lc.SetWithTTL(key, value, lc.expire)
}

// SetWithTTL 以自定义 TTL 写入本地缓存（hot key 本地缓存策略使用配置的 LocalCacheTTL）。
func (lc *LocalCache) SetWithTTL(key string, value interface{}, ttl time.Duration) {
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
		expireTime: time.Now().Add(ttl),
	}
}

// Delete 从本地缓存删除指定 key。
func (lc *LocalCache) Delete(key string) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	delete(lc.data, key)
}

// Clear 清空本地缓存的全部条目。
func (lc *LocalCache) Clear() {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.data = make(map[string]*cacheItem)
}
