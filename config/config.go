package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/mysunshines/gocommon/constants"
	"gopkg.in/yaml.v3"
)

var cfg *Config

// Config 通用配置结构，各服务通过类型别名复用
type Config struct {
	App       AppConfig       `yaml:"app"`        // 应用基础配置（名称/环境/日志/监听）
	Database  DatabaseConfig  `yaml:"database"`   // 数据库连接配置
	Redis     RedisConfig     `yaml:"redis"`      // Redis 缓存配置
	JWT       JWTConfig       `yaml:"jwt"`        // JWT 鉴权配置
	GRPC      GRPCConfig      `yaml:"grpc"`       // gRPC 监听配置
	HTTP      HTTPConfig      `yaml:"http"`       // HTTP 监听配置
	Consul    ConsulConfig    `yaml:"consul"`     // Consul 注册中心配置
	Micro     MicroConfig     `yaml:"micro"`      // 微服务（注册中心）配置
	Metrics   MetricsConfig   `yaml:"metrics"`    // Prometheus 指标暴露配置
	RateLimit RateLimitConfig `yaml:"rate_limit"` // 限流配置
	Server    ServerConfig    `yaml:"server"`     // 入站 server 调优（gRPC/HTTP 超时与 keepalive）
	CORS      CORSConfig      `yaml:"cors,omitempty"` // CORS 跨域配置（article 等需要 HTTP 直连的服务）
	Mail      MailConfig      `yaml:"mail,omitempty"` // 邮件配置（user 等需要发信的服务）
	MinIO     MinIOConfig     `yaml:"minio"`      // 对象存储配置（文件上传统一落 MinIO）
	Loki      LokiConfig      `yaml:"loki"`       // Loki 集中日志（想法 3 · 方案 A）
	OTel      OTelConfig      `yaml:"otel"`       // OpenTelemetry 链路追踪（想法 3 · 方案 B）
}

// LokiConfig Loki 集中日志配置（想法 3 · 方案 A）。
// 为空 / 不配置时自动降级，仅保留本地日志，不影响现有部署。
type LokiConfig struct {
	Enabled  bool   `yaml:"enabled"`   // 是否启用 Loki 推送
	URL      string `yaml:"url"`       // Loki push 地址，如 http://loki:3100/loki/api/v1/push
	TenantID string `yaml:"tenant_id"` // 多租户 ID（可选，空则不带 X-Scope-OrgID）
}

// OTelConfig OpenTelemetry 链路追踪配置（想法 3 · 方案 B）。
// OTLP 导出器默认走 otel-collector（再由 collector 转发到 Tempo/Jaeger）。
// Endpoint 为空 / 不配置时降级：不采集 trace，但 TraceID 取数逻辑仍可用。
type OTelConfig struct {
	Enabled  bool   `yaml:"enabled"`   // 是否启用 trace 采集
	Endpoint string `yaml:"endpoint"`  // otel-collector gRPC 地址，如 otel-collector:4317
}

// MinIOConfig 对象存储（MinIO / S3 兼容）配置。
// 网关接收文件上传后存入 MinIO 并返回公共读 URL；下游服务不再各自落本地盘。
type MinIOConfig struct {
	Enabled         bool   `yaml:"enabled"`          // 是否启用对象存储（网关上传需开启）
	Endpoint        string `yaml:"endpoint"`         // MinIO API 地址，如 "minio:9000"
	AccessKeyID     string `yaml:"access_key_id"`    // 访问 Key
	SecretAccessKey string `yaml:"secret_access_key"`// 访问 Secret
	Bucket          string `yaml:"bucket"`           // 存储桶名，如 "blog"
	UseSSL          bool   `yaml:"use_ssl"`          // 是否 HTTPS
	PublicBaseURL   string `yaml:"public_base_url"`  // 文件对外可访问基础 URL（不含末尾斜杠）
	AutoCreateBucket bool  `yaml:"auto_create_bucket"` // 初始化时自动创建不存在的 bucket（公共读）
}

type AppConfig struct {
	Name      string `yaml:"name"`       // 服务名（用于日志/标识）
	Env       string `yaml:"env"`        // 运行环境：development / production
	LogLevel  string `yaml:"log_level"`  // 日志级别：debug/info/warn/error
	LogDir    string `yaml:"log_dir"`    // 日志目录
	Host      string `yaml:"host"`       // HTTP 监听主机（默认 0.0.0.0）
	Port      int    `yaml:"port"`       // HTTP 监听端口
	ServiceID string `yaml:"service_id"` // 实例唯一 ID（注册用）
}

// Addr 返回 HTTP 监听地址
func (a *AppConfig) Addr() string {
	host := a.Host
	if host == "" {
		host = "0.0.0.0"
	}
	if a.Port == 0 {
		return host
	}
	return fmt.Sprintf("%s:%d", host, a.Port)
}

type DatabaseConfig struct {
	Host            string `yaml:"host"`             // 数据库主机
	Port            int    `yaml:"port"`             // 数据库端口
	User            string `yaml:"user"`             // 用户名
	Password        string `yaml:"password"`         // 密码
	Name            string `yaml:"name"`             // 数据库名
	MaxOpenConns    int    `yaml:"max_open_conns"`   // 最大打开连接数
	MaxIdleConns    int    `yaml:"max_idle_conns"`   // 最大空闲连接数
	ConnMaxLifetime int    `yaml:"conn_max_lifetime"`// 连接最大存活时间（秒）
	SlowThreshold   int    `yaml:"slow_threshold"`   // 慢查询阈值（毫秒）
}

// DSN 返回 MySQL 连接字符串
func (d *DatabaseConfig) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		d.User, d.Password, d.Host, d.Port, d.Name)
}

type RedisConfig struct {
	Host      string `yaml:"host"`       // Redis 主机
	Port      int    `yaml:"port"`       // Redis 端口
	Password  string `yaml:"password"`   // 密码（无则空）
	DB        int    `yaml:"db"`         // 逻辑库编号
	PoolSize  int    `yaml:"pool_size"`  // 连接池大小
	KeyPrefix string `yaml:"key_prefix"` // 键前缀（避免多服务键冲突）
}

// Addr 返回 Redis 地址
func (r *RedisConfig) Addr() string {
	return fmt.Sprintf("%s:%d", r.Host, r.Port)
}

type JWTConfig struct {
	Secret     string `yaml:"secret"`
	ExpireTime int    `yaml:"expire_time"`
}

type GRPCConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

// Addr 返回 gRPC 监听地址
func (g *GRPCConfig) Addr() string {
	return fmt.Sprintf("%s:%d", g.Host, g.Port)
}

type HTTPConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

// Addr 返回 HTTP 监听地址
func (h *HTTPConfig) Addr() string {
	return fmt.Sprintf("%s:%d", h.Host, h.Port)
}

type ConsulConfig struct {
	Address            string `yaml:"address"`
	CheckInterval      int    `yaml:"check_interval"`
	DeregisterCritical int    `yaml:"deregister_critical"`
}

// MicroConfig 微服务网关配置（服务注册中心）
type MicroConfig struct {
	Registry RegistryConfig `yaml:"registry"`
}

// RegistryConfig 服务注册中心配置
type RegistryConfig struct {
	Type    string            `yaml:"type"`    // 注册中心类型（如 consul）
	Address string            `yaml:"address"` // 注册中心地址 host:port
	Timeout int               `yaml:"timeout"` // 注册/发现超时（秒）
	Options map[string]string `yaml:"options"` // 扩展选项
}

type MetricsConfig struct {
	Enabled bool   `yaml:"enabled"` // 是否启用指标暴露
	Port    int    `yaml:"port"`    // 指标 HTTP 端口
	Path    string `yaml:"path"`    // 指标路径（默认 /metrics）
}

type RateLimitConfig struct {
	Enabled bool `yaml:"enabled"` // 是否启用限流
	QPS     int  `yaml:"qps"`     // 全局默认每秒允许请求数（速率），作为无规则匹配时的兜底
	Burst   int  `yaml:"burst"`   // 全局默认突发容量（瞬时最大放行数）

	// Rules 路由级限流规则（可选）。按请求路径前缀匹配，命中第一条即采用该规则，
	// 用于对写操作（注册/登录/发文章/发评论等）施加比全局更严格的限流。
	// 未命中任何规则时回退到全局 QPS/Burst。
	Rules []RateLimitRule `yaml:"rules,omitempty"`
}

// RateLimitRule 单条路由级限流规则。
//   - MatchPaths：路径前缀列表（前缀匹配，如 "/api/v1/auth/register"），命中其一即应用本规则；
//   - QPS/Burst：本规则使用的速率与突发容量。
type RateLimitRule struct {
	MatchPaths []string `yaml:"paths"` // 需要匹配的路径前缀（任一命中即生效）
	QPS        int      `yaml:"qps"`    // 该规则每秒允许请求数
	Burst      int      `yaml:"burst"`  // 该规则突发容量
}

// CORSConfig 跨域资源共享配置（HTTP 直连调试 / Web 前端调用时需要）。
// 仅 article 等暴露 HTTP API 的服务会配置此段；其他服务 yaml 不写 cors 时不生效。
type CORSConfig struct {
	Enabled          bool     `yaml:"enabled"`           // 是否启用 CORS
	AllowOrigins     []string `yaml:"allow_origins"`     // 允许的来源
	AllowMethods     []string `yaml:"allow_methods"`     // 允许的 HTTP 方法
	AllowHeaders     []string `yaml:"allow_headers"`     // 允许的请求头
	ExposeHeaders    []string `yaml:"expose_headers"`    // 暴露的响应头
	AllowCredentials bool     `yaml:"allow_credentials"` // 是否允许携带凭证
	MaxAge           int      `yaml:"max_age"`           // 预检结果缓存时间（秒）
}

// MailConfig 邮件（SMTP）配置。
// 仅 user 等需要发送邮件（注册验证/找回密码）的服务会配置此段；其他服务 yaml 不写 mail 时不生效。
type MailConfig struct {
	SMTPHost     string `yaml:"smtp_host"`     // SMTP 服务器地址
	SMTPPort     int    `yaml:"smtp_port"`     // SMTP 端口（默认 587）
	SMTPUsername string `yaml:"smtp_username"` // 登录用户名
	SMTPPassword string `yaml:"smtp_password"` // 登录密码
	FromAddress  string `yaml:"from_address"`  // 发件人地址（默认 noreply@blog.local）
	UseTLS       bool   `yaml:"use_tls"`       // 是否使用 TLS
}

// Load 加载配置文件
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	ApplyDefaults(&c)
	cfg = &c

	return &c, nil
}

// ResolveConfigPath 根据环境变量决定配置文件路径
// 优先级：CONFIG_PATH（显式指定）> config/config_<APP_ENV>.yaml > config/config.yaml（默认）
// 示例：
//   - APP_ENV=test       → config/config_test.yaml
//   - APP_ENV=production  → config/config_production.yaml
//   - 均未设或development  → config/config.yaml（向后兼容本地开发）
func ResolveConfigPath() string {
	// 1. 显式 CONFIG_PATH 环境变量（最高优先级）
	if p := os.Getenv(constants.EnvConfigPath); p != "" {
		return p
	}
	// 2. 根据 APP_ENV 构造：config/config_test.yaml / config/config_production.yaml
	env := os.Getenv(constants.EnvAppEnv)
	if env == "" || env == constants.EnvDevelopment {
		return "config/config.yaml"
	}
	return fmt.Sprintf("config/config_%s.yaml", env)
}

// LoadByEnv 根据 APP_ENV 环境变量加载对应环境的配置文件
// 等价于 Load(ResolveConfigPath())，同时自动调用 ApplyEnvOverrides
func LoadByEnv() (*Config, error) {
	path := ResolveConfigPath()
	c, err := Load(path)
	if err != nil {
		return nil, err
	}
	ApplyEnvOverrides(c)
	return c, nil
}

// Get 获取全局配置
func Get() *Config {
	return cfg
}

// LoadFile 读取 YAML 配置文件并反序列化到 out（任意结构体）。
// 供业务服务加载自定义配置使用，避免每个服务重复引入 yaml 依赖。
func LoadFile(path string, out interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}
	if err := yaml.Unmarshal(data, out); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}
	return nil
}

// ServerConfig 入站 server 调优配置（gRPC/HTTP 的超时与 keepalive）。
// 放在 config 而非 configcenter，是因为 server 构造（keepalive/并发流）在启动期即固定，
// 但其中的 method 超时与 HTTP 超时可由 configcenter 热更覆盖（写回本结构的全局实例）。
//
// 可热更：GRPC.DefaultTimeoutSec / SlowMethods / SlowMultiplier（拦截器每次请求读取）、
//
//	HTTP 各超时（http.Server 运行时支持动态修改）。
//
// 仅启动期：GRPC 的 keepalive 与 MaxConcurrentStreams（gRPC server options 构造后不可变）。
type ServerConfig struct {
	GRPC GRPCInboundConfig `yaml:"grpc"` // gRPC 入站配置
	HTTP HTTPInboundConfig `yaml:"http"` // HTTP 入站配置
}

// GRPCInboundConfig gRPC server 端入站配置（秒为单位；MaxConcurrentStreams 为个数）。
type GRPCInboundConfig struct {
	DefaultTimeoutSec     int      `yaml:"default_timeout_sec"`      // 默认入站请求超时（秒）
	SlowMethods           []string `yaml:"slow_methods"`            // 需放大超时的方法名后缀（如 ListComments）
	SlowMultiplier        float64  `yaml:"slow_multiplier"`         // 慢方法超时放大倍数（默认 2）
	MaxConnectionIdle     int      `yaml:"max_connection_idle_sec"` // keepalive: 空闲连接回收（秒）
	MaxConnectionAge      int      `yaml:"max_connection_age_sec"`  // keepalive: 连接最大寿命（秒）
	MaxConnectionAgeGrace int      `yaml:"max_connection_age_grace_sec"` // keepalive: 寿命宽限（秒）
	MinPingInterval       int      `yaml:"min_ping_interval_sec"`   // keepalive: 客户端最小 ping 间隔（秒）
	MaxConcurrentStreams  uint32   `yaml:"max_concurrent_streams"`  // 最大并发流（仅启动期）
}

// HTTPInboundConfig HTTP server 端入站超时配置（秒为单位）。
// 标准库 http.Server 在运行时会读取这些字段，因此可热更（改全局即生效）。
type HTTPInboundConfig struct {
	ReadTimeoutSec       int `yaml:"read_timeout_sec"`        // 读取整个请求体超时
	ReadHeaderTimeoutSec int `yaml:"read_header_timeout_sec"` // 读取请求头超时
	WriteTimeoutSec      int `yaml:"write_timeout_sec"`       // 写入响应超时
	IdleTimeoutSec       int `yaml:"idle_timeout_sec"`        // 空闲连接超时
	DefaultTimeoutSec    int `yaml:"default_timeout_sec"`     // 中间件整体请求超时（秒），TimeoutMiddlewareDefault 每次请求读取，可热更
}

// ApplyDefaults 为配置填充默认值（供各服务复用）
func ApplyDefaults(c *Config) {
	if c.App.Env == "" {
		c.App.Env = constants.EnvDevelopment
	}
	if c.App.LogLevel == "" {
		c.App.LogLevel = "info"
	}
	if c.App.LogDir == "" {
		c.App.LogDir = "./logs"
	}
	if c.App.Host == "" {
		c.App.Host = "0.0.0.0"
	}
	if c.HTTP.Host == "" {
		c.HTTP.Host = "0.0.0.0"
	}
	if c.HTTP.Port == 0 {
		c.HTTP.Port = 8080
	}
	if c.GRPC.Host == "" {
		c.GRPC.Host = "0.0.0.0"
	}
	if c.GRPC.Port == 0 {
		c.GRPC.Port = 9000
	}
	// 多实例安全：4 个服务 × 25 = 100 连接，远低于 MySQL 默认 151
	if c.Database.MaxOpenConns == 0 {
		c.Database.MaxOpenConns = 25
	}
	if c.Database.MaxIdleConns == 0 {
		c.Database.MaxIdleConns = 5
	}
	if c.Database.ConnMaxLifetime == 0 {
		c.Database.ConnMaxLifetime = 3600
	}
	if c.Redis.PoolSize == 0 {
		c.Redis.PoolSize = 100
	}
	if c.JWT.ExpireTime == 0 {
		c.JWT.ExpireTime = 86400 * 7
	}
	if c.Consul.Address == "" {
		c.Consul.Address = "localhost:8500"
	}
	if c.Consul.CheckInterval == 0 {
		c.Consul.CheckInterval = 10
	}
	if c.Consul.DeregisterCritical == 0 {
		c.Consul.DeregisterCritical = 30
	}
	if c.Micro.Registry.Type == "" {
		c.Micro.Registry.Type = "consul"
	}
	if c.Micro.Registry.Address == "" {
		c.Micro.Registry.Address = c.Consul.Address
	}
	if c.Micro.Registry.Timeout == 0 {
		c.Micro.Registry.Timeout = 5
	}
	if c.Metrics.Port == 0 {
		c.Metrics.Port = 9090
	}
	if c.Metrics.Path == "" {
		c.Metrics.Path = constants.MetricsPath
	}
	if c.RateLimit.QPS == 0 {
		c.RateLimit.QPS = 1000
	}
	if c.RateLimit.Burst == 0 {
		c.RateLimit.Burst = 2000
	}
	if c.Mail.SMTPPort == 0 {
		c.Mail.SMTPPort = 587
	}
	if c.Mail.FromAddress == "" {
		c.Mail.FromAddress = "noreply@blog.local"
	}
	// 入站 server 调优默认值（与 constants 中的历史默认值保持一致）。
	if c.Server.GRPC.DefaultTimeoutSec == 0 {
		c.Server.GRPC.DefaultTimeoutSec = constants.DefaultGRPCUnaryTimeout
	}
	if c.Server.GRPC.SlowMultiplier == 0 {
		c.Server.GRPC.SlowMultiplier = 2
	}
	if c.Server.GRPC.MaxConnectionIdle == 0 {
		c.Server.GRPC.MaxConnectionIdle = constants.DefaultGRPCMaxConnectionIdle
	}
	if c.Server.GRPC.MaxConnectionAge == 0 {
		c.Server.GRPC.MaxConnectionAge = constants.DefaultGRPCMaxConnectionAge
	}
	if c.Server.GRPC.MaxConnectionAgeGrace == 0 {
		c.Server.GRPC.MaxConnectionAgeGrace = constants.DefaultGRPCMaxConnectionAgeGrace
	}
	if c.Server.GRPC.MinPingInterval == 0 {
		c.Server.GRPC.MinPingInterval = constants.DefaultGRPCMinPingInterval
	}
	if c.Server.GRPC.MaxConcurrentStreams == 0 {
		c.Server.GRPC.MaxConcurrentStreams = constants.DefaultGRPCMaxConcurrentStreams
	}
	if c.Server.HTTP.ReadTimeoutSec == 0 {
		c.Server.HTTP.ReadTimeoutSec = constants.DefaultReadTimeout
	}
	if c.Server.HTTP.ReadHeaderTimeoutSec == 0 {
		c.Server.HTTP.ReadHeaderTimeoutSec = constants.DefaultReadHeaderTimeout
	}
	if c.Server.HTTP.WriteTimeoutSec == 0 {
		c.Server.HTTP.WriteTimeoutSec = constants.DefaultWriteTimeout
	}
	if c.Server.HTTP.IdleTimeoutSec == 0 {
		c.Server.HTTP.IdleTimeoutSec = constants.DefaultIdleTimeout
	}
	if c.Server.HTTP.DefaultTimeoutSec == 0 {
		c.Server.HTTP.DefaultTimeoutSec = constants.DefaultHTTPRequestTimeout
	}
}

// ApplyEnvOverrides 用环境变量覆盖配置值，使同一份 config.yaml 可在本地和 Docker 环境通用。
// Docker Compose 中通过 environment 设置以下变量：
//
//	DB_HOST / DB_PORT / DB_USER / DB_PASSWORD / DB_NAME
//	REDIS_HOST / REDIS_PORT / REDIS_PASSWORD
//	CONSUL_ADDRESS / MICRO_REGISTRY_ADDRESS
func ApplyEnvOverrides(c *Config) {
	// Database
	if v := os.Getenv(constants.EnvDBHost); v != "" {
		c.Database.Host = v
	}
	if v := os.Getenv(constants.EnvDBPort); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			c.Database.Port = port
		}
	}
	if v := os.Getenv(constants.EnvDBUser); v != "" {
		c.Database.User = v
	}
	if v := os.Getenv(constants.EnvDBPassword); v != "" {
		c.Database.Password = v
	}
	if v := os.Getenv(constants.EnvDBName); v != "" {
		c.Database.Name = v
	}

	// Redis
	if v := os.Getenv(constants.EnvRedisHost); v != "" {
		c.Redis.Host = v
	}
	if v := os.Getenv(constants.EnvRedisPort); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			c.Redis.Port = port
		}
	}
	if v := os.Getenv(constants.EnvRedisPassword); v != "" {
		c.Redis.Password = v
	}

	// Consul — 优先 CONSUL_ADDRESS，其次 MICRO_REGISTRY_ADDRESS
	if v := os.Getenv(constants.EnvConsulAddress); v != "" {
		c.Consul.Address = v
		c.Micro.Registry.Address = v
	} else if v := os.Getenv(constants.EnvMicroRegistryAddress); v != "" {
		// Docker Compose 中通常用 MICRO_REGISTRY_ADDRESS
		c.Consul.Address = v
		c.Micro.Registry.Address = v
	}

	// gRPC Port — 支持 MICRO_SERVER_ADDRESS 格式 :port
	if v := os.Getenv(constants.EnvGRPCPort); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			c.GRPC.Port = port
		}
	}

	// MinIO 对象存储
	if v := os.Getenv(constants.EnvMinIOEnabled); v != "" {
		c.MinIO.Enabled = v == "true" || v == "1"
	}
	if v := os.Getenv(constants.EnvMinIOEndpoint); v != "" {
		c.MinIO.Endpoint = v
	}
	if v := os.Getenv(constants.EnvMinIOAccessKeyID); v != "" {
		c.MinIO.AccessKeyID = v
	}
	if v := os.Getenv(constants.EnvMinIOSecretAccessKey); v != "" {
		c.MinIO.SecretAccessKey = v
	}
	if v := os.Getenv(constants.EnvMinIOBucket); v != "" {
		c.MinIO.Bucket = v
	}
	if v := os.Getenv(constants.EnvMinIOPublicBaseURL); v != "" {
		c.MinIO.PublicBaseURL = v
	}
	if v := os.Getenv(constants.EnvMinIOAutoCreateBucket); v != "" {
		c.MinIO.AutoCreateBucket = v == "true" || v == "1"
	}
}
