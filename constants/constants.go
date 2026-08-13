// Package constants 定义项目中所有共用的常量
package constants

// ============================================================================
// 时间格式常量 - 使用 Go 的 reference time: Mon Jan 2 15:04:05 MST 2006
// ============================================================================
const (
	// DateTimeFormat 标准日期时间格式: 2006-01-02 15:04:05
	DateTimeFormat = "2006-01-02 15:04:05"

	// DateFormat 日期格式: 2006-01-02
	DateFormat = "2006-01-02"

	// DateTimeCompact 紧凑日期时间格式（无分隔符）: 20060102150405
	DateTimeCompact = "20060102150405"

	// DateTimeSlash 斜杠分隔的日期时间格式: 2006/01/02 15:04:05
	DateTimeSlash = "2006/01/02 15:04:05"

	// DateTimeLog 日志专用时间格式: 2006/01/02 - 15:04:05
	DateTimeLog = "2006/01/02 - 15:04:05"

	// DateTimeISO8601 ISO 8601 / RFC 3339 时间格式: 2006-01-02T15:04:05Z
	DateTimeISO8601 = "2006-01-02T15:04:05Z"

	// DateTimeWithTZ 带时区缩写的时间格式: 2006-01-02 15:04:05 MST
	DateTimeWithTZ = "2006-01-02 15:04:05 MST"
)

// ============================================================================
// 文件权限常量 (Unix fs.FileMode)
// ============================================================================
const (
	// FilePermDir 目录创建权限: rwxr-xr-x (owner读写执行, 组/其他读执行)
	FilePermDir = 0755

	// FilePermFile 文件创建权限: rw-rw-rw- (所有人可读写)
	FilePermFile = 0666
)

// ============================================================================
// 文件大小常量
// ============================================================================
const (
	// MaxHeaderBytes HTTP 请求头最大字节数: 1MB
	MaxHeaderBytes = 1 << 20

	// MaxRequestBody HTTP 请求体最大字节数: 8MB
	MaxRequestBody = 8 << 20
)

// ============================================================================
// 网络层通用常量
// ============================================================================
const (
	// DefaultNetBufSize TCP/UDP 网络读缓冲区默认大小: 4KB
	DefaultNetBufSize = 4096

	// MsgLenPrefixSize 消息长度前缀字节数 (uint32 = 4 bytes)
	MsgLenPrefixSize = 4

	// DefaultDialTimeout 连接拨号默认超时 (秒)
	DefaultDialTimeout = 10

	// DefaultKeepAlivePeriod TCP KeepAlive 探测间隔 (秒)
	DefaultKeepAlivePeriod = 30

	// ConnProbeTimeout 连接探活超时 (毫秒)
	ConnProbeTimeout = 100
)

// ============================================================================
// API 路径常量
// ============================================================================
const (
	// APIPathPrefix API 版本化路径前缀
	APIPathPrefix = "/api/v1"

	// AdminAPIPrefix 管理后台对外暴露的路径前缀，网关将 /admin-api/* 反向代理到下游服务。
	AdminAPIPrefix = "/admin-api"

	// AdminTargetPrefix DynamicAdminProxy 改写后的下游目标路径前缀（接在资源名之前）。
	AdminTargetPrefix = "/api/v1/admin"

	// HealthCheckPath 健康检查路径，Docker/Consul 探活统一使用
	HealthCheckPath = "/health"

	// MetricsPath Prometheus 指标暴露路径，运维监控统一使用
	MetricsPath = "/metrics"

	// ReadinessPath 就绪检查路径，K8s/Consul 就绪探针统一使用
	ReadinessPath = "/ready"

	// VersionPath 版本信息路径，运维查看统一使用
	VersionPath = "/version"
)

// ============================================================================
// HTTP Server 默认值
// ============================================================================
const (
	// DefaultReadTimeout HTTP 读取请求默认超时 (秒)
	DefaultReadTimeout = 30

	// DefaultReadHeaderTimeout HTTP 读取请求头默认超时 (秒)
	DefaultReadHeaderTimeout = 10

	// DefaultWriteTimeout HTTP 写入响应默认超时 (秒)
	DefaultWriteTimeout = 30

	// DefaultIdleTimeout HTTP 空闲连接默认超时 (秒)
	DefaultIdleTimeout = 120

	// DefaultHTTPRequestTimeout HTTP 中间件整体请求默认超时 (秒)
	DefaultHTTPRequestTimeout = 30
)

// ============================================================================
// gRPC Server 默认配置
// ============================================================================
const (
	// DefaultGRPCMaxConnectionIdle 空闲连接超时 (秒)
	DefaultGRPCMaxConnectionIdle = 60

	// DefaultGRPCMaxConnectionAge 连接最大存活时间 (秒)
	DefaultGRPCMaxConnectionAge = 120

	// DefaultGRPCMaxConnectionAgeGrace 优雅关闭宽限期 (秒)
	DefaultGRPCMaxConnectionAgeGrace = 30

	// DefaultGRPCMinPingInterval 客户端 Ping 最小间隔 (秒)
	DefaultGRPCMinPingInterval = 30

	// DefaultGRPCMaxConcurrentStreams 最大并发流数
	DefaultGRPCMaxConcurrentStreams = 100

	// DefaultGRPCUnaryTimeout 一元调用默认超时 (秒)
	DefaultGRPCUnaryTimeout = 10
)

// ============================================================================
// 熔断器默认配置
// ============================================================================
const (
	// DefaultCBMaxRequests 半开状态最大请求数
	DefaultCBMaxRequests = 3

	// DefaultCBInterval 熔断器循环周期 (秒)
	DefaultCBInterval = 10

	// DefaultCBTimeout 熔断器打开持续时间 (秒)
	DefaultCBTimeout = 30
)

// ============================================================================
// 服务名与 Redis Key 前缀
// ============================================================================
const (
	// ServiceNameUser 用户服务名
	ServiceNameUser = "user-service"

	// ServiceNameArticle 文章服务名
	ServiceNameArticle = "article-service"

	// ServiceNameComment 评论服务名
	ServiceNameComment = "comment-service"

	// ServiceNameGateway 网关服务名
	ServiceNameGateway = "gateway"

	// ServiceNameReport 报表服务名
	ServiceNameReport = "report-service"

	// RedisKeyPrefixUser 用户服务 Redis Key 前缀
	RedisKeyPrefixUser = "user-service:"

	// RedisKeyPrefixArticle 文章服务 Redis Key 前缀
	RedisKeyPrefixArticle = "article:"

	// RedisKeyPrefixComment 评论服务 Redis Key 前缀
	RedisKeyPrefixComment = "comment:"

	// MetricPrefixUser 用户服务 Prometheus 指标前缀
	MetricPrefixUser = "user_service"

	// MetricPrefixArticle 文章服务 Prometheus 指标前缀
	MetricPrefixArticle = "article_service"

	// MetricPrefixComment 评论服务 Prometheus 指标前缀
	MetricPrefixComment = "comment_service"
)

// ============================================================================
// Proto 版本约定
// ============================================================================
const (
	// DefaultProtoVersion proto API 默认版本号，与 /api/v1 路径风格对齐。
	// 网关 DeriveGRPCService 依赖此约定：user-service -> user.v1.UserService。
	// 新增微服务只要 proto package 使用 ${name}.v1，无需额外配置。
	DefaultProtoVersion = "v1"
)

// ============================================================================
// 下游服务 gRPC API 标识（调用侧）
//
// 微服务之间通过 grpcclient.SendRequest(api, ...) 动态调用时，api 格式为
// "<逻辑名>.<Method>"，例如 "user.v1.IsInBlacklist"。逻辑名(alias)需先通过
// grpcclient.RegisterService(alias, protoService, target) 绑定到真实 proto
// 全限定服务名与地址。
//
// 逻辑名带版本号（user.v1），与 HTTP API 的 /api/v1 风格一致；当同一下游同时提供
// v1、v2 时，分别用不同逻辑名注册即可，SendRequest 用对应前缀选择版本：
//
//	grpcclient.RegisterService(constants.UserServiceV1Alias, constants.UserServiceV1Service, addr)
//	grpcclient.RegisterService(constants.UserServiceV2Alias, constants.UserServiceV2Service, addr) // 同地址或不同部署均可
//	grpcclient.SendRequest(ctx, constants.UserServiceV1Alias+".IsInBlacklist", ...)
//	grpcclient.SendRequest(ctx, constants.UserServiceV2Alias+".SomeNewMethod", ...)
// ============================================================================
const (
	// UserServiceV1Alias 用户服务 v1 的调用侧逻辑名（SendRequest api 前缀）
	UserServiceV1Alias = "user.v1"
	// UserServiceV1Service 用户服务 v1 的真实 proto 全限定服务名
	UserServiceV1Service = "user.v1.UserService"

	// ArticleServiceV1Alias 文章服务 v1 的调用侧逻辑名（SendRequest api 前缀）
	ArticleServiceV1Alias = "article.v1"
	// ArticleServiceV1Service 文章服务 v1 的真实 proto 全限定服务名
	ArticleServiceV1Service = "article.v1.ArticleService"

	// CommentServiceV1Alias 评论服务 v1 的调用侧逻辑名（SendRequest api 前缀）
	CommentServiceV1Alias = "comment.v1"
	// CommentServiceV1Service 评论服务 v1 的真实 proto 全限定服务名
	CommentServiceV1Service = "comment.v1.CommentService"
)

// ============================================================================
// 统一错误码区间分配
//
//   10000-19999  通用/网关错误
//   20000-29999  用户服务 (User Service)
//   30000-39999  文章服务 (Article Service)
//   40000-49999  评论服务 (Comment Service)
//
// 各服务 proto 中也使用相同区间，保持两套常量对齐。
// ============================================================================
const (
	// ---- 通用错误码 (10000-19999) ----

	// ErrCodeSuccess 成功
	ErrCodeSuccess = 0

	// ErrCodeBadRequest 请求参数无效
	ErrCodeBadRequest = 10001

	// ErrCodeUnauthorized 未认证
	ErrCodeUnauthorized = 10002

	// ErrCodeForbidden 无权限
	ErrCodeForbidden = 10003

	// ErrCodeNotFound 资源不存在
	ErrCodeNotFound = 10004

	// ErrCodeInternal 通用内部错误
	ErrCodeInternal = 10005

	// ErrCodeServiceUnavailable 服务不可用
	ErrCodeServiceUnavailable = 10006

	// ErrCodeTimeout 请求超时
	ErrCodeTimeout = 10007

	// ErrCodeRateLimited 请求被限流
	ErrCodeRateLimited = 10008
)

// 各服务向网关暴露的业务错误码（与各服务 proto/errors 保持一致）
const (
	// ---- 用户服务 (20000-29999) ----
	ErrCodeUserExists          = 20001
	ErrCodeTokenInvalid        = 20002
	ErrCodePasswordIncorrect   = 20003
	ErrCodeUserInBlacklist     = 20004
	ErrCodeUserNotFound        = 20005
	ErrCodeTokenExpired        = 20006

	// ---- 文章服务 (30000-39999) ----
	ErrCodeArticleNotFound = 30001

	// ---- 评论服务 (40000-49999) ----
	ErrCodeCommentNotFound  = 40001
	ErrCodeCommentDisabled  = 40003
	ErrCodeCommentBlacklist = 40004
)

// ============================================================================
// 运维相关默认值
// ============================================================================
const (
	// DefaultLogDir 默认日志目录
	DefaultLogDir = "logs"

	// DefaultLogLevel 默认日志级别
	DefaultLogLevel = "info"

	// DefaultConfigPath 默认配置文件路径
	DefaultConfigPath = "config/config.yaml"

	// DefaultGRPCPort 默认 gRPC 端口
	DefaultGRPCPort = 9090

	// LogFileNameFmt 日志文件名格式: {service}-{date}.log
	LogFileNameFmt = "%s-%s.log"

	// LogFieldService 日志中 service 字段名
	LogFieldService = "service"

	// LogFieldEnv 日志中 env 字段名
	LogFieldEnv = "env"

	// LogFieldTraceID 日志中 traceID 字段名，所有网络 I/O 日志统一携带该字段，
	// 便于在日志系统中按 traceID 串联同一请求的跨服务/跨组件调用链。
	LogFieldTraceID = "traceID"

	// LogInitMsg 日志初始化消息
	LogInitMsg = "Logger initialized"

	// AsyncLogBufferSize 异步日志写入器的 channel 缓冲大小（条）。
	// 请求 goroutine 仅把日志投递到该缓冲后立即返回；缓冲满时丢弃而非阻塞，
	// 因此该值越大可容忍的瞬时日志洪峰越高，但占用内存也越多。
	AsyncLogBufferSize = 4096
)

// ============================================================================
// 运行环境值常量
// 各服务 / 配置中心 / 配置文件路径解析统一引用，避免散落 "test" / "production" / "dev" 等字面量。
// ============================================================================
const (
	// EnvDevelopment 开发环境（默认）
	EnvDevelopment = "development"

	// EnvTest 测试环境（如 APP_ENV=test → config_test.yaml）
	EnvTest = "test"

	// EnvProduction 生产环境（如 APP_ENV=production → config_production.yaml）
	EnvProduction = "production"

	// DefaultEnv 默认运行环境，未设置 APP_ENV 时使用 development
	DefaultEnv = EnvDevelopment
)

// ============================================================================
// 环境变量名常量
// 所有读取环境变量的位置统一引用，避免字符串拼写不一致。
// ============================================================================
const (
	// EnvLogDir 日志目录环境变量
	EnvLogDir = "LOG_DIR"

	// EnvAppEnv 应用运行环境环境变量（如 test / production）
	EnvAppEnv = "APP_ENV"

	// EnvConfigPath 配置文件路径环境变量（显式指定，优先级最高）
	EnvConfigPath = "CONFIG_PATH"

	// EnvAdvertiseAddr Consul 服务注册时使用的对外通告地址（host:port 中的 host）
	EnvAdvertiseAddr = "ADVERTISE_ADDR"

	// EnvConsulAddress Consul 连接地址（host:port）
	EnvConsulAddress = "CONSUL_ADDRESS"

	// EnvMicroRegistryAddress 微服务注册中心地址（与 CONSUL_ADDRESS 等效，Docker Compose 常用）
	EnvMicroRegistryAddress = "MICRO_REGISTRY_ADDRESS"

	// EnvGRPCPort gRPC 监听端口
	EnvGRPCPort = "GRPC_PORT"

	// ---- 数据库 ----
	EnvDBHost     = "DB_HOST"
	EnvDBPort     = "DB_PORT"
	EnvDBUser     = "DB_USER"
	EnvDBPassword = "DB_PASSWORD"
	EnvDBName     = "DB_NAME"

	// ---- Redis ----
	EnvRedisHost     = "REDIS_HOST"
	EnvRedisPort     = "REDIS_PORT"
	EnvRedisPassword = "REDIS_PASSWORD"

	// ---- 邮件（user-service）----
	EnvSMTPHost     = "SMTP_HOST"
	EnvSMTPPort     = "SMTP_PORT"
	EnvSMTPUsername = "SMTP_USERNAME"
	EnvSMTPPassword = "SMTP_PASSWORD"
	EnvSMTPFrom     = "SMTP_FROM"

	// ---- 下游服务地址（comment-service 依赖 user-service）----
	EnvUserServiceHost = "USER_SERVICE_HOST"
	EnvUserServicePort = "USER_SERVICE_PORT"

	// ---- CORS ----
	EnvCORSAllowOrigins = "CORS_ALLOW_ORIGINS"

	// ---- 对象存储 MinIO ----
	EnvMinIOEnabled          = "MINIO_ENABLED"
	EnvMinIOEndpoint         = "MINIO_ENDPOINT"
	EnvMinIOAccessKeyID      = "MINIO_ACCESS_KEY_ID"
	EnvMinIOSecretAccessKey  = "MINIO_SECRET_ACCESS_KEY"
	EnvMinIOBucket           = "MINIO_BUCKET"
	EnvMinIOPublicBaseURL    = "MINIO_PUBLIC_BASE_URL"
	EnvMinIOAutoCreateBucket = "MINIO_AUTO_CREATE_BUCKET"
)

// ============================================================================
// HTTP Header & MIME 类型常量
// ============================================================================
const (
	// ContentTypeJSON JSON 类型 MIME
	ContentTypeJSON = "application/json"

	// HeaderContentType Content-Type 请求头 key
	HeaderContentType = "Content-Type"

	// HeaderXForwardedFor 客户端真实 IP 代理转发头
	HeaderXForwardedFor = "X-Forwarded-For"

	// HeaderXRealIP 客户端真实 IP 代理转发头 (单跳)
	HeaderXRealIP = "X-Real-IP"

	// HeaderXTraceID 链路追踪 ID 请求头
	HeaderXTraceID = "X-Trace-ID"
)

// ============================================================================
// 输入校验长度限制
// ============================================================================
const (
	// MinEmailLen 邮箱最小长度
	MinEmailLen = 3

	// MaxEmailLen 邮箱最大长度 (RFC 5321)
	MaxEmailLen = 254

	// MinUsernameLen 用户名最小长度
	MinUsernameLen = 3

	// MaxUsernameLen 用户名最大长度
	MaxUsernameLen = 32

	// MinPasswordLen 密码最小长度
	MinPasswordLen = 6

	// MaxPasswordLen 密码最大长度
	MaxPasswordLen = 32
)

// ============================================================================
// JWT 认证相关
// ============================================================================
const (
	// JWTAuthScheme Bearer 认证方案前缀
	JWTAuthScheme = "Bearer "

	// JWTAuthSchemeLen Bearer 前缀长度
	JWTAuthSchemeLen = 7

	// CORSMaxAge CORS 预检请求缓存时长 (秒): 24小时
	CORSMaxAge = "86400"
)

// ============================================================================
// 用户角色
// ============================================================================
const (
	// RoleNormal 普通用户
	RoleNormal uint8 = 1
	// RoleAdmin 管理员
	RoleAdmin uint8 = 2
)
