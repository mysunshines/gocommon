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

	// DateTimeMonthDay 月日时分格式（无年份）: 01-02 15:04
	DateTimeMonthDay = "01-02 15:04"
)

// ============================================================================
// 文件权限常量 (Unix fs.FileMode)
// ============================================================================
const (
	// FilePermDir 目录创建权限: rwxr-xr-x (owner读写执行, 组/其他读执行)
	FilePermDir = 0755

	// FilePermFile 文件创建权限: rw-r--r-- (owner 读写, 组/其他只读)
	FilePermFile = 0644
)

// ============================================================================
// 文件大小常量
// ============================================================================
const (
	// MaxHeaderBytes HTTP 请求头最大字节数: 1MB
	MaxHeaderBytes = 1 << 20

	// MaxRequestBody HTTP 请求体最大字节数: 1MB
	MaxRequestBody = 1 << 20
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
// 统一错误码区间分配
//
//	10000-19999  通用/网关错误
//	20000-29999  用户服务 (User Service)
//	30000-39999  文章服务 (Article Service)
//	40000-49999  评论服务 (Comment Service)
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
	ErrCodeUserExists        = 20001
	ErrCodeTokenInvalid      = 20002
	ErrCodePasswordIncorrect = 20003
	ErrCodeUserInBlacklist   = 20004
	ErrCodeUserNotFound      = 20005
	ErrCodeTokenExpired      = 20006

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
	DefaultConfigPath = "config.yaml"

	// DefaultGRPCPort 默认 gRPC 端口
	DefaultGRPCPort = 9090

	// LogFileNameFmt 日志文件名格式: {service}-{date}.log
	LogFileNameFmt = "%s-%s.log"

	// LogFieldService 日志中 service 字段名
	LogFieldService = "service"

	// LogFieldEnv 日志中 env 字段名
	LogFieldEnv = "env"

	// LogInitMsg 日志初始化消息
	LogInitMsg = "Logger initialized"
)

// ============================================================================
// 环境变量名常量
// ============================================================================
const (
	// EnvLogDir 日志目录环境变量
	EnvLogDir = "LOG_DIR"

	// EnvAppEnv 应用运行环境环境变量
	EnvAppEnv = "APP_ENV"

	// EnvConfigPath 配置文件路径环境变量
	EnvConfigPath = "CONFIG_PATH"
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
