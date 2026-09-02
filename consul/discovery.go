package consul

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
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
// ttl 为缓存有效期，<=0 时使用默认 10s。dumpBodies 为 true 时，底层 httpclient 的
// debug 日志会附带 request/response body，便于排查 Consul 请求与响应内容。
func NewDiscovery(consulAddr string, ttl time.Duration, dumpBodies bool) *Discovery {
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
			httpclient.WithDumpBodies(dumpBodies),
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
//
// 注意降级语义：当 Consul 健康查询成功但返回 0 个健康实例（即全部不健康）时，
// 会回退到全量实例并挑选其一，但会记 Warn 告警——避免把流量静默路由到已知
// 不健康的实例而无人察觉。Consul 完全不可达（健康查询失败）时同样降级全量，
// 保证容错（宁可发坏实例也不整体不可用）。
func (d *Discovery) Pick(ctx context.Context, service string) (*Instance, error) {
	healthy, herr := d.GetHealthyInstances(ctx, service)
	if herr == nil && len(healthy) > 0 {
		return healthy[rand.Intn(len(healthy))], nil
	}

	// 健康查询失败或健康实例为空：降级到全量实例。
	instances, err := d.GetInstances(ctx, service)
	if err != nil {
		return nil, err
	}
	if len(instances) == 0 {
		return nil, fmt.Errorf("consul discovery: no available instance for service %q", service)
	}
	if herr != nil {
		log.WithFields(logrus.Fields{
			constants.LogFieldTraceID: middleware.GetTraceIDFromContext(ctx),
			"service":                 service,
			"err":                     herr.Error(),
			"fallback":                len(instances),
		}).Warnf("[consul:discovery] health query failed, fallback to all instances")
	} else {
		log.WithFields(logrus.Fields{
			constants.LogFieldTraceID: middleware.GetTraceIDFromContext(ctx),
			"service":                 service,
			"fallback":                len(instances),
		}).Warnf("[consul:discovery] no healthy instance, fallback to all instances (incl. unhealthy)")
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
	disc := NewDiscovery(consulAddr, 0, false)
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

// ===========================================================================
// 蓝绿 / 金丝雀发布：版本感知的加权路由选择（想法 2）
// ===========================================================================

// RoutingPolicy 描述某服务的流量分流策略。
//
// 语义：
//   - StableVersion：稳定版本号（Consul Meta["version"]）。为空表示不限制稳定版本
//     （即所有非金丝雀实例都算 stable）。
//   - CanaryVersion：金丝雀版本号（Consul Meta["version"]）。为空但 CanaryOnly=true
//     时，所有 Meta["canary"]="true" 的实例都算 canary。
//   - CanaryWeight：导向 canary 实例的流量百分比（0~100）。0 表示全走 stable（蓝绿
//     的「全稳定」态）；100 表示全走 canary。
//
// 典型用法：
//   - 金丝雀 10%：{CanaryWeight:10, CanaryVersion:"v1.5.0"}
//   - 蓝绿切流  ：{StableVersion:"v1.4.0", CanaryVersion:"v1.5.0", CanaryWeight:100}
//     表示 100% 走 v1.5.0。
type RoutingPolicy struct {
	StableVersion string `json:"stable_version"`
	CanaryVersion string `json:"canary_version"`
	CanaryWeight  int    `json:"canary_weight"`
}

// instanceIsCanary 判断实例是否属于金丝雀集合（按 Meta）。
func instanceIsCanary(inst *Instance, p RoutingPolicy) bool {
	if inst == nil {
		return false
	}
	if v, ok := inst.Meta["canary"]; ok && v == "true" {
		// 显式标记 canary 的实例，只要没指定 CanaryVersion 就归入 canary 集合。
		return p.CanaryVersion == "" || inst.Meta["version"] == p.CanaryVersion
	}
	if p.CanaryVersion != "" && inst.Meta["version"] == p.CanaryVersion {
		return true
	}
	return false
}

// instanceIsStable 判断实例是否属于稳定集合（非 canary 且版本匹配/不限）。
func instanceIsStable(inst *Instance, p RoutingPolicy) bool {
	if inst == nil || instanceIsCanary(inst, p) {
		return false
	}
	if p.StableVersion == "" {
		return true
	}
	return inst.Meta["version"] == p.StableVersion
}

// PickWithPolicy 按路由策略从健康实例中加权挑选一个实例。
//
// 行为：
//   - 将健康实例分为 stable / canary 两组；
//   - 以 CanaryWeight% 概率挑选 canary 组，否则挑 stable 组；
//   - 任一组为空时回退到另一组，保证流量不中断；两组皆空才报错。
//
// 该方法是「想法 2」蓝绿/金丝雀的路由核心，gateway 在每次请求解析下游时调用，
// 即可实现按版本比例的流量分流（即便只有单个 gateway 实例也成立）。
func (d *Discovery) PickWithPolicy(ctx context.Context, service string, policy RoutingPolicy) (*Instance, error) {
	healthy, herr := d.GetHealthyInstances(ctx, service)
	if herr != nil || len(healthy) == 0 {
		// 健康查询失败/无健康实例：降级到全量实例（与 Pick 一致）。
		all, aerr := d.GetInstances(ctx, service)
		if aerr != nil {
			return nil, aerr
		}
		if len(all) == 0 {
			return nil, fmt.Errorf("consul discovery: no available instance for service %q", service)
		}
		healthy = all
	}

	var stable, canary []*Instance
	for _, inst := range healthy {
		if instanceIsCanary(inst, policy) {
			canary = append(canary, inst)
		} else if instanceIsStable(inst, policy) {
			stable = append(stable, inst)
		} else {
			// 既非 canary 也非指定 stable 版本的实例：归入 stable 兜底，
			// 避免 StableVersion 配错时把所有实例都丢弃。
			stable = append(stable, inst)
		}
	}

	useCanary := false
	if len(canary) > 0 && len(stable) > 0 {
		useCanary = rand.Intn(100) < policy.CanaryWeight
	} else if len(canary) > 0 {
		// 没有 stable 实例时，宁可走 canary 也不整体不可用。
		useCanary = true
	}

	pool := stable
	if useCanary {
		pool = canary
	}
	if len(pool) == 0 {
		// 兜底：任意一组为空则用另一组。
		if len(canary) > 0 {
			pool = canary
		} else {
			pool = stable
		}
	}
	if len(pool) == 0 {
		return nil, fmt.Errorf("consul discovery: no routable instance for service %q under policy", service)
	}
	return pool[rand.Intn(len(pool))], nil
}

// RoutingPolicyKey 返回某服务在 Consul KV 中的路由策略 key。
const RoutingPolicyKeyPrefix = "blog/routing/"

// GetRoutingPolicy 从 Consul KV 读取指定服务的路由策略。
// key 为 blog/routing/<service> 的 JSON。不存在时返回零值策略（全 stable）。
func (d *Discovery) GetRoutingPolicy(ctx context.Context, service string) (RoutingPolicy, error) {
	var p RoutingPolicy
	path := fmt.Sprintf("/v1/kv/%s%s", RoutingPolicyKeyPrefix, service)
	var raw []struct {
		Value string `json:"Value"` // base64 编码
	}
	if err := d.get(ctx, path, "kv.get", service, &raw); err != nil {
		// KV 不存在（404）时视为无策略。
		return p, nil
	}
	if len(raw) == 0 || raw[0].Value == "" {
		return p, nil
	}
	dec, err := base64.StdEncoding.DecodeString(raw[0].Value)
	if err != nil {
		return p, fmt.Errorf("consul kv decode routing policy for %s: %w", service, err)
	}
	if err := json.Unmarshal(dec, &p); err != nil {
		return p, fmt.Errorf("consul kv parse routing policy for %s: %w", service, err)
	}
	return p, nil
}

// SetRoutingPolicy 将路由策略写入 Consul KV（base64 编码的 JSON）。
func (d *Discovery) SetRoutingPolicy(ctx context.Context, service string, p RoutingPolicy) error {
	payload, err := json.Marshal(p)
	if err != nil {
		return err
	}
	path := fmt.Sprintf("/v1/kv/%s%s", RoutingPolicyKeyPrefix, service)
	url := fmt.Sprintf("http://%s%s", d.consulAddr, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("consul kv put routing policy for %s: %w", service, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("consul kv put routing policy for %s: status %d", service, resp.StatusCode)
	}
	return nil
}
