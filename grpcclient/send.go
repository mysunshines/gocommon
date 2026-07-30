package grpcclient

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

// serviceRegistry 服务名 -> gRPC 目标地址(host:port)。
// 调用方在启动时通过 RegisterService 写入其下游依赖地址；SendRequest 据此解析目标。
// 约定：注册键与 SendRequest 的 api 前缀保持一致，通常取 proto 全限定服务名，
// 例如 grpcclient.RegisterService("user.UserService", "user-service:9002")。
var serviceRegistry sync.Map // map[string]string

// serviceResolver 自定义服务解析器（可选），覆盖默认注册表，例如对接 Consul 健康查询。
var serviceResolver func(name string) (target string, ok bool)

// RegisterService 注册服务名到 gRPC 目标地址。服务名即 SendRequest 的 api 中
// "<Service>/<Method>" 的 Service 部分（建议用 proto 全限定服务名，如 "user.UserService"）。
func RegisterService(name, target string) {
	serviceRegistry.Store(name, target)
}

// SetServiceResolver 设置自定义服务解析器，覆盖默认注册表（用于对接 Consul 等服务发现）。
// 解析器返回 (target, true) 时优先于 RegisterService 注册表。
func SetServiceResolver(fn func(name string) (target string, ok bool)) {
	serviceResolver = fn
}

// connCache 目标地址 -> *grpc.ClientConn，按地址复用连接。
// grpcclient.Dial 已内置 insecure 凭证、round_robin 负载均衡、健康检查、keepalive 重连，
// 并默认挂载 AuthForwardInterceptor（自动透传 Authorization 到下游）。
var connCache sync.Map // map[string]*grpc.ClientConn

func getConn(target string) (*grpc.ClientConn, error) {
	if v, ok := connCache.Load(target); ok {
		return v.(*grpc.ClientConn), nil
	}
	conn, err := Dial(target)
	if err != nil {
		return nil, err
	}
	// 并发下以先存入者为准，其余关闭避免泄漏
	if actual, loaded := connCache.LoadOrStore(target, conn); loaded {
		_ = conn.Close()
		return actual.(*grpc.ClientConn), nil
	}
	return conn, nil
}

// SendRequest 根据 api（格式 "<Service>/<Method>"，开头 "/" 可省略，如
// "user.UserService/IsInBlacklist"）解析服务名与方法名，内部解析目标地址并发起 gRPC
// 一元调用。相比为每个下游生成强类型客户端，本函数统一了连接管理与鉴权透传：
//
//   - 地址解析：优先 serviceResolver，其次 RegisterService 注册的地址；
//   - 连接复用：按目标地址缓存 *grpc.ClientConn（含重连与鉴权透传）；
//   - 鉴权透传：复用 Dial 内置的 AuthForwardInterceptor，下游写方法自动带身份；
//   - req/resp 需为 proto.Message（直接传入目标服务 pb 生成的请求/响应结构体即可）。
//
// 典型用法：
//
//	grpcclient.RegisterService("user.UserService", cfg.UserService.Addr())
//	var resp user.IsBlacklistResponse
//	err := grpcclient.SendRequest(ctx, "user.UserService/IsInBlacklist",
//	    &user.IsBlacklistRequest{UserId: 1, TargetUserId: 2}, &resp)
//
// 注意：api 的 Service 部分应为 proto 全限定服务名（含包名），这样 conn.Invoke 的
// 全方法名 "/<Service>/<Method>" 才能与服务端精确匹配。
func SendRequest(ctx context.Context, api string, req, resp proto.Message) error {
	serviceName, method, err := parseAPI(api)
	if err != nil {
		return err
	}

	target, ok := resolveTarget(serviceName)
	if !ok {
		return fmt.Errorf("grpcclient: 未注册服务 %q 的地址，请调用 RegisterService 或 SetServiceResolver", serviceName)
	}

	conn, err := getConn(target)
	if err != nil {
		return fmt.Errorf("grpcclient: 建立到 %s 的连接失败: %w", target, err)
	}

	fullMethod := "/" + serviceName + "/" + method
	if err := conn.Invoke(ctx, fullMethod, req, resp); err != nil {
		return fmt.Errorf("grpcclient: 调用 %s 失败: %w", fullMethod, err)
	}
	return nil
}

// parseAPI 将 "Service/Method"（或 "/Service/Method"）拆分为服务名与方法名。
func parseAPI(api string) (service, method string, err error) {
	s := strings.TrimPrefix(api, "/")
	idx := strings.LastIndex(s, "/")
	if idx < 0 {
		return "", "", fmt.Errorf("invalid api %q: 期望格式 <Service>/<Method>", api)
	}
	service = s[:idx]
	method = s[idx+1:]
	if service == "" || method == "" {
		return "", "", fmt.Errorf("invalid api %q: 服务名或方法名为空", api)
	}
	return service, method, nil
}

// resolveTarget 解析服务名对应的目标地址：优先自定义解析器，其次注册表。
func resolveTarget(service string) (string, bool) {
	if serviceResolver != nil {
		if t, ok := serviceResolver(service); ok {
			return t, true
		}
	}
	if t, ok := serviceRegistry.Load(service); ok {
		return t.(string), true
	}
	return "", false
}
