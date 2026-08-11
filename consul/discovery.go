package consul

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mysunshines/gocommon/constants"
	"github.com/mysunshines/gocommon/grpcclient"
	httpclient "github.com/mysunshines/gocommon/http"
	"github.com/mysunshines/gocommon/log"
	"github.com/mysunshines/gocommon/middleware"

	"github.com/sirupsen/logrus"
)

// resilienceKey 供 gocommon/resilience 按下游区分超时/熔断/限流的 key。
const resilienceKey = "consul"

// MetaKeyGRPCPort / MetaKeyHTTPPort 是服务注册时写入 Consul Meta 的端口字段名，
// 与 Register 中写入的键保持一致，使调用方无需在配置里写死下游端口。
const (
	MetaKeyGRPCPort = "grpc_port"
	MetaKeyHTTPPort = "http_port"
)

// Instance 描述 Consul 中的一个服务实例。
//
// 它同时覆盖 catalog（/v1/catalog/service）与 health（/v1/health/service）两个
// 接口返回的信息，调用方既可只取 Addr() 做 gRPC 连接，也可读取 Meta/Tags
// 做路由决策，无需自行解析 Consul 原始 JSON。
type Instance struct {
	ID       string            `json:"id"`        // Consul 服务实例唯一 ID
	Name     string            `json:"name"`      // 服务名
	Address  string            `json:"address"`   // 实例 IP/主机地址
	Port     int               `json:"port"`      // 注册端口（通常为 gRPC 端口）
	HTTPPort int               `json:"http_port"` // HTTP 端口，取自 Meta["http_port"]，缺失为 0
	Tags     []string          `json:"tags"`      // 服务标签
	Meta     map[string]string `json:"meta"`      // 服务元数据
	Healthy  bool              `json:"healthy"`   // 是否来自 passing 健康实例查询
}

// Addr 返回 "host:port" 形式的注册地址（gRPC 用）。
func (i *Instance) Addr() string {
	return joinHostPort(i.Address, i.Port)
}

// HTTPAddr 返回 "host:port" 形式的 HTTP 地址；HTTPPort 为 0 时回退到 fallback。
func (i *Instance) HTTPAddr(fallback int) string {
	port := i.HTTPPort
	if port == 0 {
		port = fallback
	}
	return joinHostPort(i.Address, port)
}

func joinHostPort(host string, port int) string {
	return fmt.Sprintf("%s:%d", host, port)
}

// Discovery 是基于 Consul HTTP API 的服务发现客户端。
//
// 内部统一走 gocommon/http，因此所有请求/响应均自动带 traceID、耗时与状态码日志，
// 并接入 resilience 的超时/熔断/限流。查询结果按服务维度做 TTL 缓存；
// Consul 不可用或某次刷新失败时降级复用上一次成功的缓存，保证调用方可用性。
type Discovery struct {
	consulAddr string
	httpClient *httpclient.Client
	ttl        time.Duration

	mu       sync.RWMutex
	cache    map[string][]*Instance
	lastSeen map[string]time.Time
}

// NewDiscovery 创建服务发现客户端。consulAddr 为 Consul Agent 地址（如 "consul:8500"）。
// ttl 为缓存有效期，<=0 时使用默认 10s。
func NewDiscovery(consulAddr string, ttl time.Duration) *Discovery {
	if consulAddr == "" {
		consulAddr = "consul:8500"
	}
	if ttl <= 0 {
		ttl = 10 * time.Second
	}
	return &Discovery{
		consulAddr: consulAddr,
		httpClient: httpclient.New(
			httpclient.WithBaseURL(fmt.Sprintf("http://%s", consulAddr)),
			httpclient.WithTimeout(constants.DefaultHTTPRequestTimeout*time.Second),
			httpclient.WithResilienceKey(resilienceKey),
		),
		ttl:      ttl,
		cache:    make(map[string][]*Instance),
		lastSeen: make(map[string]time.Time),
	}
}

// Address 返回 Consul Agent 地址。
func (d *Discovery) Address() string {
	return d.consulAddr
}

// get 统一发起 Consul HTTP API 调用并解析 JSON。请求级日志由 gocommon/http
// 内部输出，这里只补充 Consul 语义层面的错误上下文。
func (d *Discovery) get(ctx context.Context, path, op, service string, out interface{}) error {
	traceID := middleware.GetTraceIDFromContext(ctx)
	fields := logrus.Fields{
		constants.LogFieldTraceID: traceID,
		"op":                      op,
		"service":                 service,
		"path":                    path,
	}

	resp, err := d.httpClient.Get(ctx, path, nil)
	if err != nil {
		fields["err"] = err.Error()
		log.WithFields(fields).Errorf("[consul:discovery] query failed")
		return fmt.Errorf("consul discovery: query %q: %w", service, err)
	}
	if resp.StatusCode != http.StatusOK {
		fields["status"] = resp.StatusCode
		log.WithFields(fields).Errorf("[consul:discovery] unexpected status")
		return fmt.Errorf("consul discovery: query %q: unexpected status %d", service, resp.StatusCode)
	}
	if err := resp.Unmarshal(out); err != nil {
		fields["err"] = err.Error()
		log.WithFields(fields).Errorf("[consul:discovery] parse response failed")
		return fmt.Errorf("consul discovery: unmarshal %q: %w", service, err)
	}
	return nil
}

// ListServices 返回 Consul 中所有服务的全部实例。
//
// 单个服务查询失败不会中断整体：失败项记录日志后跳过，尽最大努力返回其余结果。
func (d *Discovery) ListServices(ctx context.Context) ([]*Instance, error) {
	// /v1/catalog/services 返回 {"service-name": ["tag1", "tag2"]}
	var names map[string][]string
	if err := d.get(ctx, "/v1/catalog/services", "catalog.services", "", &names); err != nil {
		return nil, err
	}

	instances := make([]*Instance, 0, len(names))
	for name := range names {
		svc, err := d.GetInstances(ctx, name)
		if err != nil {
			log.WithFields(logrus.Fields{
				constants.LogFieldTraceID: middleware.GetTraceIDFromContext(ctx),
				"service":                 name,
				"err":                     err.Error(),
			}).Warnf("[consul:discovery] skip service due to query failure")
			continue
		}
		instances = append(instances, svc...)
	}
	return instances, nil
}

// GetInstances 返回指定服务的全部实例（含不健康实例），带 TTL 缓存。
// 查询失败时若存在历史缓存则降级返回缓存，避免下游因 Consul 抖动整体不可用。
func (d *Discovery) GetInstances(ctx context.Context, service string) ([]*Instance, error) {
	if cached, ok := d.fresh(service); ok {
		return cached, nil
	}

	var raw []struct {
		ServiceID      string            `json:"ServiceID"`
		ServiceName    string            `json:"ServiceName"`
		ServiceAddress string            `json:"ServiceAddress"`
		ServicePort    int               `json:"ServicePort"`
		ServiceTags    []string          `json:"ServiceTags"`
		ServiceMeta    map[string]string `json:"ServiceMeta"`
		NodeAddress    string            `json:"Address"`
	}
	path := fmt.Sprintf("/v1/catalog/service/%s", service)
	if err := d.get(ctx, path, "catalog.service", service, &raw); err != nil {
		if stale := d.cached(service); len(stale) > 0 {
			log.WithFields(logrus.Fields{
				constants.LogFieldTraceID: middleware.GetTraceIDFromContext(ctx),
				"service":                 service,
				"instances":               len(stale),
			}).Warnf("[consul:discovery] query failed, fallback to stale cache")
			return stale, nil
		}
		return nil, err
	}

	instances := make([]*Instance, 0, len(raw))
	for _, r := range raw {
		addr := r.ServiceAddress
		if addr == "" {
			addr = r.NodeAddress
		}
		if addr == "" || r.ServicePort == 0 {
			continue
		}
		instances = append(instances, &Instance{
			ID:       r.ServiceID,
			Name:     r.ServiceName,
			Address:  addr,
			Port:     r.ServicePort,
			HTTPPort: metaPort(r.ServiceMeta, MetaKeyHTTPPort),
			Tags:     r.ServiceTags,
			Meta:     r.ServiceMeta,
		})
	}

	d.store(service, instances)
	return instances, nil
}

// GetHealthyInstances 返回指定服务通过健康检查（passing）的实例。
// 该结果不写入缓存，避免与 GetInstances 的全量语义混淆。
func (d *Discovery) GetHealthyInstances(ctx context.Context, service string) ([]*Instance, error) {
	var raw []struct {
		Node struct {
			Address string `json:"Address"`
		} `json:"Node"`
		Service struct {
			ID      string            `json:"ID"`
			Service string            `json:"Service"`
			Address string            `json:"Address"`
			Port    int               `json:"Port"`
			Tags    []string          `json:"Tags"`
			Meta    map[string]string `json:"Meta"`
		} `json:"Service"`
	}
	path := fmt.Sprintf("/v1/health/service/%s?passing=true", service)
	if err := d.get(ctx, path, "health.service", service, &raw); err != nil {
		return nil, err
	}

	instances := make([]*Instance, 0, len(raw))
	for _, r := range raw {
		addr := r.Service.Address
		if addr == "" {
			addr = r.Node.Address
		}
		if addr == "" || r.Service.Port == 0 {
			continue
		}
		instances = append(instances, &Instance{
			ID:       r.Service.ID,
			Name:     r.Service.Service,
			Address:  addr,
			Port:     r.Service.Port,
			HTTPPort: metaPort(r.Service.Meta, MetaKeyHTTPPort),
			Tags:     r.Service.Tags,
			Meta:     r.Service.Meta,
			Healthy:  true,
		})
	}
	return instances, nil
}

// Pick 返回指定服务的一个可用实例：优先随机挑选健康实例，
// 健康实例为空（或健康查询失败）时回退到全量实例，两者皆空才返回错误。
func (d *Discovery) Pick(ctx context.Context, service string) (*Instance, error) {
	instances, err := d.GetHealthyInstances(ctx, service)
	if err != nil || len(instances) == 0 {
		instances, err = d.GetInstances(ctx, service)
		if err != nil {
			return nil, err
		}
	}
	if len(instances) == 0 {
		return nil, fmt.Errorf("consul discovery: no available instance for service %q", service)
	}
	return instances[rand.Intn(len(instances))], nil
}

// Resolve 返回指定服务的一个可用实例地址（host:port），用于 gRPC 连接。
func (d *Discovery) Resolve(ctx context.Context, service string) (string, error) {
	inst, err := d.Pick(ctx, service)
	if err != nil {
		return "", err
	}
	return inst.Addr(), nil
}

// Run 启动后台缓存刷新循环，直到 stop 关闭。通常在独立 goroutine 中调用。
func (d *Discovery) Run(stop <-chan struct{}) {
	ticker := time.NewTicker(d.ttl)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			d.mu.RLock()
			services := make([]string, 0, len(d.cache))
			for s := range d.cache {
				services = append(services, s)
			}
			d.mu.RUnlock()
			for _, s := range services {
				d.invalidate(s)
				if _, err := d.GetInstances(context.Background(), s); err != nil {
					log.WithFields(logrus.Fields{
						constants.LogFieldTraceID: "",
						"service":                 s,
						"err":                     err.Error(),
					}).Warnf("[consul:discovery] background refresh failed")
				}
			}
		}
	}
}

// RefreshCache 清空全部缓存，下次查询将强制回源 Consul。
func (d *Discovery) RefreshCache() {
	d.mu.Lock()
	d.cache = make(map[string][]*Instance)
	d.lastSeen = make(map[string]time.Time)
	d.mu.Unlock()
}

// fresh 返回未过期的缓存快照。
func (d *Discovery) fresh(service string) ([]*Instance, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	last, ok := d.lastSeen[service]
	if !ok || time.Since(last) >= d.ttl {
		return nil, false
	}
	cached, ok := d.cache[service]
	return cached, ok
}

// cached 返回缓存快照，不判断是否过期（用于失败降级）。
func (d *Discovery) cached(service string) []*Instance {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.cache[service]
}

// store 无条件覆盖缓存并刷新该服务的时间戳，确保 TTL 到期后能拿到最新实例。
func (d *Discovery) store(service string, instances []*Instance) {
	d.mu.Lock()
	d.cache[service] = instances
	d.lastSeen[service] = time.Now()
	d.mu.Unlock()
}

// invalidate 使指定服务的缓存过期，但保留数据以便查询失败时降级复用。
func (d *Discovery) invalidate(service string) {
	d.mu.Lock()
	delete(d.lastSeen, service)
	d.mu.Unlock()
}

// metaPort 从 Consul Meta 中解析整型端口；缺失或非法时返回 0。
func metaPort(meta map[string]string, key string) int {
	if meta == nil {
		return 0
	}
	v, ok := meta[key]
	if !ok || v == "" {
		return 0
	}
	p, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || p <= 0 || p > 65535 {
		return 0
	}
	return p
}

// ---------------------------------------------------------------------------
// grpcclient 集成：把 Consul 作为 grpcclient 的服务解析器
// ---------------------------------------------------------------------------

// aliasToService 维护 "proto 服务名(alias) -> Consul 服务名" 的映射，
// 因为 grpcclient.SendRequest 用 proto 的 full method name（如 user.v1.UserService）
// 作为解析键，而 Consul 里注册的服务名是业务名（如 user-service）。
var (
	aliasMu        sync.RWMutex
	aliasToService = make(map[string]string)
)

// RegisterAlias 声明 proto 服务名(alias) 对应的 Consul 服务名。
// 当 proto 的 full method 前缀（如 user.v1.UserService）与 Consul 注册名
// （如 user-service）不一致时使用；若二者一致则无需调用，解析会兜底直接用 alias。
// 例如：RegisterAlias("user.v1.UserService", "user-service")。
func RegisterAlias(alias, consulService string) {
	aliasMu.Lock()
	aliasToService[alias] = consulService
	aliasMu.Unlock()
}

// UseConsulDiscovery 将 Consul 接入 grpcclient 的服务解析，
// 使 grpcclient.SendRequest 不再依赖手写死地址 RegisterService，而是从 Consul
// 动态发现目标实例。consulAddr 为 Consul Agent 地址（如 "consul:8500"）。
//
// 解析时优先使用 RegisterAlias 显式声明；否则按命名约定自动推导
// （user.v1.UserService → user-service），与网关及服务注册名保持一致；
// 再不行才用 alias 本身作为 Consul 服务名。内部启动后台刷新 goroutine。
func UseConsulDiscovery(consulAddr string) {
	disc := NewDiscovery(consulAddr, 0)
	grpcclient.SetServiceResolver(func(ctx context.Context, alias string) (*grpcclient.ServiceEntry, bool) {
		svc := resolveConsulService(alias)
		target, err := disc.Resolve(ctx, svc)
		if err != nil {
			log.WithFields(logrus.Fields{
				constants.LogFieldTraceID: middleware.GetTraceIDFromContext(ctx),
				"alias":                   alias,
				"service":                 svc,
				"err":                     err.Error(),
			}).Warnf("[consul:discovery] resolve for grpcclient failed")
			return nil, false
		}
		return &grpcclient.ServiceEntry{
			Service: alias, // 用 alias 作为 proto 全限定服务名，与 SendRequest 解析出的 fullMethod 匹配
			Target:  target,
		}, true
	})
	go disc.Run(make(chan struct{})) // 常驻后台刷新；进程退出随之结束
	log.Infof("consul: service discovery enabled for grpcclient via %s", consulAddr)
}

// resolveConsulService 将 grpcclient 传入的 proto 全限定服务名（alias）映射为
// Consul 注册的服务名。优先级：
//  1. 显式 RegisterAlias 声明（如 proto 服务名与 Consul 名无规律时）；
//  2. 命名约定自动推导：取 alias 首段（包名，如 user.v1.UserService → user）
//     转为小写并追加 "-service"（user → user-service），与网关 DeriveGRPCService
//     及服务注册时的 cfg.App.Name 保持一致，实现零配置服务发现；
//  3. 兜底：直接用 alias 作为 Consul 服务名（兼容注册名与 proto 名恰好一致的情况）。
func resolveConsulService(alias string) string {
	aliasMu.RLock()
	if svc, ok := aliasToService[alias]; ok {
		aliasMu.RUnlock()
		return svc
	}
	aliasMu.RUnlock()

	if idx := strings.Index(alias, "."); idx > 0 {
		return strings.ToLower(alias[:idx]) + "-service"
	}
	return alias
}
