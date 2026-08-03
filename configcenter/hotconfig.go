package configcenter

import (
	"sync/atomic"

	"github.com/mysunshines/gocommon/config"
	"github.com/mysunshines/gocommon/log"
)

// HotConfig 是需要线上热更新、从配置后台即时下发的业务配置聚合。
//
// 设计原则：只放"在线上确实需要不改发版就能调"的配置。当前纳入：
//   - LogLevel：线上临时调 debug 排查，事后调回（无需重启生效）
//   - RateLimit：限流阈值，应对突发流量/异常调用最常调整
//   - JWTExpireTime：登录时效
//
// 基础设施配置（Database / Redis / Consul 地址与连接池）不在此列，
// 因为它们往往需重建连接或重启才安全，仍走 config_xxx.yaml + 环境变量。
//
// 字段刻意复用 config 包的既有类型（如 RateLimitConfig），保持单一数据来源，
// 后台写入的 YAML 字段名与 config.yaml 中对应段一致，降低运维心智负担。
type HotConfig struct {
	LogLevel      string                 `yaml:"log_level" json:"log_level"`
	RateLimit     config.RateLimitConfig `yaml:"rate_limit" json:"rate_limit"`
	JWTExpireTime int                    `yaml:"jwt_expire_time" json:"jwt_expire_time"`
}

// ServiceConfig 是单个服务接入配置中心的句柄：持有最新热更配置快照，
// 并能在变更时把值回写到全局 config.Config（日志级别、限流阈值等即时生效）。
type ServiceConfig struct {
	service string
	env     string
	client  *Client
	current atomic.Value // 存 *HotConfig
}

// Init 为指定服务初始化配置中心连接。
// consulAddress 形如 host:port（如 consul:8500）；service 为服务名（如 article-service）；
// env 为运行环境（如 production）。返回 ServiceConfig，调用方应再调用 Load 拉取一次、
// 并启动 Watch（通常在 goroutine 中）。
func Init(consulAddress, service, env string) *ServiceConfig {
	sc := &ServiceConfig{
		service: service,
		env:     env,
		client:  New(consulAddress),
	}
	sc.current.Store(defaultHot())
	return sc
}

// defaultHot 返回兜底热更配置（与 config.ApplyDefaults 保持一致的默认值），
// 当 Consul KV 暂无可热更配置时使用，避免线上零值崩溃。
func defaultHot() *HotConfig {
	return &HotConfig{
		LogLevel:      "info",
		RateLimit:     config.RateLimitConfig{Enabled: true, QPS: 1000, Burst: 2000},
		JWTExpireTime: 86400 * 7,
	}
}

// Load 从 Consul KV 拉取并应用热更配置。key 不存在时返回 ErrNotFound，
// 调用方据此保留 yaml 默认值（不视为错误）。
func (sc *ServiceConfig) Load() error {
	key := Key(sc.service, sc.env)
	var hc HotConfig
	if err := sc.client.Load(key, &hc); err != nil {
		return err
	}
	sc.current.Store(&hc)
	sc.apply(&hc)
	return nil
}

// Watch 先拉取一次当前配置（确保其回写全局 config），再在后台长轮询 Consul KV；
// 配置变更时即时刷新内存快照并回写全局 config。
// 阻塞式，应在 goroutine 中调用（如 go sc.Watch()）。
func (sc *ServiceConfig) Watch() {
	// 启动时先拉一次（KV 不存在会返回 ErrNotFound，可忽略），保证初始值即时生效。
	if err := sc.Load(); err != nil && err != ErrNotFound {
		log.Warnf("configcenter: initial load failed: %v", err)
	}
	key := Key(sc.service, sc.env)
	var hc HotConfig
	_ = sc.client.Watch(key, &hc, func() {
		sc.current.Store(&hc)
		sc.apply(&hc)
	})
}

// apply 将热更配置回写到全局 config.Config，使现成读取（Get 等）即时生效。
// 同时刷新本地原子快照，保证 Get() 始终返回最新值。
func (sc *ServiceConfig) apply(hc *HotConfig) {
	sc.current.Store(hc)
	c := config.Get()
	if c == nil {
		return
	}
	if hc.LogLevel != "" {
		c.App.LogLevel = hc.LogLevel
		log.SetLevel(hc.LogLevel) // 即时调整运行期日志级别
	}
	c.RateLimit = hc.RateLimit
	if hc.JWTExpireTime > 0 {
		c.JWT.ExpireTime = hc.JWTExpireTime
	}
	log.Infof("configcenter: applied hot config for %s/%s (log_level=%s qps=%d burst=%d jwt=%ds)",
		sc.service, sc.env, hc.LogLevel, hc.RateLimit.QPS, hc.RateLimit.Burst, hc.JWTExpireTime)
}

// Get 返回当前最新的热更配置快照（线程安全，无锁读取）。
func (sc *ServiceConfig) Get() *HotConfig {
	return sc.current.Load().(*HotConfig)
}

// KV 返回底层 Consul Client，便于管理后台主动写入配置（Put）。
func (sc *ServiceConfig) KV() *Client {
	return sc.client
}

// Stop 停止该服务配置的后台监听（幂等），进程优雅关闭时调用。
func (sc *ServiceConfig) Stop() {
	sc.client.Stop()
}
