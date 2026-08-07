package consul

import (
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/mysunshines/gocommon/grpcclient"
	"github.com/mysunshines/gocommon/log"
)

// Discovery 是基于 Consul 健康实例的服务发现客户端。
//
// 它通过 Consul HTTP API（/v1/health/service/:service?passing）拉取指定服务的
// 健康实例并缓存到本地；调用方通过 Resolve 获取一个可用实例地址（随机挑选，
// 实现简单负载均衡）。Consul 不可用或某次刷新失败时复用上一次成功的缓存，
// 保证可用性。仅依赖标准库，与 consul 包的注册实现保持一致。
type Discovery struct {
	consulAddr string
	httpClient *http.Client
	ttl        time.Duration

	mu       sync.RWMutex
	cache    map[string][]string
	lastSeen map[string]time.Time
}

// NewDiscovery 创建服务发现客户端。consulAddr 为 Consul Agent 地址（如 "consul:8500"）。
// ttl 为缓存刷新周期，<=0 时使用默认 10s。
func NewDiscovery(consulAddr string, ttl time.Duration) *Discovery {
	if consulAddr == "" {
		consulAddr = "consul:8500"
	}
	if ttl <= 0 {
		ttl = 10 * time.Second
	}
	return &Discovery{
		consulAddr: consulAddr,
		httpClient: &http.Client{Timeout: 3 * time.Second},
		ttl:        ttl,
		cache:      make(map[string][]string),
		lastSeen:   make(map[string]time.Time),
	}
}

// Resolve 返回指定 Consul 服务（如 "user-service"）的一个可用实例地址（host:port）。
// 优先从本地缓存随机挑选；缓存为空时即时向 Consul 查询一次。彻底无可用实例时返回错误。
func (d *Discovery) Resolve(service string) (string, error) {
	if addrs := d.cached(service); len(addrs) > 0 {
		return addrs[rand.Intn(len(addrs))], nil
	}
	addrs, err := d.query(service)
	if err != nil {
		return "", err
	}
	if len(addrs) == 0 {
		return "", fmt.Errorf("consul discovery: no healthy instance for service %q", service)
	}
	d.mu.Lock()
	d.cache[service] = addrs
	d.lastSeen[service] = time.Now()
	d.mu.Unlock()
	return addrs[rand.Intn(len(addrs))], nil
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
				if addrs, err := d.query(s); err == nil && len(addrs) > 0 {
					d.mu.Lock()
					d.cache[s] = addrs
					d.lastSeen[s] = time.Now()
					d.mu.Unlock()
				}
			}
		}
	}
}

func (d *Discovery) cached(service string) []string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.cache[service]
}

// query 实时向 Consul 查询服务的最新 passing 实例地址列表。
func (d *Discovery) query(service string) ([]string, error) {
	url := fmt.Sprintf("http://%s/v1/health/service/%s?passing", d.consulAddr, service)
	resp, err := d.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("consul discovery: query %q: %w", service, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("consul discovery: query %q: unexpected status %d", service, resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("consul discovery: read %q: %w", service, err)
	}
	var entries []struct {
		Service struct {
			Address string `json:"Address"`
			Port    int    `json:"Port"`
		} `json:"Service"`
		Node struct {
			Address string `json:"Address"`
		} `json:"Node"`
	}
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("consul discovery: unmarshal %q: %w", service, err)
	}
	addrs := make([]string, 0, len(entries))
	for _, e := range entries {
		addr := e.Service.Address
		if addr == "" {
			addr = e.Node.Address
		}
		if addr == "" || e.Service.Port == 0 {
			continue
		}
		addrs = append(addrs, fmt.Sprintf("%s:%d", addr, e.Service.Port))
	}
	return addrs, nil
}

// ---------------------------------------------------------------------------
// grpcclient 集成：把 Consul 作为 grpcclient 的服务解析器
// ---------------------------------------------------------------------------

// aliasToService 维护 "proto 服务名(alias) -> Consul 服务名" 的映射，
// 因为 grpcclient.SendRequest 用 proto 的 full method name（如 user.v1.UserService）
// 作为解析键，而 Consul 里注册的服务名是业务名（如 user-service）。
var (
	aliasMu       sync.RWMutex
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
// 调用前需先用 RegisterAlias 声明别名映射；解析时若未找到映射则尝试直接用 alias 作为
// Consul 服务名（兼容注册名与 proto 服务名一致的情况）。内部启动后台刷新 goroutine。
func UseConsulDiscovery(consulAddr string) {
	disc := NewDiscovery(consulAddr, 0)
	grpcclient.SetServiceResolver(func(alias string) (*grpcclient.ServiceEntry, bool) {
		aliasMu.RLock()
		svc, ok := aliasToService[alias]
		aliasMu.RUnlock()
		if !ok {
			svc = alias // 兜底：直接用 alias 作为 consul 服务名
		}
		target, err := disc.Resolve(svc)
		if err != nil {
			log.Warnf("consul discovery: resolve %q -> %q failed: %v", alias, svc, err)
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
