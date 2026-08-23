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
	"strings"
	"time"

	"github.com/mysunshines/gocommon/constants"
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
	// Version 服务版本号（如 v1.4.0），写入 Consul Meta["version"]。
	// 用于蓝绿/金丝雀发布（gateway 按 version 分流）。为空时不写入。
	Version string
	// Canary 是否为金丝雀实例。true 时写入 Consul Meta["canary"]="true"，
	// gateway 的加权路由可据此将部分流量导向该实例。
	Canary bool
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
		addr = os.Getenv(constants.EnvAdvertiseAddr)
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
		check["HTTP"] = fmt.Sprintf("http://%s:%d%s", addr, r.HTTPPort, constants.HealthCheckPath)
	} else if r.GRPCPort > 0 {
		// 无 HTTP 端点时退化为 TCP 检查 gRPC 端口
		check["TCP"] = fmt.Sprintf("%s:%d", addr, r.GRPCPort)
	}

	port := r.GRPCPort
	if port == 0 {
		port = r.HTTPPort
	}

	// 把 gRPC 与 HTTP 端口都写入 Meta，使 Consul 成为端口信息的唯一事实源。
	// 同时写入 version / canary，供 gateway 蓝绿/金丝雀按版本分流。
	meta := map[string]string{
		"grpc_port": fmt.Sprintf("%d", r.GRPCPort),
		"http_port": fmt.Sprintf("%d", r.HTTPPort),
	}
	if r.Version != "" {
		meta["version"] = r.Version
	}
	if r.Canary {
		meta["canary"] = "true"
	}

	body := map[string]interface{}{
		"ID":      instanceID,
		"Name":    r.Name,
		"Address": addr,
		"Port":    port,
		"Check":   check,
		"Meta":    meta,
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

// CanaryFromEnv 从环境变量 BLOG_CANARY（或 CANARY）读取金丝雀标记，
// 值为 "true"/"1"/"yes"/"on"（大小写不敏感）时返回 true。
// 供各服务 main.go 注册到 Consul 时直接填充 Registration.Canary，
// 实现同一份镜像按环境变量区分 stable / canary 实例，无需改代码。
func CanaryFromEnv() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("BLOG_CANARY")))
	if v == "" {
		v = strings.ToLower(strings.TrimSpace(os.Getenv("CANARY")))
	}
	switch v {
	case "true", "1", "yes", "on":
		return true
	default:
		return false
	}
}

// VersionFromEnv 返回服务注册到 Consul 时使用的版本号。
// 优先读取环境变量 SERVICE_VERSION（金丝雀/蓝绿发布时由部署脚本注入，
// 例如 make canary-deploy SERVICE=article-service CANARY_VERSION=v1.5.1）；
// 若未设置则回退到 defaultVersion（通常是 -ldflags "-X main.Version=xxx" 注入的构建版本）。
// 这样同一份镜像既能在常规部署下上报真实构建版本，
// 也能在临时起金丝雀副本时通过 env 覆盖为 canary 版本号，供路由策略按版本选路。
func VersionFromEnv(defaultVersion string) string {
	if v := strings.TrimSpace(os.Getenv("SERVICE_VERSION")); v != "" {
		return v
	}
	return defaultVersion
}
