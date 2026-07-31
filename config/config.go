package config

import (
	"fmt"
	"os"
	"strconv"

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
	QPS     int  `yaml:"qps"`     // 每秒允许请求数（速率）
	Burst   int  `yaml:"burst"`   // 突发容量（瞬时最大放行数）
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
	if p := os.Getenv("CONFIG_PATH"); p != "" {
		return p
	}
	// 2. 根据 APP_ENV 构造：config/config_test.yaml / config/config_production.yaml
	env := os.Getenv("APP_ENV")
	if env == "" || env == "development" {
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

// ApplyDefaults 为配置填充默认值（供各服务复用）
func ApplyDefaults(c *Config) {
	if c.App.Env == "" {
		c.App.Env = "development"
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
		c.Metrics.Path = "/metrics"
	}
	if c.RateLimit.QPS == 0 {
		c.RateLimit.QPS = 1000
	}
	if c.RateLimit.Burst == 0 {
		c.RateLimit.Burst = 2000
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
	if v := os.Getenv("DB_HOST"); v != "" {
		c.Database.Host = v
	}
	if v := os.Getenv("DB_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			c.Database.Port = port
		}
	}
	if v := os.Getenv("DB_USER"); v != "" {
		c.Database.User = v
	}
	if v := os.Getenv("DB_PASSWORD"); v != "" {
		c.Database.Password = v
	}
	if v := os.Getenv("DB_NAME"); v != "" {
		c.Database.Name = v
	}

	// Redis
	if v := os.Getenv("REDIS_HOST"); v != "" {
		c.Redis.Host = v
	}
	if v := os.Getenv("REDIS_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			c.Redis.Port = port
		}
	}
	if v := os.Getenv("REDIS_PASSWORD"); v != "" {
		c.Redis.Password = v
	}

	// Consul — 优先 CONSUL_ADDRESS，其次 MICRO_REGISTRY_ADDRESS
	if v := os.Getenv("CONSUL_ADDRESS"); v != "" {
		c.Consul.Address = v
		c.Micro.Registry.Address = v
	} else if v := os.Getenv("MICRO_REGISTRY_ADDRESS"); v != "" {
		// Docker Compose 中通常用 MICRO_REGISTRY_ADDRESS
		c.Consul.Address = v
		c.Micro.Registry.Address = v
	}

	// gRPC Port — 支持 MICRO_SERVER_ADDRESS 格式 :port
	if v := os.Getenv("GRPC_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			c.GRPC.Port = port
		}
	}
}
