package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/mysunshines/gocommon/config"
)

// demoRedisConfig 返回用于示例的本地 Redis 配置。
// 以下示例仅在编译期校验 API 用法；如需真正运行，请在本机启动 Redis。
func demoRedisConfig() *config.RedisConfig {
	return &config.RedisConfig{Host: "127.0.0.1", Port: 6379, DB: 0}
}

// ExampleInit 演示 Redis 缓存初始化 (Init 内部自动 Ping 验证)
func ExampleInit() {
	cfg := demoRedisConfig()
	if err := Init(cfg); err != nil {
		fmt.Printf("init cache failed: %v\n", err)
		return
	}
	defer Close()
	fmt.Println("cache connected and ready")
}

// ExampleSet 演示 Set / Get / Exists / Delete / Expire，Key 自动加前缀
func ExampleSet() {
	cfg := demoRedisConfig()
	if err := Init(cfg); err != nil {
		return
	}
	defer Close()

	ctx := context.Background()
	key := "user:1001"

	if err := Set(ctx, key, `{"name":"张三"}`, 10*time.Minute); err != nil {
		fmt.Printf("set failed: %v\n", err)
		return
	}
	fmt.Println("set ok")

	val, err := Get(ctx, key)
	if err != nil {
		fmt.Printf("get failed: %v\n", err)
		return
	}
	fmt.Printf("get: %s\n", val)

	ok, _ := Exists(ctx, key)
	fmt.Printf("exists: %v\n", ok)

	Expire(ctx, key, 30*time.Minute)

	Delete(ctx, key)
	fmt.Println("deleted")
}

// ExampleIncr 演示原子自增 — 适用于点赞数、阅读量等
func ExampleIncr() {
	cfg := demoRedisConfig()
	if err := Init(cfg); err != nil {
		return
	}
	defer Close()

	ctx := context.Background()
	key := "article:42:views"

	for i := 0; i < 5; i++ {
		n, _ := Incr(ctx, key)
		fmt.Printf("views: %d\n", n)
	}
}

// ExampleBloomFilter 演示布隆过滤器 — 用于缓存穿透保护
func ExampleBloomFilter() {
	cfg := demoRedisConfig()
	if err := Init(cfg); err != nil {
		return
	}
	defer Close()

	ctx := context.Background()
	bf := NewBloomFilter("cache:bf:users", 100000, 7)

	bf.Add(ctx, "1001")
	bf.Add(ctx, "1002")
	bf.Add(ctx, "1003")

	exist, _ := bf.Exists(ctx, "1001")
	fmt.Printf("user 1001 exists: %v\n", exist)

	notExist, _ := bf.Exists(ctx, "9999")
	fmt.Printf("user 9999 exists: %v\n", notExist)
}

// ExampleSingleFlightDo 演示 singleflight 合并并发请求，只查一次 DB
func ExampleSingleFlightDo() {
	cfg := demoRedisConfig()
	if err := Init(cfg); err != nil {
		return
	}
	defer Close()

	ctx := context.Background()
	key := "article:42"

	if val, err := Get(ctx, key); err == nil {
		fmt.Printf("cache hit: %s\n", val)
		return
	}

	result, err := SingleFlightDo(key, func() (interface{}, error) {
		time.Sleep(100 * time.Millisecond)
		data := fmt.Sprintf(`{"title":"Hello","content":"..."}`)
		Set(ctx, key, data, 5*time.Minute)
		return data, nil
	})
	if err != nil {
		fmt.Printf("singleflight failed: %v\n", err)
		return
	}
	fmt.Printf("loaded from db: %v\n", result)
}

// ExampleLocalCacheSet 演示内存缓存 — 用于热点数据、配置项
func ExampleLocalCacheSet() {
	LocalCacheSet("config:site_name", "我的博客")
	LocalCacheSet("config:max_page_size", 50)

	if val, ok := LocalCacheGet("config:site_name"); ok {
		fmt.Printf("site_name = %v\n", val)
	}
	if val, ok := LocalCacheGet("config:max_page_size"); ok {
		fmt.Printf("max_page_size = %v\n", val)
	}
}

// ExampleGet 演示 本地缓存 → Redis → DB 三级查询
func ExampleGet() {
	cfg := demoRedisConfig()
	if err := Init(cfg); err != nil {
		return
	}
	defer Close()

	ctx := context.Background()
	key := "article:42"

	if val, ok := LocalCacheGet(key); ok {
		fmt.Printf("local cache hit: %v\n", val)
		return
	}

	if val, err := Get(ctx, key); err == nil {
		fmt.Printf("redis hit: %s\n", val)
		LocalCacheSet(key, val)
		return
	}

	result, _ := SingleFlightDo(key, func() (interface{}, error) {
		data := `{"title":"Hello","content":"World"}`
		Set(ctx, key, data, 5*time.Minute)
		return data, nil
	})

	fmt.Printf("db hit: %v\n", result)
	LocalCacheSet(key, result)
}

// ExampleSetNX 演示分布式锁 / 幂等操作 — 同时设置 key 和过期时间
func ExampleSetNX() {
	cfg := demoRedisConfig()
	if err := Init(cfg); err != nil {
		return
	}
	defer Close()

	ctx := context.Background()
	key := "lock:order:1001"

	ok, _ := SetNX(ctx, key, "locked", 30*time.Second)
	if ok {
		fmt.Println("acquired lock")
		defer Delete(ctx, key)
	} else {
		fmt.Println("lock already held, skip")
	}
}

// ExampleLPush 演示 LPush/RPush/LPop/RPop/LRange/LLen/LTrim
func ExampleLPush() {
	cfg := demoRedisConfig()
	if err := Init(cfg); err != nil {
		return
	}
	defer Close()

	ctx := context.Background()
	key := "article:recent"

	LPush(ctx, key, "article:3", "article:2", "article:1")

	length, _ := LLen(ctx, key)
	fmt.Printf("list length: %d\n", length)

	items, _ := LRange(ctx, key, 0, -1)
	fmt.Printf("all items: %v\n", items)

	item, _ := LPop(ctx, key)
	fmt.Printf("popped: %s\n", item)

	LTrim(ctx, key, 0, 99)
}

// ExampleHSet 演示 HSet/HGet/HGetAll/HDel/HExists/HIncrBy
func ExampleHSet() {
	cfg := demoRedisConfig()
	if err := Init(cfg); err != nil {
		return
	}
	defer Close()

	ctx := context.Background()
	key := "user:1001"

	HSet(ctx, key, "name", "张三", "email", "zhangsan@example.com", "age", 28)

	name, _ := HGet(ctx, key, "name")
	fmt.Printf("name: %s\n", name)

	hasPhone, _ := HExists(ctx, key, "phone")
	fmt.Printf("has phone: %v\n", hasPhone)

	all, _ := HGetAll(ctx, key)
	fmt.Printf("all fields: %v\n", all)

	HIncrBy(ctx, key, "views", 1)

	HDel(ctx, key, "age")
}

// ExampleSAdd 演示 SAdd/SRem/SIsMember/SMembers/SCard
func ExampleSAdd() {
	cfg := demoRedisConfig()
	if err := Init(cfg); err != nil {
		return
	}
	defer Close()

	ctx := context.Background()
	key := "article:42:tags"

	SAdd(ctx, key, "Go", "Redis", "Docker")
	SAdd(ctx, key, "Go")

	count, _ := SCard(ctx, key)
	fmt.Printf("tag count: %d\n", count)

	hasGo, _ := SIsMember(ctx, key, "Go")
	fmt.Printf("has Go: %v\n", hasGo)

	tags, _ := SMembers(ctx, key)
	fmt.Printf("tags: %v\n", tags)

	SRem(ctx, key, "Docker")
}

// ExampleZAdd 演示 ZAdd/ZRem/ZRange/ZRevRange/ZCard
func ExampleZAdd() {
	cfg := demoRedisConfig()
	if err := Init(cfg); err != nil {
		return
	}
	defer Close()

	ctx := context.Background()
	key := "article:views:rank"

	ZAdd(ctx, key,
		&Z{Score: 100, Member: "article:1"},
		&Z{Score: 250, Member: "article:2"},
		&Z{Score: 180, Member: "article:3"},
	)

	count, _ := ZCard(ctx, key)
	fmt.Printf("rank count: %d\n", count)

	lowest, _ := ZRange(ctx, key, 0, 2)
	fmt.Printf("asc rank: %v\n", lowest)

	top, _ := ZRevRange(ctx, key, 0, 2)
	fmt.Printf("desc rank: %v\n", top)

	rankWithScore, _ := ZRangeWithScores(ctx, key, 0, -1)
	for _, z := range rankWithScore {
		fmt.Printf("%s: %.0f views\n", z.Member, z.Score)
	}

	ZRem(ctx, key, "article:1")
}

// ExampleMSet 演示 MSet/MGet/TTL 批量操作
func ExampleMSet() {
	cfg := demoRedisConfig()
	if err := Init(cfg); err != nil {
		return
	}
	defer Close()

	ctx := context.Background()

	MSet(ctx, "user:1", "Alice", "user:2", "Bob", "user:3", "Charlie")
	Expire(ctx, "user:1", 10*time.Minute)

	vals, _ := MGet(ctx, "user:1", "user:2", "user:3")
	for i, v := range vals {
		fmt.Printf("user:%d = %v\n", i+1, v)
	}

	ttl, _ := TTL(ctx, "user:1")
	fmt.Printf("user:1 ttl: %v\n", ttl)

	raw, _ := GetBytes(ctx, "user:1")
	fmt.Printf("raw bytes: %s\n", raw)
}
