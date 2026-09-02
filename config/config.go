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
	App       AppConfig       `yaml:"app"`
	Database  DatabaseConfig  `yaml:"database"`
	Redis     RedisConfig     `yaml:"redis"`
	JWT       JWTConfig       `yaml:"jwt"`
	GRPC      GRPCConfig      `yaml:"grpc"`
	HTTP      HTTPConfig      `yaml:"http"`
	Consul    ConsulConfig    `yaml:"consul"`
	Metrics   MetricsConfig   `yaml:"metrics"`
	RateLimit RateLimitConfig `yaml:"rate_limit"`
}

type AppConfig struct {
	Name      string `yaml:"name"`
	Env       string `yaml:"env"`
	LogLevel  string `yaml:"log_level"`
	LogDir    string `yaml:"log_dir"`
	Host      string `yaml:"host"`
	Port      int    `yaml:"port"`
	ServiceID string `yaml:"service_id"`
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
	Host            string `yaml:"host"`
	Port            int    `yaml:"port"`
	User            string `yaml:"user"`
	Password        string `yaml:"password"`
	Name            string `yaml:"name"`
	MaxOpenConns    int    `yaml:"max_open_conns"`
	MaxIdleConns    int    `yaml:"max_idle_conns"`
	ConnMaxLifetime int    `yaml:"conn_max_lifetime"`
	SlowThreshold   int    `yaml:"slow_threshold"`
}

// DSN 返回 MySQL 连接字符串
func (d *DatabaseConfig) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		d.User, d.Password, d.Host, d.Port, d.Name)
}

type RedisConfig struct {
	Host      string `yaml:"host"`
	Port      int    `yaml:"port"`
	Password  string `yaml:"password"`
	DB        int    `yaml:"db"`
	PoolSize  int    `yaml:"pool_size"`
	KeyPrefix string `yaml:"key_prefix"`
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

type MetricsConfig struct {
	Enabled bool   `yaml:"enabled"`
	Port    int    `yaml:"port"`
	Path    string `yaml:"path"`
}

type RateLimitConfig struct {
	Enabled bool `yaml:"enabled"`
	QPS     int  `yaml:"qps"`
	Burst   int  `yaml:"burst"`
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

// Get 获取全局配置
func Get() *Config {
	return cfg
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
//	REDIS_HOST / REDIS_PORT
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

	// Consul — 优先 CONSUL_ADDRESS，其次 MICRO_REGISTRY_ADDRESS
	if v := os.Getenv("CONSUL_ADDRESS"); v != "" {
		c.Consul.Address = v
	} else if v := os.Getenv("MICRO_REGISTRY_ADDRESS"); v != "" {
		// Docker Compose 中通常用 MICRO_REGISTRY_ADDRESS
		c.Consul.Address = v
	}

	// gRPC Port — 支持 MICRO_SERVER_ADDRESS 格式 :port
	if v := os.Getenv("GRPC_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			c.GRPC.Port = port
		}
	}
}
