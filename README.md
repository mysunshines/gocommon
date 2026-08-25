# gocommon - 通用工具库

通用工具库（module: `github.com/mysunshines/gocommon`，当前版本 v1.5.9），提供跨微服务共享的基础设施组件。各业务服务通过 go.mod `replace` 指向本地目录或直接引用版本号。

## 目录结构

```
gocommon/
├── config/         # 配置管理（YAML 按环境加载 + 全局单例 + 热更字段）
├── log/            # 日志（logrus JSON 结构化 + 按天轮转 + Loki 异步推送）
├── constants/      # 跨服务契约常量（服务名/端口/JWT claims/错误码/Redis 前缀/WS 路径等）
├── util/           # 通用工具（加密/校验/时间/IP/切片操作）
├── database/       # 数据库连接（GORM MySQL，Ping 验证 + 慢查询检测）
├── cache/          # 缓存（Redis + 本地缓存 + 布隆过滤器 + singleflight + 热点分片）
├── pool/           # Goroutine 池（并行/串行/混合执行 + Future 模式）
├── grpcclient/     # 服务间 gRPC 调用统一入口（注册表 + 连接复用 + 鉴权透传 + 韧性）
├── consul/         # Consul 注册/注销（零 SDK，标准库 HTTP API + 金丝雀标记）
├── configcenter/   # 配置中心（Consul KV 长轮询 Watch + 热更下发）
├── resilience/     # 韧性策略（超时 + 熔断 + 限流 + 降级，按下游服务隔离）
├── observability/  # OpenTelemetry 链路追踪（TraceID 与日志共用）
├── retry/          # 带指数退避与抖动的可重试执行器
├── scheduler/      # 轻量定时调度器（daily/weekly/interval 三种规格）
├── notify/         # 邮件发送（net/smtp，HTML 正文 + 内联图片）
├── prometheus/     # Prometheus HTTP 查询客户端（即时/区间查询）
├── captcha/        # 图形验证码
├── minio/          # 对象存储（MinIO / S3 兼容）
├── upload/         # Web 框架无关的文件上传落盘
├── http/           # HTTP 客户端（net/http + fasthttp 双实现）
├── tcp/            # TCP 客户端
├── udp/            # UDP 客户端
├── kafka/          # Kafka 生产者/消费者
├── metrics/        # Prometheus 指标采集
├── middleware/     # 中间件（Gin HTTP + gRPC 拦截器：鉴权/限流/指标/日志/超时/CSRF）
├── response/       # 统一 HTTP 响应（业务错误码 + 分页）
└── grafana/        # Grafana 面板 JSON（监控配套）
```

---

## 配置管理

### 基本用法

```go
import goconfig "github.com/mysunshines/gocommon/config"

// 推荐：按环境加载（CONFIG_PATH 优先；否则 config/config_<APP_ENV>.yaml；兜底 config/config.yaml）
conf, err := goconfig.LoadByEnv()

// 显式指定路径加载
conf, err := goconfig.Load("config/config_test.yaml")

fmt.Println(conf.App.Name)
fmt.Println(conf.Database.DSN())      // "root:pass@tcp(localhost:3306)/blog?..."
fmt.Println(conf.Redis.Addr())        // "localhost:6379"
fmt.Println(conf.GRPC.Addr())         // "0.0.0.0:9101"

// 任意位置获取全局配置（无需传递 cfg 参数）
cfg := goconfig.Get()
fmt.Println(cfg.JWT.Secret)
```

**环境切换约定**：Docker 部署时由 compose 注入 `APP_ENV` 与 `CONFIG_PATH`（如 `APP_ENV=test` + `CONFIG_PATH=config/config_test.yaml`），本地开发默认读 `config/config.yaml`。配置中的热更字段（日志级别/限流阈值/超时/韧性/缓存策略）可被 Consul 配置中心覆盖，见「配置中心」章节。

### Config 结构体全貌

```go
type Config struct {
    App       AppConfig       // 应用名/环境/日志级别/端口
    Database  DatabaseConfig  // MySQL 连接信息 + DSN()
    Redis     RedisConfig     // Redis 连接信息 + Addr()
    JWT       JWTConfig       // JWT 密钥 + 过期时间
    GRPC      GRPCConfig      // gRPC 监听地址 + Addr()
    HTTP      HTTPConfig      // HTTP 监听地址 + Addr()
    Consul    ConsulConfig    // 服务发现配置
    Micro     MicroConfig     // 注册中心配置（type/address）
    Metrics   MetricsConfig   // Prometheus /metrics 端口
    RateLimit RateLimitConfig // 限流 QPS/Burst + 路由级规则
    Server    ServerConfig    // 入站 server 调优（gRPC/HTTP 超时与 keepalive，部分可热更）
    CORS      CORSConfig      // CORS 跨域（可选，HTTP 直连服务用）
    Mail      MailConfig      // SMTP 邮件（可选，发信服务用）
    MinIO     MinIOConfig     // 对象存储（文件上传统一落 MinIO）
    Loki      LokiConfig      // Loki 集中日志（未启用自动降级为本地日志）
    OTel      OTelConfig      // OpenTelemetry 链路追踪（OTLP 上报地址）
}
```

### 底层原理

#### YAML 到结构体的映射

`config.Load()` 的核心流程：

```
os.ReadFile("config.yaml")
  │
  ▼  原始字节
yaml.Unmarshal(data, &Config{})
  │
  ├─ yaml tag 映射规则：
  │    type DatabaseConfig struct {
  │        Host string `yaml:"host"`   ← 匹配 YAML 中的 database.host
  │        Port int    `yaml:"port"`
  │    }
  │
  ├─ yaml.Unmarshal 内部流程：
  │    1. yaml.Decoder.Decode() 递归遍历 YAML 节点树
  │    2. 根据 yaml tag → 反射 (reflect) 找到对应 struct field
  │    3. 类型转换：YAML 的字符串 "3306" → Go 的 int 3306
  │    4. 嵌套对象：database → DatabaseConfig, redis → RedisConfig
  │
  ▼
ApplyDefaults(&c)   ← 为未配置字段填充默认值
  │
  │   默认值策略（零值检测）：
  │    if c.App.Env == ""       → "development"
  │    if c.HTTP.Port == 0      → 8080
  │    if c.JWT.ExpireTime == 0 → 604800 (7天)
  │    if c.RateLimit.QPS == 0  → 1000
  │    ...
  │
  ▼
cfg = &c             ← 写入全局单例，供 Get() 访问
```

**`yaml.Unmarshal` 背后的原理**：
- Go 的 `gopkg.in/yaml.v3` 库将 YAML 文本解析为 **AST（抽象语法树）**，再通过 `reflect` 包动态写入结构体字段
- `yaml:"host"` 这类 tag 通过 `field.Tag.Get("yaml")` 在运行时提取，决定字段与 YAML key 的映射关系
- 嵌套结构体如 `DatabaseConfig`，在 YAML 中通过缩进表示层级关系，解析器递归处理

#### 全局单例模式

```go
var cfg *Config          // 包级变量，协程不安全但设计上只写一次

func Load(path string) (*Config, error) {
    ...
    cfg = &c             // 写入全局单例
    return &c, nil        // 同时返回，兼容两种使用方式
}

func Get() *Config {     // 无需传参，任意位置直接调用
    return cfg
}
```

**设计权衡**：
- **优点**：消除参数传递 — handler / service / middleware 中无需层层透传 `cfg`，直接 `config.Get()` 即可
- **风险**：全局可变状态。约定 `Load()` 仅在 `main()` 中调用一次，之后只读。如果并行 `Load()`，存在 data race
- **替代方案**：依赖注入（wire/fx）可避免全局变量，但增加复杂度。对于这种微服务规模的配置，全局单例是务实选择

#### 便利方法的模式

每个子配置都提供 `Addr()` / `DSN()` 等**派生方法**，避免调用方重复拼接字符串：

```go
// DatabaseConfig — 抽象 DSN 构建逻辑
func (d *DatabaseConfig) DSN() string {
    return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
        d.User, d.Password, d.Host, d.Port, d.Name)
}

// AppConfig — 自动处理 host 为空的情况
func (a *AppConfig) Addr() string {
    host := a.Host
    if host == "" { host = "0.0.0.0" }    // 防御性默认值
    return fmt.Sprintf("%s:%d", host, a.Port)
}
```

这些方法封装了**字符串拼装的关注点**：如果 DSN 格式需要变更（如增加 `tls=true`），只需修改一处。

---

## 日志

### 基本用法

```go
import "github.com/mysunshines/gocommon/log"

// 显式初始化
log.Init("logs", "info", "user-service")

// 自动初始化（掉用任何日志方法前未 Init 时自动触发）
log.Info("hello")  // 自动使用 defaults: "logs" + "info"

// 分级输出
log.Debug("SQL: SELECT * FROM users WHERE id = ?", 42)
log.Infof("用户 %d 登录成功", userID)
log.Warn("磁盘使用率 %.2f%%", 85.5)
log.Error("数据库连接失败: %v", err)
log.Fatal("无法启动服务: %v", err)  // 输出后 os.Exit(1)

// 结构化字段（推荐 — 可被日志采集系统索引）
log.WithField("user_id", 1001).Info("用户资料更新")
log.WithFields(map[string]interface{}{
    "host":   "10.0.0.1",
    "port":   3306,
    "latency_ms": 230,
}).Error("数据库连接超时")

// 模块子 Logger（注入固定字段，避免每行重复）
authLog := log.WithField("module", "auth")
authLog.Info("令牌生成成功")
authLog.Info("令牌验证通过")

// 日志轮转（切换到当天的新文件）
if err := log.RotateLog("user-service"); err != nil {
    // handle error
}
```

### 底层原理

#### logrus 架构

```
业务代码
  │
  ├── log.Info("msg")        → 包级函数（简便）
  ├── log.WithField(k,v).Info("msg") → 带结构化字段
  │
  ▼
GetLogger()           ← 懒初始化（sync.Once 保证单次）
  │
  ▼
*logrus.Logger        ← 全局单例
  │
  ├── Formatter: JSONFormatter    ← 所有日志统一 JSON 输出
  │     TimestampFormat: "2006-01-02 15:04:05"
  │
  ├── Level: 由 Init() 的 logLevel 参数设定
  │     Debug < Info < Warn < Error < Fatal
  │     仅 >= 设定级别的日志才会输出
  │
  └── Output: io.Writer
        ├── 文件: logs/user-service-2025-01-15.log  ← 按天分文件
        └── 兜底: os.Stdout  ← 文件打开失败时输出到标准输出
```

**logrus 的字段传递机制**：
- `logrus.Entry` 是不可变对象：`WithField()` 返回新 `*Entry`（浅拷贝 + 追加字段），原 Logger 不受影响
- 每次 `Info()` / `Error()` 等调用都会序列化当前 Entry 的所有字段为 JSON
- 这种设计使子 Logger（`authLog := log.WithField("module","auth")`）的行为与原始 Logger 完全一致

#### JSON 结构化日志 vs 纯文本

| 纯文本 | 结构化 JSON |
|--------|------------|
| `[INFO] user 1001 login success` | `{"level":"info","msg":"login success","user_id":1001,"time":"2025-01-15 10:30:00"}` |
| 需要用正则提取信息 | 直接 JSON 字段查询 |
| 不易被日志系统索引 | Elasticsearch/Loki 可按字段过滤聚合 |

**JSONFormatter 输出结构**：

```json
{
  "level": "info",
  "msg": "用户登录成功",
  "service": "user-service",
  "env": "production",
  "user_id": 1001,
  "time": "2025-01-15 10:30:00"
}
```

`service` 和 `env` 在 `Init()` 时自动注入，无需每行手写。

#### sync.Once — 线程安全的单次初始化

```go
var (
    logger *logrus.Logger
    once   sync.Once     // 核心：保证 Init() 只执行一次
)

func Init(logDir, logLevel, serviceName string) {
    once.Do(func() {     // 即使多个 goroutine 同时调用 Init()，也只会执行一次
        logger = logrus.New()
        ...
    })
}

func GetLogger() *logrus.Logger {
    if logger == nil {
        Init(defaults...)  // 懒初始化：只有首次调用时触发
    }
    return logger
}
```

`sync.Once.Do()` 内部使用 **atomic + mutex** 双检锁：第一个 goroutine 拿到锁执行函数，后续 goroutine 看到 `done` 标志位后直接跳过。这是 Go 标准库中最优雅的"只执行一次"原语。

#### 按天轮转（`RotateLog`）

```go
func Init(logDir, logLevel, serviceName string) {
    today := time.Now().Format("2006-01-02")
    logFile := filepath.Join(logDir, "user-service-" + today + ".log")
    file, _ := os.OpenFile(logFile, O_CREATE|O_WRONLY|O_APPEND, 0666)
    logger.SetOutput(file)
}

// 跨天时调用，切换到新日期的文件
func RotateLog(serviceName string) error {
    today := time.Now().Format("2006-01-02")
    logFile := filepath.Join(logDir, "user-service-" + today + ".log")
    file, _ := os.OpenFile(logFile, O_CREATE|O_WRONLY|O_APPEND, 0666)
    logger.SetOutput(file)
}
```

**轮转原理**：
- `SetOutput(file)` 替换 logger 的 `io.Writer`，新的日志写入新文件
- 旧文件的文件描述符在 logger 替换 writer 后被 Go GC 回收时关闭（或由 OS 在进程退出时关闭）
- 这种简化方案适合 Docker 容器场景（日志由 Docker logging driver 采集，不需要复杂的 logrotate）
- 没有使用 lumberjack 等第三方轮转库，保持依赖最小化

#### Loki 集中日志（可选启用）

```go
// 方式一：直接启用（main.go 中，Init 之后调用）
log.EnableLoki("http://loki:3100/loki/api/v1/push", "article-service", "")

// 方式二：从配置启用（推荐，未配置时自动降级为仅本地文件）
log.EnableLokiFromConfig(cfg.Loki, cfg.App.Name)
```

- 日志经后台 goroutine **异步批量推送**到 Loki（不阻塞业务写日志的路径），推送失败静默重试
- 推送到 Loki 的日志带 `service` 标签与每条的 `trace_id` 字段，可按 trace 聚合检索
- 配合 gateway 的 `GET /admin/trace?trace_id=xxx` 实现"一次查询看全链路"

---

## 常量

```go
import "github.com/mysunshines/gocommon/constants"

// 时间格式
constants.DateTimeFormat     // "2006-01-02 15:04:05"
constants.DateFormat         // "2006-01-02"
constants.DateTimeISO8601    // "2006-01-02T15:04:05Z"
constants.DateTimeLog        // "2006/01/02 - 15:04:05"

// HTTP Header
constants.HeaderContentType    // "Content-Type"
constants.HeaderXForwardedFor  // "X-Forwarded-For"
constants.HeaderXRealIP        // "X-Real-IP"

// 输入校验
constants.MinEmailLen    // 3
constants.MaxEmailLen    // 254
constants.MinUsernameLen // 3
constants.MaxUsernameLen // 32
constants.MinPasswordLen // 6
constants.MaxPasswordLen // 32

// JWT
constants.JWTAuthScheme    // "Bearer "
constants.JWTAuthSchemeLen // 7

// 环境变量
constants.EnvLogDir     // "LOG_DIR"
constants.EnvAppEnv     // "APP_ENV"
constants.EnvConfigPath // "CONFIG_PATH"

// 服务名（跨服务契约，Consul 注册与 grpcclient 注册表共用）
constants.ServiceNameUser         // "user-service"
constants.ServiceNameArticle      // "article-service"
constants.ServiceNameComment      // "comment-service"
constants.ServiceNameGateway      // "gateway"
constants.ServiceNameNotification // "notification-service"

// 站内消息契约（gateway / notification-service / 前端三方共用）
constants.WSPathNotification          // "/ws/notification"（WS 路径，Nginx 转发目标）
constants.RedisKeyPrefixNotification  // "notification:"（未读计数缓存前缀）
constants.DefaultCacheTTLNotification // 600s（未读计数缓存 TTL）
constants.MetricPrefixNotification    // "notification_service"（指标前缀）

// API 路径前缀（网关路由与前端 API_BASE 共用）
constants.APIPathPrefix // "/api/v1"
constants.GRPCServiceNameSuffix // "Service"（服务名推导约定：<prefix>-service → <pkg>.v1.<Prefix>Service）
```

> **契约常量的意义**：跨服务/跨仓库的字符串（服务名、路径、前缀）集中在此处单一来源，任何一方的改动在编译期传导到所有使用方，避免字符串散落导致的静默不一致。新增微服务应在此登记服务名常量。

### 统一错误码

```
 10001  ErrCodeBadRequest          请求参数无效
 10002  ErrCodeUnauthorized        未认证
 10003  ErrCodeForbidden           无权限
 10004  ErrCodeNotFound            资源不存在
 10005  ErrCodeInternal            内部错误
 10006  ErrCodeServiceUnavailable  服务不可用
 10007  ErrCodeTimeout             请求超时
 10008  ErrCodeRateLimited         请求被限流

 20001  ErrCodeUserExists          用户已存在
 20002  ErrCodeTokenInvalid        令牌无效
 20003  ErrCodePasswordIncorrect   密码错误
 20005  ErrCodeUserNotFound        用户不存在
 20006  ErrCodeTokenExpired        令牌过期

 30001  ErrCodeArticleNotFound     文章不存在

 40001  ErrCodeCommentNotFound     评论不存在
 40003  ErrCodeCommentDisabled     评论已关闭
 40004  ErrCodeCommentBlacklist    用户被拉黑
```

---

## 通用工具

```go
import "github.com/mysunshines/gocommon/util"

// 哈希与加密
md5Hash := util.MD5("data")
shaHash := util.SHA256("data")
hashedPwd, _ := util.HashPassword("password123")
isMatch := util.CheckPassword("password123", hashedPwd)

// UUID / Token / 随机字符串
uid := util.GenerateUUID()            // "550e8400-e29b-41d4-a716-446655440000"
token := util.GenerateToken(32)       // 32 位随机 token
randStr := util.GenerateRandomString(16)

// Base64
encoded := util.Base64Encode("hello")
decoded, _ := util.Base64Decode(encoded)

// 客户端 IP
func handler(c *gin.Context) {
    ip := util.GetClientIP(c)  // 自动解析 X-Forwarded-For、X-Real-IP
}

// 输入校验
util.IsValidEmail("test@example.com")     // true
util.IsValidUsername("alice")             // true
util.IsValidPassword("123456")            // true

// 时间处理
t, _ := util.ParseTime("2025-01-15 10:30:00")  // 支持 5 种时间格式
str := util.FormatTime(t)                        // "2025-01-15 10:30:00"
days := util.GetDaysBetween(start, end)

// 切片工具
util.Contains([]string{"a","b","c"}, "b")          // true
util.RemoveDuplicates([]string{"a","b","a"})       // ["a","b"]
util.InSlice([]int{1,2,3}, 2)                      // true
util.MaxInt(1, 2)  // 2
util.MinInt(1, 2)  // 1
```

---

## 数据库

### 基本用法

```go
import "github.com/mysunshines/gocommon/database"

// Init: 创建连接 + Ping 验证
if err := database.Init(&conf.Database, conf.App.Env); err != nil {
    panic(err)
}
defer database.Close()

// CRUD
db := database.GetDB()
db.Create(&user)
db.Where("id = ?", 1).First(&user)
db.Model(&user).Update("name", "new_name")
db.Delete(&user)

// 带超时的查询（context 传播到数据库驱动）
ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
defer cancel()
database.WithContext(ctx).Where("id = ?", 1).First(&user)

// 事务（自动 commit / rollback）
database.Transaction(func(tx *gorm.DB) error {
    tx.Create(&order)
    tx.Model(&product).Update("stock", gorm.Expr("stock - ?", order.Qty))
    return nil  // 返回 nil → commit；返回 error → rollback
})
```

### 底层原理

#### GORM 初始化全流程

```
database.Init(cfg, env)
  │
  ├── sync.Once.Do()          ← 数据库只初始化一次
  │
  ├── gorm.Open(mysql.Open(dsn), &gorm.Config{})
  │     │
  │     ├── mysql.Open() 内部：
  │     │    └── sql.Open("mysql", dsn)  ← 建立连接池(不建立实际连接)
  │     │        │
  │     │        ├── 加载 mysql driver（init() 注册到 database/sql）
  │     │        └── 创建 *sql.DB 对象（连接池管理器）
  │     │
  │     └── gorm.Config{
  │           Logger: GORM Logger（dev 环境 → Info 级别打印 SQL）
  │           NowFunc: time.Now().Local()  ← 统一本地时区
  │        }
  │
  ├── sqlDB.SetMaxOpenConns(100)      ← 最大打开连接数
  ├── sqlDB.SetMaxIdleConns(10)       ← 最大空闲连接数
  ├── sqlDB.SetConnMaxLifetime(3600s) ← 连接最大存活时间
  │
  └── sqlDB.PingContext(ctx)          ← 验证数据库可达
```

#### database/sql 连接池机制

GORM 底层使用 Go 标准库 `database/sql`，其连接池是整个数据库访问的核心：

```
            应用层                          连接池层                    MySQL
         ┌──────────┐              ┌──────────────────────┐      ┌────────┐
Query───>│ gorm.DB  │──>QueryRow──>│      *sql.DB         │──>TCP│ MySQL  │
         │          │<──result────│                      │<─────│ Server │
         └──────────┘              │  MaxOpenConns: 100   │      └────────┘
                                   │  MaxIdleConns: 10    │
                                   │  idle conns: [...]   │  空闲连接池
                                   │  in-use conns: [...] │  活跃连接
                                   │  wait queue: [...]   │  等待队列
                                   └──────────────────────┘
```

**连接生命周期**：

```
1. 获取连接：
   QueryRow() → sql.DB.conn()
     ├─ 有空闲连接 → 直接复用
     ├─ 无空闲且未达 MaxOpen → 创建新连接（三次握手）
     └─ 已达 MaxOpen → 阻塞等待（直到有连接归还或 ctx 超时）

2. 归还连接：
   查询结束 → 连接放回 idle 池
     ├─ idle < MaxIdle → 保留（下次复用）
     └─ idle >= MaxIdle → Close()（四次挥手）

3. 连接驱逐：
   ConnMaxLifetime 到期 → Close()（强制回收）
   驱动错误（网络断开） → 标记为 bad，不放回 idle 池
```

**关键参数调优**：

| 参数 | 本库默认 | 含义 | 过大的风险 |
|------|---------|------|-----------|
| `MaxOpenConns` | 100 | 最大连接数 | MySQL `max_connections` 耗尽 |
| `MaxIdleConns` | 10 | 空闲连接保留数 | 占用 MySQL 连接资源，尤其多实例部署 |
| `ConnMaxLifetime` | 3600s | 连接最大存活 | 太小则频繁建连；太大则 MySQL `wait_timeout` 早于回收导致 stale connection |

**常见坑**：`MaxIdleConns` 默认值为 2（Go 1.15+），如果 QPS 很高而空闲连接只有 2，每次请求都需创建新连接（三次握手 + TLS），极端情况下延迟增加 10-50ms。

#### Ping 验证的意义

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
sqlDB.PingContext(ctx)  // 发送一个轻量级 "ping" 包到 MySQL
```

- `sql.Open()` **不验证连接** — 只校验 DSN 格式，不连接数据库
- `PingContext()` 才真正发起 TCP 连接 + MySQL 握手验证
- 在 `main()` 启动阶段 Ping 失败 → 快速失败（fail-fast），避免服务运行后才暴露连接问题

#### 慢查询检测

```go
// SlowQueryLogger 实现了 gorm.logger.Interface 的 Trace 方法
func (s *SlowQueryLogger) Trace(ctx context.Context, begin time.Time, 
    fc func() (sql string, rowsAffected int64), err error) {
    
    elapsed := time.Since(begin)
    sql, rows := fc()              // fc() 返回本次执行的 SQL 和影响行数
    if elapsed > 100*time.Millisecond {   // 阈值: 100ms
        metrics.RecordSlowQuery(sql, elapsed)
        log.Warnf("Slow query: %s, duration: %v, rows: %d", sql, elapsed, rows)
    }
}
```

**运行机制**：
- GORM 在执行每条 SQL 的前后调用 `Trace`
- `begin` 是执行前的时间戳，`elapsed` = 执行后 - 执行前
- `fc()` 是一个**闭包**，调用它才会真正拿到 SQL 语句和影响行数（延迟求值，避免非慢查询的开销）
- 超过 100ms 时同时上报告警日志 + Prometheus 指标

#### `WithContext` 与 Context 传播

```go
func WithContext(ctx context.Context) *gorm.DB {
    return db.WithContext(ctx)
}
```

GORM 的 `WithContext(ctx)` 会将 `ctx` 传递给底层的 `database/sql`，实现：
- **超时传播**：`ctx, cancel := context.WithTimeout(...)` → SQL 执行超过 deadline 时自动返回 `context.DeadlineExceeded`
- **取消传播**：HTTP 客户端断开连接 → `ctx` 被取消 → 正在执行的 SQL 被中止
- 仅对支持 context 的数据库操作生效（所有 `*Context` 方法如 `QueryContext`、`ExecContext`）

---

## 缓存

### 基本用法

```go
import "github.com/mysunshines/gocommon/cache"

// 初始化（含 Ping 验证）
if err := cache.Init(&conf.Redis); err != nil {
    panic(err)
}
defer cache.Close()

// ========== 基础 String 操作 ==========
cache.Set(ctx, "user:1", data, 5*time.Minute)
val, err := cache.Get(ctx, "user:1")
exists, _ := cache.Exists(ctx, "user:1")
cache.Delete(ctx, "user:1")

// 仅当 key 不存在时设置（分布式锁的基础）
ok, _ := cache.SetNX(ctx, "lock:order:123", "node-1", 30*time.Second)

// 原子递增（点赞计数、阅读量）
count, _ := cache.Incr(ctx, "article:42:views")
cache.IncrBy(ctx, "article:42:likes", 5)
cache.Decr(ctx, "article:42:likes")

// 批量操作
vals, _ := cache.MGet(ctx, "user:1", "user:2", "user:3")
cache.MSet(ctx, "k1", "v1", "k2", "v2")

// TTL 管理
ttl, _ := cache.TTL(ctx, "session:abc")
cache.Expire(ctx, "session:abc", 30*time.Minute)

// ========== List（消息队列、时间线）==========
cache.LPush(ctx, "timeline:user:1", "post-3", "post-2", "post-1")
cache.RPush(ctx, "notifications", "new_comment")
msg, _ := cache.LPop(ctx, "notifications")     // 左侧出队
cache.LRange(ctx, "timeline:user:1", 0, 9)     // 最近 10 条
cache.LTrim(ctx, "timeline:user:1", 0, 99)     // 只保留最近 100 条
cache.LLen(ctx, "timeline:user:1")

// ========== Hash（用户资料、文章元信息）==========
cache.HSet(ctx, "user:profile:1", "name", "Alice", "age", 25)
name, _ := cache.HGet(ctx, "user:profile:1", "name")
allFields, _ := cache.HGetAll(ctx, "user:profile:1")
cache.HExists(ctx, "user:profile:1", "email")
cache.HIncrBy(ctx, "user:profile:1", "login_count", 1)
cache.HDel(ctx, "user:profile:1", "temp_field")

// ========== Set（标签、去重、黑白名单）==========
cache.SAdd(ctx, "article:1:tags", "go", "redis", "microservice")
isMember, _ := cache.SIsMember(ctx, "article:1:tags", "go")
allTags, _ := cache.SMembers(ctx, "article:1:tags")
count, _ := cache.SCard(ctx, "article:1:tags")
cache.SRem(ctx, "article:1:tags", "redis")

// ========== Sorted Set（排行榜、优先级队列）==========
cache.ZAdd(ctx, "leaderboard", &redis.Z{Score: 95.5, Member: "player-1"})
cache.ZAdd(ctx, "leaderboard", &redis.Z{Score: 88.0, Member: "player-2"})
top10, _ := cache.ZRevRangeWithScores(ctx, "leaderboard", 0, 9)
rank, _ := cache.ZCard(ctx, "leaderboard")
cache.ZRemRangeByScore(ctx, "leaderboard", "0", "50")  // 清理低分

// ========== 布隆过滤器（防缓存穿透）==========
bf := cache.NewBloomFilter("bloom:users", 100000, 3)
bf.Add(ctx, "user:1")
bf.Add(ctx, "user:2")
exists, _ := bf.Exists(ctx, "user:1")  // true
exists, _ = bf.Exists(ctx, "user:99")  // false（一定不存在）

// ========== singleflight（防缓存击穿）==========
// 1000 个并发请求同一个 key，只有一个真正执行数据库查询，其余共享结果
val, err := cache.SingleFlightDo("hotkey:user:1", func() (interface{}, error) {
    return queryFromDB(userID)
})

// Channel 模式（非阻塞获取）
ch := cache.SingleFlightDoChan("hotkey:user:1", fn)
select {
case result := <-ch:
    val, err = result.Val, result.Err
case <-time.After(time.Second):
    // 超时降级
}

// ========== 本地缓存（进程内热点数据，纳秒级）==========
cache.LocalCacheSet("hot:article:1", articleData)
val, ok := cache.LocalCacheGet("hot:article:1")

// ========== 热点感知 API（配合 Consul 配置中心热策略）==========
// 未命中任何热策略时，以下 API 行为与 Set/Get 完全一致，可平滑切换。
cache.SetSmart(ctx, "article:detail:42", articleData, 5*time.Minute)
data, err := cache.GetSmart(ctx, "article:detail:42")      // 本地缓存 → 随机分片 → Redis
raw, err := cache.GetBytesSmart(ctx, "article:detail:42")  // 原始字节（序列化对象场景）
cache.DeleteSmart(ctx, "article:detail:42")                 // 删全分片 + 本地缓存 + 跨实例失效广播
exists, _ := cache.ExistsSmart(ctx, "article:detail:42")
cache.ExpireSmart(ctx, "article:detail:42", 30*time.Minute)
```

### 热策略配置（Consul 配置中心动态下发）

`Set / SetNX / Expire` 的 TTL 与 `*Smart` 系列的分片/本地缓存策略，均可通过 **Consul 配置中心**按 key 在线动态下发，秒级生效、无需发版。

- **按 key 动态 TTL**（`redis_ttl`）：命中模式的 key 写入 TTL 被覆盖（`Set/SetNX/Expire` 内部自动套用，业务零改动）；未命中时保持调用方传入的默认值。
- **hot key 调整**（`hot_keys`）：分片分散（单 key QPS 均摊到 N 个 Redis 分片）+ 本地缓存（热点读卸载到进程内内存，多实例经 pub/sub 失效广播保证最终一致）。

**下发方式**：将 YAML/JSON 写入 Consul KV `config/<service>/<env>`（由 `configcenter.HotConfig.Cache` 承载，`apply` 时自动同步到本包 `cache.SetPolicy`）：

```yaml
cache:
  redis_ttl:                  # 按 key 模式覆盖 TTL（秒），支持 * 通配符，精确模式（模式更长者）优先
    "verify_code:*": 300
    "session:*": 1800
    "article:views:*": 86400
  hot_keys:                   # 热点 key 策略，key 为模式（支持 * 通配符）
    "article:detail:42":      # 精确模式优先于通配符
      shard_count: 8          # 分片数（1–64）：>1 时拆成 key:0..N-1，写全量、读随机
      local_cache_ttl: 5      # 本地缓存秒数：>0 时读取先查进程内缓存，miss 回源并回填
    "user:info:*":
      shard_count: 4
      local_cache_ttl: 10
```

> 修改 Consul KV 后由 `Watch` 长轮询即时感知；热点策略集合变化时会自动清空本地缓存，避免旧 TTL 的脏值残留。

### 底层原理

#### Redis 连接池（go-redis 内部）

```
cache.Init(cfg)
  └─ redis.NewClient(&redis.Options{
       PoolSize: 100,        ← 连接池大小
       Addr: "localhost:6379",
     })
       │
       ▼
   go-redis 连接池架构：
   ┌────────────────────────────────────┐
   │          connPool                   │
   │                                    │
   │  idleConns → [conn1] [conn2] ...   │ 空闲连接链表
   │  inUse      → conn3 conn4 ...      │ 正在使用的连接
   │                                    │
   │  Get(ctx):                         │
   │   ├─ 有空闲 → 复用                  │
   │   ├─ 少于 PoolSize → 新建 TCP 连接  │
   │   └─ 已达上限 → 阻塞等待             │
   │                                    │
   │  Put(conn):                        │
   │   ├─ conn 正常 → 放回 idle 链表     │
   │   └─ conn 异常 → Close() 丢弃       │
   └────────────────────────────────────┘
```

**连接池 vs 短连接**：
- 每次 Redis 操作都新建 TCP 连接 → 三次握手 + 四次挥手开销 ≈ 1-5ms
- 连接池复用 → 省去握/挥手开销，延迟 ≈ RTT（通常 < 1ms）
- **10 倍以上的延迟差异**，对高 QPS 服务至关重要

#### Key 前缀设计

```go
func GetKey(key string) string {
    return config.Get().Redis.KeyPrefix + key  // "blog:" + "user:1" → "blog:user:1"
}
```

所有缓存操作都自动添加前缀，**对调用方透明**：
- 多服务共用同一个 Redis 实例时，前缀隔离命名空间
- 切换环境（dev / staging / prod）只需改一个 prefix 配置
- 方便批量清理：`redis-cli KEYS "blog:*" | xargs redis-cli DEL`

#### 布隆过滤器：数学原理与实现

**概率性数据结构** — 判断一个元素 "可能存在于集合中" 或 "一定不存在于集合中"：

```
BloomFilter 内部：
                             ┌─ hash1("user:1") % size → 位置 a → setbit(a)=1
"user:1" → murmur3.SeedSum64 ─┼─ hash2("user:1") % size → 位置 b → setbit(b)=1
                             └─ hash3("user:1") % size → 位置 c → setbit(c)=1

Exists("user:99"):
  hash1 % size → 位置 a → getbit(a)=0 → ❌ 确定不存在（不查 DB）
  hash1 % size → 位置 a → getbit(a)=1
  hash2 % size → 位置 b → getbit(b)=1
  hash3 % size → 位置 c → getbit(c)=1 → ✅ 可能存在（需查 DB 确认）
```

**多哈希函数生成（Double Hashing 优化）**：

```go
fp := murmur3.SeedSum64(seed, []byte(value))  // 只计算一次哈希
for i := uint64(0); i < hashes; i++ {
    position = (fp + i * 0x5bd1e995) % size    // 用加法+乘法派生第 i 个哈希
}
```

- 标准布隆过滤器需要 k 个独立的哈希函数 → k 次计算
- Double Hashing 只需 1 次 MurmurHash3 + k 次加乘 → **快 k 倍**
- `0x5bd1e995` 是一个精心选择的质数，保证派生的哈希值均匀分布
- **假阳性率** ≈ `(1 - e^(-kn/m))^k`，本库默认 k=3, m=100000, n=10000 → 假阳性率 ≈ 0.8%

**适用场景**：
- 用户 ID 是否存在 → 不存在则不查 DB（防缓存穿透）
- URL 去重爬虫
- 黑名单快速判定

#### singleflight：缓存击穿的防线

```
正常情况（1000 个并发请求同一 key）：
  req-1 → Cache Miss → DB Query(500ms) → Set Cache → Return
  req-2 → Cache Miss → DB Query(500ms) → ...       ← 999 个重复 DB 查询
  req-3 → Cache Miss → ...
  ...（DB 被打挂）

使用 singleflight：
  req-1 → Cache Miss → singleflight.Do("key", fn)
  req-2 → Cache Miss → singleflight.Do("key", fn)  ← 检测到相同的 key
  req-3 → Cache Miss → singleflight.Do("key", fn)  ← 等待 req-1 的结果
  ...
  req-1: DB Query(500ms) → Set Cache → Return
  req-2~1000: 共享 req-1 的结果（无额外 DB 查询）
```

**实现原理**（golang.org/x/sync/singleflight）：

```go
type Group struct {
    mu sync.Mutex
    m  map[string]*call  // key → 正在进行的调用
}

type call struct {
    wg  sync.WaitGroup
    val interface{}
    err error
}

func (g *Group) Do(key string, fn func() (interface{}, error)) (interface{}, error) {
    g.mu.Lock()
    if c, ok := g.m[key]; ok {  // 已有相同 key 正在执行
        g.mu.Unlock()
        c.wg.Wait()              // 等待它完成
        return c.val, c.err      // 共享结果
    }
    c := &call{}
    c.wg.Add(1)
    g.m[key] = c
    g.mu.Unlock()

    c.val, c.err = fn()          // 只有一个 goroutine 执行 fn
    c.wg.Done()

    g.mu.Lock()
    delete(g.m, key)             // 清理
    g.mu.Unlock()

    return c.val, c.err
}
```

**关键细节**：
- `fn()` 只被调用一次，其他 `Do(key)` 通过 `sync.WaitGroup` 阻塞等待
- `SingleFlightDoChan` 返回 channel，可用于非阻塞等待 + select 超时
- 注意：singleflight 只防**相同 key 的并发重复计算**，不同 key 的请求仍然并发执行

#### 本地缓存（进程内 Map）

```go
type LocalCache struct {
    data      map[string]*cacheItem   // Go map，O(1) 查找
    mu        sync.RWMutex             // 读写锁，读并发、写互斥
    maxSize   int                      // 最大条目数（1000）
    expire    time.Duration            // 过期时间（10 分钟）
    cleanupCh chan struct{}            // 停止清理 goroutine 的信号
}
```

**过期清理**：

```go
func (lc *LocalCache) cleanup() {
    ticker := time.NewTicker(time.Minute)  // 每分钟扫描一次
    for {
        select {
        case <-ticker.C:
            lc.mu.Lock()
            for k, v := range lc.data {
                if time.Now().After(v.expireTime) {
                    delete(lc.data, k)     // 删除过期条目
                }
            }
            lc.mu.Unlock()
        case <-lc.cleanupCh:
            return                          // 停止清理
        }
    }
}
```

- **惰性清理 + 定时清理**：`Get()` 时检测过期 + 后台每分钟全量扫描
- **容量控制**：`maxSize` 满时随机删除一个条目（简单的 FIFO 近似）
- **延迟对比**：本地缓存 ≈ **纳秒级**，Redis 网络往返 ≈ **毫秒级** → 快 1000 倍以上
- 适用场景：配置文件、热门文章列表、不常变的元数据

#### 缓存穿透 / 击穿 / 雪崩对照

| 问题 | 场景 | 本库解决方案 |
|------|------|------------|
| **穿透** | 查询不存在的数据（恶意请求随机 ID） | 布隆过滤器（BloomFilter）预先判断 |
| **击穿** | 热点 key 过期瞬间大量请求打到 DB | singleflight（合并并发请求） |
| **雪崩** | 大量 key 同时过期 | 过期时间加随机抖动（业务侧）+ 多级缓存（本地缓存作为第一道防线） |

---

## Goroutine 池

```go
import "github.com/mysunshines/gocommon/pool"

// 创建池（默认 GOMAXPROCS*2 并发度）
p := pool.New(pool.WithMaxWorkers(8))

// 并行执行 — 同时查询多个微服务
results := p.Parallel(ctx,
    func(ctx context.Context) (interface{}, error) { return articleSvc.Get(id) },
    func(ctx context.Context) (interface{}, error) { return commentSvc.Count(id) },
    func(ctx context.Context) (interface{}, error) { return userSvc.GetName(uid) },
)
article := results[0].Value.(*Article)
article.CommentCount = results[1].Value.(int)
article.AuthorName = results[2].Value.(string)

// 串行执行 — 有依赖的任务顺序执行
results := pool.Default().Serial(ctx,
    func(ctx context.Context) (interface{}, error) { return saveDraft(), nil },
    func(ctx context.Context) (interface{}, error) { return validate(), nil },
    func(ctx context.Context) (interface{}, error) { return syncToES(), nil },
)

// Future 模式 — 先提交再等待，灵活编排
f1, _ := p.Submit(ctx, task1)
f2, _ := p.Submit(ctx, task2)
// ... 期间做其他事
v1, _ := f1.Get(ctx)
v2, _ := f2.Get(ctx)

// 混合执行 — 组间并行、组内串行（多表写入场景）
allResults := p.Mixed(ctx,
    []pool.Task{validateArticle, insertArticle},  // 组1：先校验后写入
    []pool.Task{validateTags, insertTags},        // 组2：先校验后写入
    []pool.Task{updateStats},                     // 组3：更新统计
)
// 三组并发执行，组内串行

// 便捷函数（使用默认全局池）
pool.Go(ctx, task1, task2, task3)       // 并行
pool.GoSerial(ctx, step1, step2)        // 串行
pool.GoMixed(ctx, group1, group2)       // 混合

// 池统计
stats := p.Stats()
fmt.Printf("提交:%d 完成:%d 失败:%d 活跃:%d\n",
    stats.TotalSubmitted, stats.TotalCompleted, stats.TotalFailed, stats.ActiveWorkers)
```

### 架构原理

**两种并发控制模型并存，按场景选用：**

#### Parallel — Worker-Pool 模式（高 QPS 优化）

`Parallel` 不按 task 数创建 goroutine，而是只创建 `min(maxWorkers, N)` 个 worker goroutine，各 worker 从带缓冲的 task channel 中消费任务：

```
Parallel(6 tasks, maxWorkers=3)

  taskCh (cap=6):  [t0] [t1] [t2] [t3] [t4] [t5]  → close
                       │      │      │
                    ┌──┘      │      └──┐
                    ▼         ▼         ▼
               worker0    worker1    worker2
                    │         │         │
                    ▼         ▼         ▼
                results[0] results[1] results[2]
                results[3] results[4] results[5]
```

- 6 个 task 但只创建了 3 个 goroutine（worker 从 channel 各取 2 个任务处理）
- 每个 worker 取完一个 task → 执行 → 取下一个，直到 channel 关闭
- goroutine 在执行完本批次所有 task 后自动退出，无需显式回收

| 场景 | goroutine 数 |
|------|-------------|
| 100 tasks × maxWorkers=8 | 8 个 |
| 2000 QPS × 5 tasks × maxWorkers=8 | **~8 goroutine/s**（复用） |

#### Mixed / Submit — Semaphore 模式

`Mixed`（组间并行、组内串行）和 `Submit/Future`（单任务提交）使用 `p.sem`（buffered channel）控制并发：

```
Submit → p.sem <- struct{}{}  // 槽位满则阻塞
           │
           go func() { ...; <-p.sem }  // 结束时释放
```

因为这两种模式的 goroutine 数本身很少（`Mixed` 按 group 数 + `Submit` 每次一个 task），semaphore 已足够。

### 流程时序

```
1. Parallel(ctx, t0..t9)           // 10 个 task
2. taskCh ← [t0..t9]，close        // 推入通道并关闭
3. spawn 8 个 worker goroutine     // min(maxWorkers=8, 10) = 8
4. 每个 worker: for item := range taskCh { ... }
   ├─ ctx.Err() 检查 → 已取消则跳过
   ├─ item.task(ctx) 执行
   └─ results[item.idx] = Result{...}
5. wg.Wait() — 所有 worker 退出后返回
6. 调用方按 Index 取 results
```

- **ctx 超时**：`ctx.Err()` 非 nil 时，channel 中剩余 task 被跳过并填充 `ctx.Err()`
- **panic 安全**：业务 task 中的 panic 由 gin RecoveryMiddleware 兜底，不影响 pool 本身
- **结果有序**：`results[task.idx]` 保证返回顺序与传入顺序一致

---

## gRPC 客户端（服务间调用统一入口）

`grpcclient` 是微服务间 gRPC 调用的唯一入口，统一了服务寻址、连接管理、鉴权透传与韧性控制。**禁止绕过本包直接 `grpc.Dial` 下游服务。**

### 基本用法

```go
import "github.com/mysunshines/gocommon/grpcclient"

// 方式一（推荐）：直接传下游 pb 生成的全方法名常量
var resp user.GetUserResponse
err := grpcclient.SendRequest(ctx, user.UserService_GetUser_FullMethodName,
    &user.GetUserRequest{UserId: 1}, &resp)

// 方式二：点号风格别名（不便引入下游 pb 时）
err = grpcclient.SendRequest(ctx, "user.v1.GetUser", &req, &resp)

// 带降级：熔断打开或调用失败时执行 fallback 填充兜底响应
err = grpcclient.SendRequestWithFallback(ctx, api, req, resp, func(ctx context.Context) error {
    // 填充 resp 的兜底值...
    return nil
})
```

### 服务寻址

| 机制 | 说明 |
|------|------|
| `RegisterService(alias, service, target)` | 启动时静态注册：`RegisterService("user.v1", "user.v1.UserService", "user-service:9101")` |
| `SetServiceResolver(fn)` | 自定义解析器（如对接 Consul 健康查询动态寻址），优先于注册表 |

### 核心能力（Dial 内置，调用方零配置）

```
SendRequest(ctx, api, req, resp)
  │
  ├─ parseAPI(api)              # 拆出 alias + method（兼容 "/" 与 "." 两种风格）
  ├─ resolveTarget(alias)       # resolver 优先，其次注册表
  ├─ getConn(target)            # 按 host:port 缓存 *grpc.ClientConn（含重连）
  │     └─ AuthForwardInterceptor  # 自动从 ctx 取原始 JWT 透传到下游 metadata
  ├─ resilience.ForService(alias)  # 按下游隔离的超时/熔断/限流策略
  └─ conn.Invoke(ctx, "/<service>/<method>", req, resp)
```

### 鉴权透传链（防越权的关键）

```
上游请求（带 Authorization）
  → 服务 A GRPCAuthInterceptor 解析 JWT，原始 token 存入 ctx（grpcTokenKey）
  → 业务代码 ctx 调 grpcclient.SendRequest
  → AuthForwardInterceptor 从 ctx 取 token 注入下游 metadata
  → 服务 B GRPCAuthInterceptor 校验并提取身份
```

服务 A 的 handler 若用 `context.Background()` 发起下游调用会丢失 token——链式调用务必透传请求 ctx。

---

## Consul 服务注册

`consul` 包提供微服务向 Consul 注册/注销的轻量实现，**零第三方 SDK**（标准库直接调 Consul HTTP API）。

### 基本用法

```go
import "github.com/mysunshines/gocommon/consul"

consul.UseConsulDiscovery(cfg.Consul.Address) // 初始化解析器

deregister, err := consul.Register(consul.Registration{
    Name:               cfg.App.Name,
    ConsulAddress:      cfg.Consul.Address,
    Address:            cfg.App.Host,   // 留空时自动探测容器 IP（非 loopback IPv4）
    GRPCPort:           cfg.GRPC.Port,
    HTTPPort:           cfg.HTTP.Port,
    CheckInterval:      cfg.Consul.CheckInterval,
    DeregisterCritical: cfg.Consul.DeregisterCritical,
    Canary:             consul.CanaryFromEnv(),        // BLOG_CANARY=true 时标记金丝雀
    Version:            consul.VersionFromEnv(Version), // SERVICE_VERSION 环境变量可覆盖
})
defer deregister() // 进程退出时注销
```

### 设计要点

| 要点 | 说明 |
|------|------|
| **地址自动探测** | 默认取本机非 loopback IPv4——Docker bridge 网络下即容器 IP，同网络其它容器可达；可用 `ADVERTISE_ADDR` 环境变量覆盖 |
| **降级友好** | 注册失败仅返回 error 不 panic，无 Consul 的本地开发环境可照常运行 |
| **金丝雀标记** | `Canary` + `Version` 写入 Consul Tag/Meta，配合 gateway 的 RoutingPolicy 实现按版本加权分流（同一镜像用环境变量区分 stable/canary） |

---

## 配置中心（Consul KV 热更）

`configcenter` 基于 Consul KV 实现配置热更：变更经长轮询 Watch 即时感知，秒级生效、无需发版。

### 基本用法

```go
import "github.com/mysunshines/gocommon/configcenter"

client := configcenter.New(cfg.Consul.Address)
key := configcenter.Key(cfg.App.Name, cfg.App.Env) // "config/<service>/<env>"

var hc configcenter.HotConfig
if err := client.Load(key, &hc); err != nil && err != configcenter.ErrNotFound {
    log.Warnf("load hot config failed: %v", err)
}

go client.Watch(key, &hc, func() {
    // KV 变更回调：hc 已被原子替换为最新值
    resilience.ApplySpecs(hc.Resilience)
    cache.SetPolicy(hc.Cache)
})
```

### 可热更的配置项（HotConfig）

```yaml
# Consul KV: config/<service>/<env>（YAML 或 JSON）
log_level: debug                 # 日志级别即时切换
rate_limit:                      # 入站限流阈值（本服务作为被调用方的总闸）
  enabled: true
  qps: 500
jwt_expire_time: 168
resilience:                      # 出站韧性（本服务作为调用方，按下游隔离）
  user.v1.UserService:
    timeout_sec: 3
    circuit: { max_requests: 5, interval_sec: 10, timeout_sec: 5 }
server:                          # HTTP 超时参数
  http_default_timeout: 30
cache:                           # Redis TTL 覆盖 + 热点 key 策略（见缓存章节）
  redis_ttl: { "session:*": 1800 }
  hot_keys: { "article:detail:*": { shard_count: 8, local_cache_ttl: 5 } }
```

> **RateLimit 与 Resilience 的语义区分**：前者是本服务的**入站**总限流（被调用方，一个总阈值）；后者是本服务对每个下游的**出站**韧性（调用方，按下游隔离）。二者方向相反，在同一份 YAML 中并列。

---

## 韧性策略（resilience）

`resilience` 聚合**超时 + 熔断 + 限流 + 降级**四种控制，按下游服务 key 隔离（一个下游故障不影响对其它下游的调用）。

### Policy 结构

```go
type Policy struct {
    Timeout   time.Duration       // 单次出站调用超时（0 = 默认）
    Circuit   CircuitConfig       // 熔断：Enabled/MaxRequests/Interval/Timeout/FailureRate
    RateLimit RateLimitConfig     // 出站限流：保护自身不被单一下游拖垮
    Fallback  func(ctx) (any, error) // 降级：熔断打开或致命错误时返回兜底
}
```

### 执行模型

```
policy.Execute(ctx, invoke, fallback)
  │
  ├─ 超时控制：ctx.WithTimeout(Timeout)
  ├─ 出站限流：令牌桶 Allow()，超限直接降级
  ├─ 熔断器（按 serviceKey 隔离的实例）：
  │    CLOSED ──错误率超阈值──▶ OPEN ──Timeout 后──▶ HALF_OPEN（放 MaxRequests 探测）
  │      ▲                                            │
  │      └────────── 探测成功 ────────── 探测失败 ──回到 OPEN
  └─ invoke 返回致命错误（isFatal）→ fallback 降级填充 resp
```

### 接入方式

```go
// 1. 代码直配
resilience.SetPolicy("user.v1.UserService", resilience.Policy{Timeout: 3 * time.Second})

// 2. 配置中心热更（推荐）
resilience.ApplySpecs(hc.Resilience)

// 3. 经 grpcclient 自动生效（SendRequestWithFallback 内部调用 ForService(alias).Execute）
```

---

## OpenTelemetry 链路追踪（observability）

用 OTel 的 TraceID 作为全链路唯一 ID，替换早期自研的随机 X-Trace-ID，使**日志（Loki）与链路追踪（Tempo/Jaeger）共用同一个 trace_id**。

### 基本用法

```go
import "github.com/mysunshines/gocommon/observability"

// main.go 初始化（未启用时各 API 均为安全空操作）
observability.InitAndRegister(cfg.App.Name, cfg.OTel)
defer observability.ShutdownGlobal(context.Background())

// gRPC server 自动挂载 OTel 拦截器
grpc.NewServer(observability.GRPCServerOptions()...)

// 业务代码取当前 TraceID（写日志、透传）
traceID := observability.TraceIDFromContext(ctx)
```

### 配置

```yaml
otel:
  enabled: false                # 默认关闭，配好 Tempo/otel-collector 后开启
  endpoint: "otel-collector:4317" # OTLP gRPC 上报地址
```

---

## 其它工具包速览

| 包 | 用途 | 典型调用方 |
|----|------|-----------|
| `retry` | 指数退避 + 抖动的可重试执行器（邮件、Prometheus 查询等瞬时失败场景） | report-service |
| `scheduler` | 零依赖定时调度器，支持 `daily HH:MM` / `weekly <Dow> HH:MM` / `interval 30m` 三种规格 | report-service（定时报表） |
| `notify` | net/smtp 邮件发送，支持 HTML 正文与内联图片（Content-ID） | report-service |
| `prometheus` | Prometheus HTTP 查询客户端（即时/区间查询，解析为时间序列结构） | report-service |
| `minio` | MinIO / S3 兼容对象存储轻量封装（仅依赖官方 SDK，不绑业务） | gateway（文件上传） |
| `upload` | Web 框架无关的文件上传落盘（不绑定 gin/echo） | 各业务服务 |
| `captcha` | 图形验证码生成与校验 | user-service |

---

## HTTP 客户端

提供两套实现，API 完全一致，按需切换：

| 客户端 | 底层 | 适用场景 |
|--------|------|---------|
| `Client` | `net/http` | 通用场景、需要 HTTP/2、低频调用 |
| `FastClient` | `fasthttp` | 高 QPS 内部调用、对延迟敏感 |

### 原生 Client（net/http）

```go
import httpclient "github.com/mysunshines/gocommon/http"

client := httpclient.New(
    httpclient.WithBaseURL("http://api.example.com"),
    httpclient.WithTimeout(30 * time.Second),
    httpclient.WithHeader("Authorization", "Bearer xxx"),
)

// GET / POST / PUT / DELETE / PATCH
resp, err := client.Get(ctx, "/users", map[string]string{"page": "1"})
resp, err := client.Post(ctx, "/users", userData)
resp, err := client.Put(ctx, "/users/1", updateData)
resp, err := client.Delete(ctx, "/users/1")
resp, err := client.Patch(ctx, "/users/1", patchData)

// 自定义 Header
resp, err := client.SendRequest(ctx, "DELETE", "/api/profile", nil,
    func(h http.Header) {
        h.Set("Authorization", "Bearer xxx")
        h.Set("X-Request-ID", "req-001")
    },
)

// 响应处理
resp.StatusCode   // int
resp.String()     // 响应体字符串
resp.IsSuccess()  // 2xx
resp.Unmarshal(&result)  // JSON 反序列化
```

### FastClient（fasthttp）

```go
// API 与原生 Client 完全一致，仅构造函数不同
client := httpclient.NewFast(
    httpclient.WithFastBaseURL("http://api.example.com"),
    httpclient.WithFastTimeout(30 * time.Second),
    httpclient.WithFastMaxConns(200),
)

resp, err := client.Get(ctx, "/users", map[string]string{"page": "1"})
resp, err := client.Post(ctx, "/users", userData)

// 便捷方法
var users []User
httpclient.QuickGetJSON(ctx, "http://api.example.com/users",
    map[string]string{"page": "1"}, &users)

var result LoginResult
httpclient.QuickPostJSON(ctx, "http://api.example.com/login",
    LoginReq{Username: "admin", Password: "123"}, &result)
```

### 底层原理

#### net/http Client 内部架构

```
client.Get(ctx, path, params)
  │
  ▼
http.NewRequestWithContext(ctx, method, url, body)
  │  └─ ctx 附加到 Request: req = req.WithContext(ctx)
  │
  ▼
http.Client.Do(req)
  │
  ▼
http.Transport.RoundTrip(req)
  │
  ├── 1. 获取连接
  │     ┌──────────────────────────────────────┐
  │     │  Transport 连接池                     │
  │     │                                      │
  │     │  idleConn map[connectKey][]*conn      │
  │     │    ├─ "http|example.com:80" → [c1,c2]│
  │     │    └─ "https|api.com:443" → [c3]     │
  │     │                                      │
  │     │  connRequest 排队等待                 │
  │     │    └─ 使用 chan 实现先到先得           │
  │     └──────────────────────────────────────┘
  │
  ├── 2. 发送请求 → conn.Write(req)
  │     └─ ctx 超时 → net.Conn.SetWriteDeadline(t)
  │
  ├── 3. 读取响应 → conn.Read(resp)
  │     └─ ctx 超时 → net.Conn.SetReadDeadline(t)
  │
  └── 4. 归还连接
        ├─ resp.Body.Close() → 连接放回 idle pool
        └─ 连接异常 → 丢弃
```

**关键配置**（`http.Transport` 默认值）：

| 参数 | 默认值 | 含义 |
|------|-------|------|
| `MaxIdleConns` | 100 | 全局最大空闲连接 |
| `MaxIdleConnsPerHost` | 2（≤Go 1.15） | 每个 host 最大空闲连接 |
| `MaxConnsPerHost` | 0（无限制） | 每个 host 最大连接数 |
| `IdleConnTimeout` | 90s | 空闲连接保活时间 |
| `TLSHandshakeTimeout` | 10s | TLS 握手超时 |

**常见性能陷阱**：`MaxIdleConnsPerHost=2` 是瓶颈 — 高 QPS 下，只有 2 个空闲连接可复用，其余请求必须建立新 TCP 连接。

#### fasthttp 为什么快 2-3 倍

| 对比维度 | net/http | fasthttp |
|---------|----------|----------|
| 内存分配 | 每次请求分配新对象 | 对象池（sync.Pool）复用 |
| Request/Response 生命周期 | GC 回收 | `AcquireRequest()` / `ReleaseRequest()` 手动管理 |
| Header 存储 | `map[string][]string`（哈希表） | 连续 `[]byte`（零分配解析） |
| 连接管理 | Transport 连接池 | 自定义连接池 + 管道化 |
| URI 解析 | 每次 Parse | 就地解析（不分配） |

**fasthttp 对象池机制**：

```
fasthttp.AcquireRequest()              // 从 sync.Pool 取（或新建）
  │
  ├── req.SetRequestURI(url)
  ├── req.Header.SetMethod("GET")
  ├── client.DoDeadline(req, resp, deadline)
  │
  ▼
fasthttp.ReleaseRequest(req)           // 归还到 sync.Pool（重置字段）
fasthttp.ReleaseResponse(resp)
```

**sync.Pool 原理**：
- 每个 P（GOMAXPROCS）维护一个本地 pool，无需加锁
- `Get()` 优先取本地 pool → 本地空则从其他 P 偷取或新建
- `Put()` 存回本地 pool
- GC 时清空所有 pool 中的对象（防止内存泄漏）

**关键注意事项**：fasthttp 的 Request/Response 对象被归还池后，其内部 buffer 会被重置。如果要在 `Do()` 之后继续使用响应数据，**必须拷贝**：

```go
bodyCopy := make([]byte, len(resp.Body()))
copy(bodyCopy, resp.Body())
```

本库已在 `do()` 中完成拷贝，调用方拿到的 `Response.Body` 是安全副本。

#### Context 超时在两种实现中的表达

```
net/http:
  req.WithContext(ctx)  → ctx 的 deadline 自动传播到 TCP 读写

fasthttp:
  ctx.Deadline() → 提取 deadline → client.DoDeadline(req, resp, deadline)
  注意：DoDeadline 不会自动中止正在进行的 I/O，只在开始前检查
        执行后检查 ctx.Err() 作为补偿
```

### 切换指南

从 `net/http` 切换到 `fasthttp` 只需改两处：

```diff
- client := httpclient.New(httpclient.WithBaseURL(url))
+ client := httpclient.NewFast(httpclient.WithFastBaseURL(url))

- httpclient.WithTimeout(d)  →  httpclient.WithFastTimeout(d)
- httpclient.WithHeader(k,v) →  httpclient.WithFastHeader(k,v)
```

返回值类型仍是 `*httpclient.Response`，业务代码无需改动。

### 性能对比

| 指标 | `net/http` Client | `fasthttp` FastClient |
|------|------------------|----------------------|
| 吞吐量 | ~80K req/s | ~200K+ req/s |
| 内存分配 | 每次请求分配新对象 | sync.Pool 复用，接近零分配 |
| HTTP/2 | ✅ 原生支持 | ❌ 不支持（仅 HTTP/1.1） |
| 连接池 | `http.Transport` 内置 | `MaxConnsPerHost` 控制 |

---

---

## TCP 客户端

### 基本用法

```go
import "github.com/mysunshines/gocommon/tcp"

client, err := tcp.New("localhost:8080",
    tcp.WithReadTimeout(30 * time.Second),
    tcp.WithWriteTimeout(30 * time.Second),
    tcp.WithKeepAlive(true),
)
defer client.Close()

// 发送数据并等待响应
resp, err := client.Send(ctx, []byte("hello"))

// 发送带长度前缀的数据（自定义协议）
resp, err := client.SendWithLength(ctx, data)

// 仅发送不等待响应（单向通知、心跳）
err = client.SendRaw(ctx, []byte("ping"))

// 发送后关闭连接（适合一次性请求）
resp, err = client.SendAndClose(ctx, data)

// 主动读取（不发送）
resp, err = client.Read(ctx)

// 检查连接存活
if !client.IsConnected() {
    // 触发重连逻辑
}
```

### 底层原理

#### TCP 协议基础

TCP（Transmission Control Protocol）是面向连接的可靠传输协议。数据在不同层次会被逐层封装：

```
┌─────────────────────────────────────────────────┐
│        应用层数据（业务 payload）                    │
├─────────────────────────────────────────────────┤
│         传输层：TCP 头                            │
│  ┌──────┬──────┬──────┬──────┬──────┬──────┐    │
│  │源端口 │目的端口│ 序列号 │ 确认号 │ 标志位 │窗口大小│   │
│  └──────┴──────┴──────┴──────┴──────┴──────┘    │
├─────────────────────────────────────────────────┤
│         网络层：IP 头（源IP、目的IP、TTL 等）        │
├─────────────────────────────────────────────────┤
│         链路层：以太网帧头 + 帧尾 CRC               │
└─────────────────────────────────────────────────┘
```

关键字段：
- **序列号（Sequence Number）**：每个字节一个序号，用于有序交付和去重
- **确认号（Acknowledgment Number）**：告知发送方"此序号之前的数据已收到"
- **标志位**：SYN（握手）、ACK（确认）、FIN（挥手）、RST（重置）、PSH（立即推送）

#### 三次握手（建立连接）

`net.DialTimeout("tcp", address, timeout)` 触发内核完成三次握手：

```
Client                          Server
  │                               │
  │─── SYN, Seq=x ───────────────>│  ① 客户端发送 SYN
  │                               │
  │<── SYN+ACK, Seq=y, Ack=x+1 ──│  ② 服务器确认并回复 SYN
  │                               │
  │─── ACK, Seq=x+1, Ack=y+1 ───>│  ③ 客户端确认
  │                               │
  │       连接建立，开始传输数据       │
```

深度细节：
- SYN 包中携带 MSS（最大分段大小）协商，通常为 MTU-40 = 1460 字节
- 握手过程中内核维护**半连接队列（SYN Queue）**和**全连接队列（Accept Queue）**
- SYN Flood 攻击利用半连接队列耗尽，内核通过 `tcp_syncookies=1` 防御
- `net.DialTimeout` 在 Go 中通过 `connect()` 系统调用实现，超时由 SO_SNDTIMEO 控制

#### 四次挥手（断开连接）

`conn.Close()` 触发四次挥手：

```
Client（主动关闭）              Server（被动关闭）
  │                               │
  │─── FIN, Seq=u ───────────────>│  ① 客户端：我要关闭了
  │                               │
  │<── ACK, Seq=v, Ack=u+1 ──────│  ② 服务器：知道了（半关闭状态）
  │                               │    Server 仍可发送剩余数据
  │                               │
  │<── FIN, Seq=w, Ack=u+1 ──────│  ③ 服务器：我也关闭
  │                               │
  │─── ACK, Seq=u+1, Ack=w+1 ───>│  ④ 客户端：确认
  │                               │
  │   TIME_WAIT (2MSL ≈ 60s)      │
```

深度细节：
- 主动关闭方进入 **TIME_WAIT** 状态，持续 2MSL（Maximum Segment Lifetime，通常 60 秒），确保最后的 ACK 能到达、且旧连接报文在网络中消亡
- 大量 TIME_WAIT 会导致端口耗尽（`net.ipv4.tcp_tw_reuse=1` 可复用）
- `tcpConn.SetLinger(0)` 可发送 RST 跳过 TIME_WAIT，但可能丢数据
- 被动关闭方进入 **CLOSE_WAIT** 状态，若应用层忘记 `Close()` 会导致 CLOSE_WAIT 泄漏

#### 本库核心设计

**1. 连接管理与自动重连（`connect` / `reconnect`）**

```
New(address)
  └─ net.DialTimeout("tcp", address, 10s)
       └─ tcpConn.SetKeepAlive(true)       ← 启用心跳
       └─ tcpConn.SetKeepAlivePeriod(30s)  ← 30 秒一次探测

Send() 发送失败时：
  └─ reconnect()                           ← 自动重连
       └─ conn.Close()  (关闭旧连接，发送 FIN)
       └─ connect()     (重新三次握手)
```

**2. 并发安全（`sync.RWMutex`）**

读操作（`Send`、`Read`、`IsConnected`）使用 `RLock`，多个 goroutine 可并发读取；写操作（`reconnect`、`Close`）使用 `Lock`，保证连接替换时的原子性。

**3. 超时控制（Deadline 机制）**

```
Send() {
    conn.SetWriteDeadline(now + writeTimeout)   ← 写超时
    conn.Write(data)
    conn.SetReadDeadline(now + readTimeout)     ← 读超时
    io.ReadAll(conn)
}
```

`SetDeadline` 是 Go net 包的绝对时间超时机制，底层通过 `SO_RCVTIMEO` / `SO_SNDTIMEO` 或 epoll 定时器实现。超时后 I/O 操作返回 `i/o timeout` 错误，TCP 连接本身不会被关闭。

**4. 自定义协议（`SendWithLength`）**

解决 TCP 流式传输中的**粘包/拆包**问题：

```
普通 Send：
  发送 "hello" + "world" → 对端可能收到 "helloworld"（粘包）
                         → 或 "hel" + "loworld"（拆包）

SendWithLength（Length-Prefixed）：
  ┌──────────┬──────────────┐
  │ 4 字节长度 │  数据 payload │
  │ (BigEndian)│              │
  └──────────┴──────────────┘

  发送流程：
    1. binary.BigEndian.PutUint32 写入数据长度
    2. 拼接 [长度前缀 + 数据] 发送
    3. io.ReadFull 精确读取 4 字节长度 → 再读 N 字节数据

  保证每次读取到完整的一条消息帧，不会多读或少读。
```

**5. 保活探测（TCP KeepAlive）**

```
SetKeepAlive(true)  → SO_KEEPALIVE = 1
SetKeepAlivePeriod(30s) → TCP_KEEPIDLE = 30s

空闲 30s 后内核发送探测包：
  t=30s → 发送第 1 个 KeepAlive 探测（空 ACK）
  t=30+75s → 未收到 ACK，发送第 2 个探测
  t=30+75+75s → ...直到 tcp_keepalive_probes 次（默认 9 次）
  全部失败 → ETIMEDOUT，连接关闭
```

注意：TCP KeepAlive 是**操作系统内核**级别的机制，应用层通过 `IsConnected()` 做主动探活（尝试读取 1 字节，超时 100ms 则判定断开）。

---

## UDP 客户端

### 基本用法

```go
import "github.com/mysunshines/gocommon/udp"

client, err := udp.New("localhost:8080",
    udp.WithLocalAddress("0.0.0.0:0"),  // 指定本地端口（可选）
    udp.WithReadTimeout(30 * time.Second),
    udp.WithWriteTimeout(30 * time.Second),
    udp.WithBufferSize(4096),
)
defer client.Close()

// 单向发送（发后即忘）
err := client.Send(ctx, []byte("hello"))

// 发送到指定地址
err = client.SendTo(ctx, "192.168.1.100:8080", data)

// 发送并接收响应
resp, err := client.SendAndReceive(ctx, data)

// 异步发送（不阻塞）
client.SendAsync(ctx, data)

// 主动接收数据
data, remoteAddr, err := client.Receive(ctx)

// 广播（255.255.255.255）
err = client.Broadcast(ctx, 8080, data)

// 组播（224.0.0.0 ~ 239.255.255.255）
err = client.Multicast(ctx, "224.0.0.1:9999", data)

// 创建服务端监听连接
conn, err := udp.ServerConn("0.0.0.0:8080")
```

### 底层原理

#### UDP 协议基础

UDP（User Datagram Protocol）是**无连接**、**不可靠**的传输协议。与 TCP 的核心差异：

| 特性 | TCP | UDP |
|------|-----|-----|
| 连接状态 | 面向连接（有状态） | 无连接（无状态） |
| 可靠性 | 确认+重传，有序交付 | 不确认，不重传，可能乱序 |
| 传输单位 | 字节流（Stream） | 数据报（Datagram） |
| 头部大小 | 20 字节 | 8 字节 |
| 握手 | 三次握手 | 无 |
| 流量控制 | 滑动窗口 | 无 |
| 拥塞控制 | 慢启动/拥塞避免/快速重传 | 无 |
| 适用场景 | HTTP、数据库、文件传输 | DNS、音视频、游戏、IoT |

#### UDP 报文结构

```
 0      7 8     15 16    23 24    31
┌────────┬────────┬────────┬────────┐
│ 源端口号 │ 目的端口号│   长度   │  校验和  │   ← 8 字节头部
│ (2B)   │ (2B)   │ (2B)   │ (2B)   │
├────────┴────────┴────────┴────────┤
│                                    │
│          数据（应用层 payload）       │   ← 最大 65507 字节
│                                    │      (65535 - 8 UDP头 - 20 IP头)
└────────────────────────────────────┘
```

关键字段：
- **长度**：UDP 头 + 数据的字节总数（最小 8，即仅头部）
- **校验和**：覆盖 IP 伪首部 + UDP 头 + 数据，IPv4 可选（可选），IPv6 强制
- 没有序列号、确认号、窗口大小——极简设计

#### 无连接的本质

UDP 没有"连接"对象——`net.ListenUDP` 实际上只是**绑定了一个本地端口**，内核在该端口上接收所有数据报：

```
应用层：                 内核网络栈：
                         
Send(data)               ┌─────────────────┐
  └─ sendto(fd, data,   │ UDP 协议栈        │
            dest_addr)   │  + 8B UDP 头     │
                         │  → IP 层添加 IP 头│
Receive()                │  → 链路层添加帧头  │
  └─ recvfrom(fd, buf)  │                 │
                         │ 收到数据报时：      │
                         │  校验和 → 端口匹配  │
                         │  → 放入 socket 缓冲区│
                         └─────────────────┘
```

每次 `sendto()` 都指定目标地址，每次 `recvfrom()` 返回来源地址——没有"会话"概念。

#### 本库核心设计

**1. 收发分离、无状态**

TCP 客户端维护一个 `net.Conn` 连接对象，收发都在同一连接上；UDP 客户端维护 `*net.UDPConn`，本质是一个绑定了本地端口的 socket，每次发送都重新解析目标地址：

```
Send() {
    addr = net.ResolveUDPAddr("udp", c.address)  ← 每次解析（可缓存优化）
    conn.WriteToUDP(data, addr)                   ← 指定目标地址发送
}

Receive() {
    buf = make([]byte, c.bufSize)                 ← 分配缓冲区
    n, remoteAddr, err = conn.ReadFromUDP(buf)    ← 返回来源地址
    return buf[:n], remoteAddr, nil
}
```

**2. 部分发送检测**

UDP 数据报作为整体发送或丢弃，理论上不会出现"部分发送"。但 `WriteToUDP` 的返回值 `n` 仍被检查，防止数据报过大被截断：

```
if n != len(data) {
    return fmt.Errorf("partial send: sent %d, expected %d", n, len(data))
}
```

UDP 单数据报理论最大为 65535 字节。超过 MTU（通常 1500 字节）的数据报会被 IP 层**分片**：
- 分片后任一 fragment 丢失 → 整个数据报丢弃（无重传）
- 实际应用中建议单包不超过 MTU 避免分片，典型为 1400 字节以内

**3. 广播与组播**

```
Broadcast（广播）：
  目标地址: 255.255.255.255（受限广播）
  机制：数据报被发送到同一广播域内的所有主机
  要求：发送方 socket 需设置 SO_BROADCAST 选项

Multicast（组播）：
  目标地址: 224.0.0.0 ~ 239.255.255.255（D 类地址）
  机制：数据报仅发送给加入该组播组的成员
  底层：IGMP 协议管理组成员关系，路由器用 PIM 等协议转发
        交换机默认将组播泛洪到所有端口（除非启用 IGMP Snooping）
```

**4. 丢包与不可靠性的应对**

UDP 不提供可靠性保障，丢包可能发生在：
- 链路层：碰撞、信号干扰
- 网络层：TTL 耗尽、路由表问题、分片丢失
- 传输层：socket 接收缓冲区满、校验和错误
- 应用层：应用处理速度跟不上到达速率

本库只提供基础封装，上层业务需自行处理：
- 序列号 + ACK 确认（仿 TCP 但更轻量）
- 超时重传（`SendAndReceive` + `readTimeout` + 外层重试）
- FEC 前向纠错（适用于音视频等可容忍少量丢包的场景）

---

## Kafka

### 基本用法

```go
import "github.com/mysunshines/gocommon/kafka"

// ========== 生产者 ==========
producer := kafka.NewProducer([]string{"localhost:9092"}, "my-topic")
defer producer.Close()

// 基础发送
err := producer.Send(ctx, []byte("key"), []byte("value"))

// 发送 JSON
err = producer.SendJSON(ctx, "user-key", map[string]interface{}{
    "username": "alice",
    "action":   "login",
})

// 带 Header 发送（用于链路追踪）
err = producer.SendWithHeaders(ctx, []byte("key"), []byte("value"),
    map[string]string{
        "trace-id": "abc123",
        "source":   "user-service",
    })

// 批量发送（减少网络往返）
err = producer.SendBatch(ctx, []kafka.Message{
    {Key: []byte("k1"), Value: []byte("v1")},
    {Key: []byte("k2"), Value: []byte("v2")},
})

// 配置子链式调用
producer.WithBalancer("least")           // 负载均衡：LeastBytes
producer.WithBalancer("roundrobin")      // 负载均衡：RoundRobin
producer.WithBalancer("hash")            // 负载均衡：Hash（相同 Key 进同一分区）
producer.WithBatch(500, 50*time.Millisecond)  // 批量：500 条或 50ms 触发
producer.WithAsync()                     // 异步模式（不等待 Broker 确认）

// ========== 消费者 ==========
consumer := kafka.NewConsumer(
    []string{"localhost:9092"},           // Broker 列表
    "my-topic",                           // 主题
    "my-group",                           // 消费者组 ID
)
defer consumer.Stop()

// 方式一：Handler 回调模式
consumer.AddHandler(func(ctx context.Context, key, value []byte) error {
    fmt.Printf("收到: key=%s, value=%s\n", key, value)
    return nil
})
consumer.AddHandler(func(ctx context.Context, key, value []byte) error {
    // 第二个处理器（消息会广播给所有 Handler）
    return saveToDatabase(ctx, key, value)
})
go consumer.Start(ctx)

// 方式二：Channel 模式（更灵活）
msgChan, _ := consumer.StartWithChannel(ctx)
for msg := range msgChan {
    fmt.Printf("分区: %d, 偏移: %d, Key: %s\n",
        msg.Partition, msg.Offset, msg.Key)
}

// 消费者统计
stats := consumer.Stats()
fmt.Printf("已消费: %d 条, 字节数: %d, 错误: %d\n",
    stats.Messages, stats.Bytes, stats.Errors)

// ConsumerGroupHandler 接口（自定义消费逻辑）
kafka.SimpleHandler{
    Handler: func(ctx context.Context, key, value []byte) error {
        return process(ctx, key, value)
    },
}
```

### 底层原理

#### Kafka 核心架构

```
                      Kafka Cluster
┌──────────────────────────────────────────────────────┐
│                     Zookeeper / KRaft                  │
│              (集群元数据 / Controller 选举)              │
├──────────────────────────────────────────────────────┤
│  Broker 0           Broker 1           Broker 2       │
│  ┌──────────┐      ┌──────────┐      ┌──────────┐    │
│  │ Topic-A  │      │ Topic-A  │      │ Topic-A  │    │
│  │  P0 [L]  │◄─────│  P0 [F]  │◄─────│  P0 [F]  │    │
│  │  P1 [F]  │      │  P1 [L]  │      │  P1 [F]  │    │
│  └──────────┘      └──────────┘      └──────────┘    │
└──────────────────────────────────────────────────────┘
        ▲  ▲              ▲                  ▲
        │  │              │                  │
   Producer 1      Producer 2    Consumer Group "my-grp"
                                        │
                                  ┌─────┴─────┐
                                  │            │
                             Consumer 1    Consumer 2
                              (消费 P0)     (消费 P1)
```

核心概念：
- **Topic**：消息的逻辑分类，如 `order-events`
- **Partition**：Topic 的物理分片，每个分区是一个有序的、不可变的消息序列
- **Segment**：分区在磁盘上的存储单元，由 `.log`（数据）+ `.index`（偏移索引）+ `.timeindex`（时间索引）组成
- **Offset**：消息在分区内的唯一递增序号（64 位整数）
- **Replica**：分区的副本，Leader 处理读写，Follower 从 Leader 同步
- **ISR（In-Sync Replicas）**：与 Leader 保持同步的副本集合

#### 生产者写入流程（本库 `Producer.Send` 全链路）

```
1. 客户端侧（本库）：
   Send(ctx, key, value)
     └─ kafka.Writer.WriteMessages(ctx, msg)
          │
          ▼
   序列化 → 通过 Metadata 请求获取分区 Leader
          → 根据 Balancer 选择目标分区：
            • LeastBytes：选当前写入量最少的分区
            • RoundRobin：轮询分配
            • Hash：hash(key) % partition_count（同 Key 保序）
          │
          ▼
   构建 ProduceRequest → TCP 发送给 Broker

2. Broker 侧处理：
   接收 ProduceRequest
     ├─ 校验：Topic 存在？分区属于当前 Broker？
     ├─ 写入 OS Page Cache（顺序追加，不使用 fsync）
     │   └─ Segment 文件：00000000000000012345.log
     │       [offset=12345, msg1][offset=12346, msg2]...
     ├─ 更新 LEO（Log End Offset）
     └─ 提交到副本同步队列

3. 副本同步（ISR 机制）：
   Follower 发起 FetchRequest → Leader 返回新数据
     → Follower 写入本地 Page Cache
     → 更新自己的 LEO
   Leader 的 HW（High Watermark）= min(所有 ISR 的 LEO)
   仅 HW 之前的消息对 Consumer 可见

4. 返回 ACK（根据 acks 配置）：
   acks=0：不等待确认（最快，可能丢消息）
   acks=1：Leader 写入即返回（默认，可能丢已写未同步的消息）
   acks=all（-1）：等待所有 ISR 确认（最可靠）
```

**Page Cache 写**（关键优化）：Kafka 不直接执行 `fsync`，数据先写入 OS Page Cache：
- 读取直接命中 Page Cache，避免磁盘 I/O
- 脏页由 OS 调度刷盘（`vm.dirty_ratio` / `vm.dirty_background_ratio`）
- 大量顺序写入 ≈ 内存速度（磁盘只需跟上平均吞吐量即可）

#### 消费者消费流程（本库 `Consumer.Start` 全链路）

```
1. 客户端侧（本库）：
   Start(ctx)
     └─ for loop { reader.ReadMessage(ctx) }
           │
           ▼
   kafka-go 库维护与 Coordinator 的心跳
           │
           ▼
   向 Coordinator 发送 JoinGroup 请求
     → Coordinator（GroupCoordinator）选举 Consumer Leader
     → Leader 执行分区分配策略（Range / RoundRobin / Sticky）
     → 各 Consumer 获得分配的分区

2. 拉取消息：
   FetchRequest → Broker
     ├─ 指定 fetch.min.bytes / fetch.max.wait.ms
     ├─ Broker 返回 HW 之前且 ≥ offset 的消息
     └─ 更新 Consumer 的 position（当前消费位置）

3. 位移提交（Offset Commit）：
   自动提交（enable.auto.commit=true）：
     → 定时（auto.commit.interval.ms）提交已处理的 offset
     → 风险：提交后崩溃可能丢消息（at-most-once）
   
   手动提交（本库使用 kafka-go 默认手动提交）：
     → 业务 handler 返回 nil → Reader.CommitMessages() → 提交 offset
     → 返回 error → 不提交 → 重启后重试（at-least-once）

4. Rebalance（重平衡）：
   触发条件：
     • 消费者加入/离开组
     • Topic 分区数变化
     • 心跳超时（session.timeout.ms）
   过程：
     ① Coordinator 通知所有消费者重新 JoinGroup
     ② 所有消费者暂停消费（Stop-The-World）
     ③ 重新分配分区
     ④ 消费者从上次提交的 offset 恢复
   本库使用 session.timeout.ms（默认 45s）作为心跳超时基准
```

#### 批处理与异步发送

```
WithBatch(size, timeout)：
  ┌────────────────────────────────┐
  │  内存批量缓冲区（Writer 内部）     │
  │  [msg1][msg2][msg3]...         │
  │                                │
  │  触发发送条件（任一满足即发送）：    │
  │    1. 缓冲区 >= BatchSize 条    │
  │    2. 距上次发送 >= BatchTimeout │
  └────────────────────────────────┘
  
  优点：
    • 减少 TCP 请求/响应往返次数
    • 提高单次写入的数据量，减少磁盘 I/O 次数

WithAsync()：
  WriteMessages 立即返回，不等待 Broker 响应
  → 适合"发后即忘"场景（日志、埋点）
  → 缺点：发送失败无感知
```

---

以下是完整的 Kafka 消息生命周期：

```
Producer.Send()                           Consumer.ReadMessage()
     │                                          ▲
     ▼                                          │
┌─────────┐    TCP     ┌───────────┐    TCP    ┌──────────┐
│ Producer│ ────────→  │  Broker   │ ────────→ │ Consumer │
│ (本库)  │            │ (Leader)  │           │  (本库)   │
└─────────┘            └───────────┘           └──────────┘
     │                      │                      │
     ├─ 序列化              ├─ Page Cache           ├─ 反序列化
     ├─ 分区选择            ├─ 副本同步            ├─ Handler 处理
     ├─ 批量/异步           ├─ HW 更新             ├─ 手动 Commit
     └─ 压缩(可选)          └─ Segment 文件         └─ Rebalance
```

---

## Metrics

### 基本用法

```go
import "github.com/mysunshines/gocommon/metrics"

// 初始化（注册所有指标到 DefaultRegisterer），传入服务名用于带 service 标签的指标
metrics.Init(constants.ServiceNameArticle)

// ========== HTTP 请求指标（由 MetricsMiddleware 自动记录）==========
metrics.RecordRequest("GET", "/api/v1/users", 200, duration)
metrics.IncrementInFlight()  // 请求开始时 +1
// 请求结束时 RecordRequest 会 DecrementInFlight

// ========== 错误指标 ==========
metrics.RecordError("timeout")
metrics.RecordError("connection_refused")
metrics.RecordError("validation_failed")

// ========== 数据库指标 ==========
metrics.RecordDBOperation("create", "success")
metrics.RecordDBOperation("query", "fail")
metrics.RecordSlowQuery("SELECT * FROM articles WHERE ...", 523*time.Millisecond)

// ========== 缓存指标 ==========
metrics.RecordCacheOperation("get", "hit")
metrics.RecordCacheOperation("get", "miss")
metrics.RecordRedisHit(true)
metrics.RecordHotKey("article:hot:42")

// ========== RPC 调用指标 ==========
start := time.Now()
resp, err := userClient.GetUser(ctx, uid)
metrics.RecordRPCRequest("user-service", "GetUser", 
    map[bool]string{true: "success", false: "fail"}[err == nil],
    time.Since(start))

// ========== 系统健康 ==========
metrics.SetServiceHealth("user-service", true)
metrics.RecordPanic("user-service")

// 每 30 秒更新系统指标
go func() {
    ticker := time.NewTicker(30 * time.Second)
    for range ticker.C {
        metrics.UpdateSystemMetrics()
    }
}()
```

### Prometheus 指标类型

| 类型 | 应用 | 示例 |
|------|------|------|
| **Counter** | 只增不减的累计值 | 请求总数、错误总数、慢查询数 |
| **Gauge** | 可增可减的瞬时值 | 内存使用、goroutine 数、健康状态 |
| **Histogram** | 观察值的分布 | 请求延迟分布（P50/P95/P99） |
| **Summary** | 类似 Histogram，客户端计算分位数 | （本库未使用） |

### 所有暴露的指标

```
HTTP 层:
  http_requests_total{method, endpoint, status}         ← Counter
  http_request_duration_seconds{method, endpoint}       ← Histogram
  request_duration_seconds{method, endpoint}            ← Histogram (请求级)
  requests_total                                        ← Counter (所有请求)
  requests_in_flight                                     ← Gauge (并发请求数)
  errors_total{type}                                     ← Counter

数据库:
  db_operations_total{operation, status}                ← Counter
  mysql_slow_queries_total                               ← Counter

缓存:
  cache_operations_total{operation, status}             ← Counter
  redis_cache_hits_total{service}                       ← Counter
  redis_cache_misses_total{service}                     ← Counter
  redis_hot_keys_total{key}                             ← Counter

  # 命中率比率（推荐在 PromQL 中计算，而非直接采集 0/1 Gauge）：
  # sum(rate(redis_cache_hits_total[5m])) / clamp_min(sum(rate(redis_cache_hits_total[5m])) + sum(rate(redis_cache_misses_total[5m])), 0)
  # 按服务拆分：sum(rate(redis_cache_hits_total{service="article-service"}[5m])) / ...

RPC:
  rpc_requests_total{service, method, status}           ← Counter
  rpc_request_duration_seconds{service, method}         ← Histogram

系统:
  service_health{service}                                ← Gauge (1/0)
  goroutine_count                                        ← Gauge
  memory_usage_bytes                                     ← Gauge
  panic_counter_total{service}                           ← Counter
```

### 底层原理

#### Prometheus 指标模型

```
                 ┌──────────────┐
  Instrumented──>│  Metric 对象   │──> Register ──> │ DefaultRegisterer │
    代码         │  + Labels     │                   │ (全局注册表)       │
                 └──────────────┘                   └───────────────────┘
                                                            │
                                            HTTP GET /metrics (Prometheus 抓取)
                                                            │
                                                  ┌───────────────────┐
                                                  │  Prometheus Server  │
                                                  │  (定期 pull 指标)    │
                                                  └───────────────────┘
```

#### Counter vs Gauge vs Histogram 的实现

```go
// Counter: 原子累加（底层用 sync/atomic 或 mutex）
httpRequestsTotal.WithLabelValues("GET", "/users", "200").Inc()
// 内部: labelValues → hash → metricVec 中查找或新建 *counter
// 然后 atomic.AddUint64(&counter.val, 1)

// Gauge: 直接设置值
requestsInFlight.Inc()   // atomic.AddUint64
requestsInFlight.Dec()   // atomic.AddUint64(-1)
serviceHealth.WithLabelValues("user-service").Set(1.0)
// 内部: 将 float64 转 math.Float64bits → uint64 → atomic.StoreUint64

// Histogram: 落入 bucket
requestDuration.WithLabelValues("GET", "/users").Observe(0.15)
// 内部:
//   1. 遍历预设 buckets: [.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10]
//   2. 对每个 bucket ≤ observed_value 的 counter +1
//   3. sum += observed_value, count += 1
//   4. 暴露: _bucket{le="0.1"}, _bucket{le="0.25"}, _sum, _count
```

**Histogram buckets 设计哲学**：

```go
Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}
```

- 从 1ms 到 10s，覆盖了从极快到极慢的全范围
- 对数据传输优化：每个 bucket 都需传输其 count，bucket 数量过多 → /metrics 响应体积膨胀
- PromQL 计算分位数：`histogram_quantile(0.99, rate(http_request_duration_seconds_bucket[5m]))`

#### promauto 自动注册

```go
// 在包级 var 声明时自动注册（init() 之前通过 promauto 完成）
var httpRequestsTotal = promauto.NewCounterVec(
    prometheus.CounterOpts{Name: "http_requests_total", Help: "..."},
    []string{"method", "endpoint", "status"},
)
```

`promauto` 是对 `prometheus.MustRegister` 的封装：
- 自动向 `prometheus.DefaultRegisterer` 注册
- 如果注册失败（如指标名冲突）→ **panic**（fail-fast）
- 无需显式调用 `Register()`，声明即注册

#### 为什么 labels 是一个 []string 切片而不是 map

```go
// Prometheus 的做法:
NewCounterVec(..., []string{"method", "endpoint", "status"})

// 不是:
NewCounterVec(..., map[string]string{"method":"", "endpoint":""})
```

**原因**：
- Labels 是有序的 — `WithLabelValues("GET", "/users", "200")` 按声明顺序匹配
- `[]string` 比 `map[string]string` 更轻量（无哈希计算、无冲突处理）
- Prometheus 的时间序列由 `metric_name{label1=val1, label2=val2}` 唯一标识 — label 顺序在存储层不敏感，但在 client 层需要确定性

#### 指标的最佳实践

1. **避免高基数标签**：不要将 `user_id`、`request_id` 作为 label — 每个唯一组合创建一个时间序列，消耗内存
2. **使用 Counter 记录错误，按 `type` 分类**（不按具体用户）
3. **Histogram 不只是延迟**：也可衡量请求大小、响应大小等任何分布
4. **`_in_flight` 是重要的排障指标**：如果该值持续很高，说明存在请求堆积

---

## 中间件

中间件分两套：**Gin HTTP 中间件**（服务自身 HTTP 入口）与 **gRPC 拦截器**（服务间/gateway→服务的 gRPC 入口）。业务流量主体走 gRPC，gRPC 拦截器是鉴权真正落地的地方。

### gRPC 拦截器（一元服务端）

```go
import "github.com/mysunshines/gocommon/middleware"

grpcServer := grpc.NewServer(grpc.ChainUnaryInterceptor(
    yourTimeoutAndBreakerInterceptor,     // 自定义：超时 + gobreaker（各服务在 main.go 组装）
    middleware.GRPCAuthInterceptor(),     // 鉴权：校验 metadata authorization，注入身份到 ctx
    middleware.GRPCMetricsInterceptor(svc),// 指标：rpc_requests_total 等
    middleware.GRPCLoggingInterceptor(),  // 日志：从 metadata 提取 traceID 串联链路
))

// handler 内取身份（写方法在开头调用，未登录返回 Unauthenticated）
uid, err := middleware.RequireGRPCAuth(ctx)      // (uint, error)
role, _ := middleware.GetGRPCRole(ctx)            // 角色判断（管理员接口用）
token, ok := middleware.GetGRPCToken(ctx)         // 原始 JWT（供 AuthForwardInterceptor 透传）
```

**GRPCAuthInterceptor 的行为**：携带 token 时校验签名，无效直接 Unauthenticated；未携带 token 时放行（匿名），是否要求登录由 handler 调 `RequireGRPCAuth` 决定——公开方法（列表/详情）不强制，写方法必须强制。

**关键约定**：gRPC handler 禁止信任请求体的 user_id 字段（可伪造，IDOR 漏洞），身份一律取自 `RequireGRPCAuth(ctx)`；下游调用由 `grpcclient` 的 AuthForwardInterceptor 自动透传 token。

### Gin HTTP 中间件基本用法

```go
import "github.com/mysunshines/gocommon/middleware"

r := gin.Default()

// 最外层：超时 + 请求体大小限制 + TraceID
r.Use(middleware.ValidateRequestMiddleware())  // 限制请求体最大 1MB
r.Use(middleware.TimeoutMiddleware(10 * time.Second))
r.Use(middleware.TraceMiddleware())            // 注入 X-Trace-ID

// CORS
r.Use(middleware.CORSMiddleware())

// Panic 恢复
r.Use(middleware.RecoveryMiddleware())

// JWT 认证（鉴权模式：缺失/无效 token 返回 401）
r.Use(middleware.AuthMiddleware(true))

// 上下文传播（将 JWT 中的 user_id 注入 context）
r.Use(middleware.ContextMiddleware())

// 请求日志 + Prometheus 指标
r.Use(middleware.LoggingMiddleware())
r.Use(middleware.MetricsMiddleware())

// 限流（需先初始化）
middleware.InitRateLimiter(&conf.RateLimit)
r.Use(middleware.RateLimitMiddleware())

// 业务 handler 获取认证信息
func handler(c *gin.Context) {
    userID, _ := middleware.GetUserIDFromContext(c)
    username := middleware.GetUsernameFromContext(c)
}
```

### 底层原理

#### Gin 中间件洋葱模型

```
    Request ────────────────────────────────> Response
    
    TimeoutMiddleware(ctx, 10s)
      ValidateRequestMiddleware (MaxBytesReader)
        TraceMiddleware (X-Trace-ID)
          CORSMiddleware
            RecoveryMiddleware (defer + recover)
              AuthMiddleware (解析令牌)
                ContextMiddleware (注入 context)
                  LoggingMiddleware
                    MetricsMiddleware
                      RateLimitMiddleware
                        │          ▲
                        ▼          │
                     Handler 执行业务代码
```

**执行机制**：
- `c.Next()` — 调用下一个中间件，之后回到当前位置（递归调用链）
- `c.Abort()` — 终止后续中间件和 handler，直接返回
- 中间件在 `c.Next()` **之前**的代码 → 请求预处理；**之后**的代码 → 响应后处理

#### 限流中间件：Token Bucket 算法

```go
type RateLimiter struct {
    limiters map[string]*rate.Limiter  // IP → 令牌桶
    mu       sync.RWMutex
}

func (rl *RateLimiter) Allow(key string) bool {
    limiter := rl.GetLimiter(key)      // 惰性创建每个 IP 的令牌桶
    return limiter.Allow()
}
```

**Token Bucket 原理**（`golang.org/x/time/rate`）：

```
令牌桶模型：
  ┌──────────────────────────┐
  │     令牌桶（burst=2000）   │←── QPS=1000，每秒补充 1000 个令牌
  │  Token: 1500             │
  │                          │
  └──────────────────────────┘
           ↓ 消耗 1 token
    请求 → Allow() → true (允许，token ≥ 1)
            Allow() → true
            ... (令牌耗尽)
            Allow() → false (限流)

桶大小 = burst (2000):
  - 应对突发：空闲时攒满 2000 token，突发 2000 QPS 不被拦截
  - 稳态限制：长时间来看，平均速率 ≤ QPS (1000)
```

**惰性创建**：每个 IP 的令牌桶在第一次请求时创建，不会预分配所有 IP 的桶。

**限流键的选择**：
- `c.ClientIP()` — 按 IP 限流（当前实现），适合防止单 IP 刷接口
- 可按 `user_id` 限流 — 防止单用户高频请求
- 可按 `endpoint` 限流 — 不同接口独立限流

#### JWT 认证中间件

```
Authorization: Bearer eyJhbGciOiJIUzI1NiIs...

AuthMiddleware(true)(c):
  1. 提取 Header: c.GetHeader("Authorization")
  2. 校验格式: "Bearer <token>"
  3. 解析 JWT: jwt.Parse(tokenString, keyFunc)
     │
     ├── keyFunc: 验证签名算法 (必须是 HMAC-SHA256)
     │            返回密钥 []byte(jwtSecret)
     │
     ├── jwt.Parse 内部:
     │   ├── 拆分: Header.Payload.Signature
     │   ├── Base64 解码 Header + Payload (无加密，仅编码)
     │   ├── 验证签名: HMAC-SHA256(Header.Payload, secret) == Signature
     │   ├── 验证过期: exp > now()
     │   └── 返回 *jwt.Token + Claims
     │
     └── 提取 Claims → c.Set("user_id", ...), c.Set("username", ...)
```

**JWT 签名验证原理**：

```
签名 = HMAC-SHA256(
    base64UrlEncode(header) + "." + base64UrlEncode(payload),
    secret
)

验证: 重新计算签名 → 与收到的 signature 比对 → 一致则合法

为什么安全:
  - 只有持有 secret 的一方才能生成合法签名
  - payload 被修改 → 签名不匹配 → 验证失败
  - 但 payload 本身是 base64 编码(非加密)，任何人都可读取内容
    → 不要把密码、银行卡号等敏感信息放在 JWT Payload 中
```

#### RecoveryMiddleware：defer + recover 机制

```go
func RecoveryMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        defer func() {
            if err := recover(); err != nil {
                metrics.RecordPanic(serviceID)    // 指标
                log.Errorf("Panic: %v", err)      // 日志
                c.JSON(500, ...)                   // 返回 500
                c.Abort()
            }
        }()
        c.Next()
    }
}
```

**`recover` 的工作原理**：
- Go 的 panic 会沿调用栈向上传播，逐层执行 defer
- `recover()` 只能在 **defer 内部直接调用** 时才能捕获 panic
- 捕获后，panic 传播链中断，程序不崩溃，goroutine 恢复正常
- `runtime.Stack()` 可以获取完整的调用栈（生产环境建议记录）

#### TimeoutMiddleware：context 超时传播

```go
func TimeoutMiddleware(timeout time.Duration) gin.HandlerFunc {
    return func(c *gin.Context) {
        ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
        defer cancel()  // 请求结束时释放 timer 资源
        c.Request = c.Request.WithContext(ctx)
        c.Next()
    }
}
```

关键步骤：
- `context.WithTimeout(parent, d)` 创建带超时的子 context
- 设置到 `c.Request` 后，所有使用 `c.Request.Context()` 的下游调用都会继承此超时
- `defer cancel()` **必须在中间件函数中返回时执行**，否则 timer 泄漏（底层 timer 不会被 GC 回收）

#### TraceMiddleware：全链路追踪的基础设施

```go
func TraceMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        traceID := c.GetHeader("X-Trace-ID")
        if traceID == "" {
            traceID = generateTraceID()  // 没有上游 TraceID 则生成新的
        }
        c.Set("X-Trace-ID", traceID)    // 存入 gin.Context
        c.Header("X-Trace-ID", traceID) // 返回给客户端
        c.Next()
    }
}
```

- **分布式追踪入口**：网关第一个中间件，确保每个请求有唯一 TraceID
- **透传**：如果请求已携带 TraceID（来自上游或网关），则复用，不重新生成
- **格式**：`20250115150405-aB3xK9pQ`（时间戳 + 8 位随机字符串），便于按时间大致排序

---

## 统一响应

### 基本用法

```go
import "github.com/mysunshines/gocommon/response"

func handler(c *gin.Context) {
    // 成功响应
    response.Success(c, data)
    response.SuccessWithMessage(c, "操作成功", data)

    // 分页
    response.SuccessPage(c, total, page, pageSize, list)

    // 通用 HTTP 错误（HTTP 状态码 ≠ 200）
    response.BadRequest(c, "参数无效")
    response.Unauthorized(c, "未登录")
    response.Forbidden(c, "无权限")
    response.NotFound(c, "资源不存在")
    response.InternalServerError(c, "服务器内部错误")
    response.TooManyRequests(c, "请求过于频繁")

    // 业务错误（HTTP 200 + 业务错误码）
    response.UserNotFound(c)       // code: 20005
    response.UserExists(c)         // code: 20001
    response.PasswordIncorrect(c)  // code: 20003
    response.InvalidToken(c)       // code: 20002
    response.TokenExpired(c)       // code: 20006
    response.ArticleNotFound(c)    // code: 30001
    response.CommentNotFound(c)    // code: 40001
    response.CommentDisabled(c)    // code: 40003
    response.InBlacklist(c)        // code: 40004
    response.PermissionDenied(c)   // code: 10003
}
```

### 响应格式

```json
// 成功
{"code": 0, "message": "success", "data": {...}}

// 分页
{"code": 0, "message": "success", "data": {
    "total": 100,
    "page": 1,
    "page_size": 10,
    "data": [...]
}}

// 通用 HTTP 错误 (HTTP 400)
{"code": 10001, "message": "参数无效"}

// 业务错误 (HTTP 200)
{"code": 20005, "message": "User not found"}
```

### 底层原理

#### 业务错误码 vs HTTP 状态码分离

```
HTTP 状态码                   业务错误码 (code)
──────────────────────────────────────────────────────────────
         │                     ├─ 0       = Success
400 BAD  │ BadRequest()        ├─ 10001   = BadRequest
400 BAD  │ ParamError()        ├─ 10001   = BadRequest
401 UNAUTH│ Unauthorized()     ├─ 10002   = Unauthorized
403 NO   │ Forbidden()         ├─ 10003   = Forbidden
404 NONE │ NotFound()          ├─ 10004   = NotFound
500 ERR  │ InternalServerError ├─ 10005   = Internal
429 MANY │ TooManyRequests()   ├─ 10008   = RateLimited
                              │
         │ UserNotFound()      ├─ 20005   = 用户不存在
200 OK   │ UserExists()        ├─ 20001   = 用户已存在
         │ ArticleNotFound()   ├─ 30001   = 文章不存在
         │ CommentNotFound()   ├─ 40001   = 评论不存在
```

**为什么要分离**：
- **HTTP 状态码** 表达的是 **传输层** 的状态（请求本身是否有问题）
- **业务错误码** 表达的是 **业务层** 的结果（业务逻辑是否成功）

**两种策略**：

| 策略 | 实现 | 适用场景 |
|------|------|---------|
| `ErrorWithStatus` | HTTP 状态码 = 业务含义（400/401/404/500） | 通用错误、网关层 |
| `Error` | HTTP 200 + 业务 code | 业务错误（用户不存在、令牌过期等） |

- `UserNotFound(c)` 返回 **HTTP 200 + code 20005**，因为请求本身是合法的，只是用户不存在
- `Unauthorized(c)` 返回 **HTTP 401**，因为请求本身缺少认证凭据
- 这种设计避免了前端需要同时解析 HTTP 状态码和 response body 来判断错误类型

#### 错误码区间分配

```
10000-19999 → 通用/网关
20000-29999 → 用户服务
30000-39999 → 文章服务
40000-49999 → 评论服务
```

- 每个服务独占 10000 个错误码，永不冲突
- 网关可以根据错误码直接判断来源微服务
- 与 gRPC proto 中的错误码定义保持对齐

#### Response 结构体设计

```go
type Response struct {
    Code    int         `json:"code"`
    Message string      `json:"message"`
    Data    interface{} `json:"data,omitempty"`  // omitempty: 空时不输出该字段
}
```

- `Data` 使用 `interface{}` 接受任意类型，由 JSON 序列化器处理
- `omitempty` 标签：错误响应不携带 `data` 字段，减少响应体积
- 不包含 `trace_id` 等运维字段在 body 中（这些在 Header 中通过 `X-Trace-ID` 传递）

#### 分页响应设计

```go
type PageResult struct {
    Total    int64       `json:"total"`
    Page     int         `json:"page"`
    PageSize int         `json:"page_size"`
    Data     interface{} `json:"data"`
}
```

命名遵循 **RESTful 分页惯例**：
- `total` — 总记录数（前端据此计算总页数 = ceil(total/page_size)）
- `page` — 当前页码（从 1 开始）
- `page_size` — 每页条数
- `data` — 当前页数据

也可以直接扩展为 cursor-based 分页（`next_cursor` / `has_more`），需要时在 `PageResult` 中添加字段即可。

---

## 依赖

```go
go get github.com/gin-gonic/gin
go get github.com/go-redis/redis/v8
go get github.com/segmentio/kafka-go
go get github.com/golang-jwt/jwt/v5
go get github.com/sirupsen/logrus
go get github.com/prometheus/client_golang
go get github.com/valyala/fasthttp
go get gorm.io/gorm
go get gorm.io/driver/mysql
go get golang.org/x/sync
go get gopkg.in/yaml.v3
go get google.golang.org/grpc
go get google.golang.org/protobuf
go get go.uber.org/zap
go get github.com/gorilla/websocket
go get github.com/minio/minio-go/v7
go get go.opentelemetry.io/otel
```

> 业务服务引用方式：go.mod 中 `require github.com/mysunshines/gocommon v1.5.x`，本地联调可加 `replace github.com/mysunshines/gocommon => ../gocommon`。
