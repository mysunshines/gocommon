package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/mysunshines/gocommon/config"
)

// ============================================================================
// 缓存初始化示例
// ============================================================================

// ExampleInit 演示 Redis 缓存初始化 (Init 内部自动 Ping 验证)
func ExampleInit() {
	conf, err := config.Load("config.yaml")
	if err != nil {
		fmt.Printf("load config failed: %v\n", err)
		return
	}

	if err := Init(&conf.Redis); err != nil {
		fmt.Printf("init cache failed: %v\n", err)
		return
	}
	defer Close()
	fmt.Println("cache connected and ready")
}

// ============================================================================
// 基础读写与 Key 前缀示例
// ============================================================================

// ExampleBasicOps 演示 Set / Get / Exists / Delete / Expire，Key 自动加前缀
func ExampleBasicOps(redisCfg *config.RedisConfig) {
	if err := Init(redisCfg); err != nil {
		return
	}
	defer Close()

	ctx := context.Background()
	key := "user:1001"

	// Set: 实际 key 是 "{prefix}user:1001"
	if err := Set(ctx, key, `{"name":"张三"}`, 10*time.Minute); err != nil {
		fmt.Printf("set failed: %v\n", err)
		return
	}
	fmt.Println("set ok")

	// Get
	val, err := Get(ctx, key)
	if err != nil {
		fmt.Printf("get failed: %v\n", err)
		return
	}
	fmt.Printf("get: %s\n", val)

	// Exists
	ok, _ := Exists(ctx, key)
	fmt.Printf("exists: %v\n", ok)

	// Expire (刷新 TTL)
	Expire(ctx, key, 30*time.Minute)

	// Delete
	Delete(ctx, key)
	fmt.Println("deleted")
}

// ============================================================================
// Incr 计数器示例
// ============================================================================

// ExampleCounter 演示原子自增 — 适用于点赞数、阅读量等
func ExampleCounter(redisCfg *config.RedisConfig) {
	if err := Init(redisCfg); err != nil {
		return
	}
	defer Close()

	ctx := context.Background()
	key := "article:42:views"

	// 每次访问 +1
	for i := 0; i < 5; i++ {
		n, _ := Incr(ctx, key)
		fmt.Printf("views: %d\n", n)
	}
	// Output: views: 1 ... views: 5
}

// ============================================================================
// BloomFilter 示例
// ============================================================================

// ExampleBloomFilter 演示布隆过滤器 — 用于缓存穿透保护
func ExampleBloomFilter(redisCfg *config.RedisConfig) {
	if err := Init(redisCfg); err != nil {
		return
	}
	defer Close()

	ctx := context.Background()
	bf := NewBloomFilter("cache:bf:users", 100000, 7)

	// 添加已存在的用户 ID
	bf.Add(ctx, "1001")
	bf.Add(ctx, "1002")
	bf.Add(ctx, "1003")

	// 查询前先判断是否存在
	exist, _ := bf.Exists(ctx, "1001")
	fmt.Printf("user 1001 exists: %v\n", exist) // true

	notExist, _ := bf.Exists(ctx, "9999")
	fmt.Printf("user 9999 exists: %v\n", notExist) // false (大概率)
}

// ============================================================================
// SingleFlight 示例 — 防止缓存击穿
// ============================================================================

// ExampleSingleFlight 演示 singleflight 合并并发请求，只查一次 DB
func ExampleSingleFlight(redisCfg *config.RedisConfig) {
	if err := Init(redisCfg); err != nil {
		return
	}
	defer Close()

	ctx := context.Background()
	key := "article:42"

	// 先查缓存
	if val, err := Get(ctx, key); err == nil {
		fmt.Printf("cache hit: %s\n", val)
		return
	}

	// 缓存未命中，通过 singleflight 确保只有一个请求进入 DB
	result, err := SingleFlightDo(key, func() (interface{}, error) {
		// 模拟查询数据库
		time.Sleep(100 * time.Millisecond)
		data := fmt.Sprintf(`{"title":"Hello","content":"..."}`)
		// 回写缓存
		Set(ctx, key, data, 5*time.Minute)
		return data, nil
	})
	if err != nil {
		fmt.Printf("singleflight failed: %v\n", err)
		return
	}
	fmt.Printf("loaded from db: %v\n", result)
}

// ============================================================================
// LocalCache 本地内存缓存示例
// ============================================================================

// ExampleLocalCache 演示内存缓存 — 用于热点数据、配置项
func ExampleLocalCache() {
	// LocalCacheSet 写入内存缓存
	LocalCacheSet("config:site_name", "我的博客")
	LocalCacheSet("config:max_page_size", 50)

	// LocalCacheGet 读取
	if val, ok := LocalCacheGet("config:site_name"); ok {
		fmt.Printf("site_name = %v\n", val)
	}
	if val, ok := LocalCacheGet("config:max_page_size"); ok {
		fmt.Printf("max_page_size = %v\n", val)
	}
}

// ============================================================================
// 两级缓存组合示例
// ============================================================================

// ExampleTwoLevelCache 演示 本地缓存 → Redis → DB 三级查询
func ExampleTwoLevelCache(redisCfg *config.RedisConfig) {
	if err := Init(redisCfg); err != nil {
		return
	}
	defer Close()

	ctx := context.Background()
	key := "article:42"

	// 1. 先查本地缓存
	if val, ok := LocalCacheGet(key); ok {
		fmt.Printf("local cache hit: %v\n", val)
		return
	}

	// 2. 再查 Redis
	if val, err := Get(ctx, key); err == nil {
		fmt.Printf("redis hit: %s\n", val)
		LocalCacheSet(key, val) // 回写本地
		return
	}

	// 3. Redis 未命中，singleflight 查 DB
	result, _ := SingleFlightDo(key, func() (interface{}, error) {
		data := `{"title":"Hello","content":"World"}`
		Set(ctx, key, data, 5*time.Minute)
		return data, nil
	})

	fmt.Printf("db hit: %v\n", result)
	LocalCacheSet(key, result)
}

// ============================================================================
// SetNX 分布式锁示例
// ============================================================================

// ExampleSetNX 演示分布式锁 / 幂等操作 — 同时设置 key 和过期时间
func ExampleSetNX(redisCfg *config.RedisConfig) {
	if err := Init(redisCfg); err != nil {
		return
	}
	defer Close()

	ctx := context.Background()
	key := "lock:order:1001"

	// SetNX 仅当 key 不存在时设置成功，返回 true
	ok, _ := SetNX(ctx, key, "locked", 30*time.Second)
	if ok {
		fmt.Println("acquired lock")
		defer Delete(ctx, key) // 释放锁
		// ... 执行业务逻辑 ...
	} else {
		fmt.Println("lock already held, skip")
	}
}

// ============================================================================
// List 列表命令示例 — 消息队列 / 最近文章
// ============================================================================

// ExampleList 演示 LPush/RPush/LPop/RPop/LRange/LLen/LTrim
func ExampleList(redisCfg *config.RedisConfig) {
	if err := Init(redisCfg); err != nil {
		return
	}
	defer Close()

	ctx := context.Background()
	key := "article:recent"

	// LPush 左侧入队（最新文章排在前面）
	LPush(ctx, key, "article:3", "article:2", "article:1")
	// 也可从右侧追加：RPush(ctx, key, "article:4")

	// LLen 获取列表长度
	length, _ := LLen(ctx, key)
	fmt.Printf("list length: %d\n", length) // 3

	// LRange 获取 [0, -1] 表示全部
	items, _ := LRange(ctx, key, 0, -1)
	fmt.Printf("all items: %v\n", items) // [article:1 article:2 article:3]

	// LPop 从左侧出队（模拟消费队列）
	item, _ := LPop(ctx, key)
	fmt.Printf("popped: %s\n", item) // article:1

	// LTrim 只保留前 100 个
	LTrim(ctx, key, 0, 99)
}

// ============================================================================
// Hash 哈希命令示例 — 用户信息 / 文章元数据
// ============================================================================

// ExampleHash 演示 HSet/HGet/HGetAll/HDel/HExists/HIncrBy
func ExampleHash(redisCfg *config.RedisConfig) {
	if err := Init(redisCfg); err != nil {
		return
	}
	defer Close()

	ctx := context.Background()
	key := "user:1001"

	// HSet 设置多个字段
	HSet(ctx, key, "name", "张三", "email", "zhangsan@example.com", "age", 28)

	// HGet 获取单个字段
	name, _ := HGet(ctx, key, "name")
	fmt.Printf("name: %s\n", name)

	// HExists 判断字段是否存在
	hasPhone, _ := HExists(ctx, key, "phone")
	fmt.Printf("has phone: %v\n", hasPhone) // false

	// HGetAll 获取所有字段
	all, _ := HGetAll(ctx, key)
	fmt.Printf("all fields: %v\n", all)

	// HIncrBy 原子自增（比如文章阅读量）
	HIncrBy(ctx, key, "views", 1) // views += 1

	// HDel 删除字段
	HDel(ctx, key, "age")
}

// ============================================================================
// Set 集合命令示例 — 标签 / 去重
// ============================================================================

// ExampleSet 演示 SAdd/SRem/SIsMember/SMembers/SCard
func ExampleSet(redisCfg *config.RedisConfig) {
	if err := Init(redisCfg); err != nil {
		return
	}
	defer Close()

	ctx := context.Background()
	key := "article:42:tags"

	// SAdd 添加标签
	SAdd(ctx, key, "Go", "Redis", "Docker")
	SAdd(ctx, key, "Go") // 重复添加无影响

	// SCard 获取集合大小
	count, _ := SCard(ctx, key)
	fmt.Printf("tag count: %d\n", count) // 3

	// SIsMember 判断某个标签是否存在
	hasGo, _ := SIsMember(ctx, key, "Go")
	fmt.Printf("has Go: %v\n", hasGo) // true

	// SMembers 获取所有标签
	tags, _ := SMembers(ctx, key)
	fmt.Printf("tags: %v\n", tags)

	// SRem 删除标签
	SRem(ctx, key, "Docker")
}

// ============================================================================
// Sorted Set 有序集合示例 — 排行榜 / 时间线
// ============================================================================

// ExampleZSet 演示 ZAdd/ZRem/ZRange/ZRevRange/ZCard
func ExampleZSet(redisCfg *config.RedisConfig) {
	if err := Init(redisCfg); err != nil {
		return
	}
	defer Close()

	ctx := context.Background()
	key := "article:views:rank"

	// ZAdd 添加成员-分数（文章阅读量排行）
	ZAdd(ctx, key,
		&Z{Score: 100, Member: "article:1"},
		&Z{Score: 250, Member: "article:2"},
		&Z{Score: 180, Member: "article:3"},
	)

	// ZCard 排行榜成员数
	count, _ := ZCard(ctx, key)
	fmt.Printf("rank count: %d\n", count) // 3

	// ZRange 正序排名（阅读量从低到高）
	lowest, _ := ZRange(ctx, key, 0, 2)
	fmt.Printf("asc rank: %v\n", lowest)

	// ZRevRange 倒序排名（阅读量从高到低 — 常用）
	top, _ := ZRevRange(ctx, key, 0, 2)
	fmt.Printf("desc rank: %v\n", top) // [article:2 article:3 article:1]

	// ZRangeWithScores 带分数
	rankWithScore, _ := ZRangeWithScores(ctx, key, 0, -1)
	for _, z := range rankWithScore {
		fmt.Printf("%s: %.0f views\n", z.Member, z.Score)
	}

	// ZRem 移除某个成员
	ZRem(ctx, key, "article:1")
}

// ============================================================================
// 批量与 TTL 示例
// ============================================================================

// ExampleBatch 演示 MSet/MGet/TTL 批量操作
func ExampleBatch(redisCfg *config.RedisConfig) {
	if err := Init(redisCfg); err != nil {
		return
	}
	defer Close()

	ctx := context.Background()

	// MSet 批量写入
	MSet(ctx, "user:1", "Alice", "user:2", "Bob", "user:3", "Charlie")
	Expire(ctx, "user:1", 10*time.Minute)

	// MGet 批量读取
	vals, _ := MGet(ctx, "user:1", "user:2", "user:3")
	for i, v := range vals {
		fmt.Printf("user:%d = %v\n", i+1, v)
	}

	// TTL 查看剩余过期时间
	ttl, _ := TTL(ctx, "user:1")
	fmt.Printf("user:1 ttl: %v\n", ttl)

	// GetBytes 获取原始字节（适合二进制或 JSON 反序列化）
	raw, _ := GetBytes(ctx, "user:1")
	fmt.Printf("raw bytes: %s\n", raw)
}
