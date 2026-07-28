// Package consul 提供微服务向 Consul Agent 注册自身、以及优雅注销的轻量实现。
//
// 设计目标：
//   - 不依赖第三方 consul SDK，仅用标准库 + gocommon/log 调用 Consul HTTP API。
//   - 注册失败时仅返回 error（不致命），便于开发/单机环境无 Consul 时降级运行。
//   - 注册地址默认自动探测本机非 loopback IPv4（Docker bridge 网络内即容器 IP，
//     同网络内其它容器——含 Consul——均可达）；可用 ADVERTISE_ADDR 环境变量覆盖。
//
// 适用场景：各微服务启动时调用 Register，进程退出时执行返回的 deregister 函数，
// 使 Consul 后台能正确显示服务列表并在实例下线后摘除。
package consul

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/mysunshines/gocommon/log"
)

// Registration 描述一个待注册到 Consul 的服务实例。
type Registration struct {
	// Name 服务名（如 user-service），对应 Consul 的 Service.Name
	Name string
	// ConsulAddress Consul Agent 地址，形如 host:port（如 consul:8500）
	ConsulAddress string
	// Address 注册到 Consul 的实例地址（必须让 Consul/同网容器可达）。
	// 为空时依次回退到 ADVERTISE_ADDR 环境变量、本机非 loopback IPv4。
	Address string
	// GRPCPort 作为 Consul Service.Port，便于其它服务发现后直连 gRPC。无 gRPC 时填 0。
	GRPCPort int
	// HTTPPort 仅用于拼接 HTTP 健康检查地址（/health）。无 HTTP 时填 0。
	HTTPPort int
	// CheckInterval 健康检查间隔（秒），默认 10
	CheckInterval int
	// DeregisterCritical 实例持续 critical 多久后自动注销（秒），默认 30
	DeregisterCritical int
}

// Register 将服务注册到 Consul，并返回注销函数。
// 注册失败时返回 error（调用方可选择仅告警），便于无 Consul 环境降级。
func Register(r Registration) (func() error, error) {
	if r.ConsulAddress == "" {
		return nil, fmt.Errorf("consul: ConsulAddress is required")
	}
	if r.CheckInterval <= 0 {
		r.CheckInterval = 10
	}
	if r.DeregisterCritical <= 0 {
		r.DeregisterCritical = 30
	}

	addr := r.Address
	if addr == "" {
		addr = os.Getenv("ADVERTISE_ADDR")
	}
	if addr == "" {
		addr = detectOutboundIP()
	}

	instanceID := fmt.Sprintf("%s-%s", r.Name, addr)
	if r.GRPCPort > 0 {
		instanceID = fmt.Sprintf("%s-%d", instanceID, r.GRPCPort)
	} else if r.HTTPPort > 0 {
		instanceID = fmt.Sprintf("%s-%d", instanceID, r.HTTPPort)
	}

	check := map[string]interface{}{
		"Interval":                       fmt.Sprintf("%ds", r.CheckInterval),
		"Timeout":                        "5s",
		"DeregisterCriticalServiceAfter": fmt.Sprintf("%ds", r.DeregisterCritical),
	}
	if r.HTTPPort > 0 {
		// 优先 HTTP /health 检查（所有微服务均已实现 /health）
		check["HTTP"] = fmt.Sprintf("http://%s:%d/health", addr, r.HTTPPort)
	} else if r.GRPCPort > 0 {
		// 无 HTTP 端点时退化为 TCP 检查 gRPC 端口
		check["TCP"] = fmt.Sprintf("%s:%d", addr, r.GRPCPort)
	}

	port := r.GRPCPort
	if port == 0 {
		port = r.HTTPPort
	}

	body := map[string]interface{}{
		"ID":      instanceID,
		"Name":    r.Name,
		"Address": addr,
		"Port":    port,
		"Check":   check,
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("consul: marshal registration: %w", err)
	}

	if err := putAgent(r.ConsulAddress, "/v1/agent/service/register", payload); err != nil {
		return nil, err
	}
	log.Infof("consul: registered %s (id=%s) via %s", r.Name, instanceID, r.ConsulAddress)

	deregister := func() error {
		return putAgent(r.ConsulAddress, "/v1/agent/service/deregister/"+instanceID, nil)
	}
	return deregister, nil
}

// putAgent 调用 Consul Agent 接口；body 为 nil 表示无请求体（如 deregister）。
func putAgent(consulAddr, path string, body []byte) error {
	url := fmt.Sprintf("http://%s%s", consulAddr, path)
	var req *http.Request
	var err error
	if body != nil {
		req, err = http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req, err = http.NewRequest(http.MethodPut, url, nil)
	}
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("consul: request %s failed: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("consul: %s returned status %d", path, resp.StatusCode)
	}
	return nil
}

// detectOutboundIP 返回本机非 loopback 的 IPv4，作为 Consul 注册地址。
// Docker bridge 网络下即容器 IP，同网络内其它容器（含 Consul）可达。
func detectOutboundIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ip := ipnet.IP.To4(); ip != nil {
				return ip.String()
			}
		}
	}
	return "127.0.0.1"
}
