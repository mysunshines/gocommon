package grpcclient

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mysunshines/gocommon/log"
	"github.com/mysunshines/gocommon/middleware"
	"github.com/mysunshines/gocommon/resilience"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// ServiceEntry 注册表中的一项：逻辑服务名对应的真实 proto 服务名与 gRPC 目标地址。
type ServiceEntry struct {
	// Service 是 proto 全限定服务名（包名.服务名），如 "user.UserService"，
	// 用于构造 gRPC 全方法名 "/user.UserService/Method"，必须与服务端精确匹配。
	Service string
	// Target 是 gRPC 目标地址（host:port），如 "user-service:9002"。
	Target string
}

// serviceRegistry 服务查找键 -> ServiceEntry。
// 调用方在启动时通过 RegisterService 写入其下游依赖；SendRequest 据此解析目标。
// 同一个下游会以两个键写入，两种调用风格因此都能命中同一项：
//   - 逻辑名(alias)，如 "user.v1"，对应 api "user.v1.IsInBlacklist"；
//   - proto 全限定服务名，如 "user.v1.UserService"，对应下游 pb 生成的
//     xxx_FullMethodName 常量（"/user.v1.UserService/IsInBlacklist"）。
//
// 例如 grpcclient.RegisterService("user.v1", "user.v1.UserService", "user-service:9002")。
var serviceRegistry sync.Map // map[string]*ServiceEntry

// serviceResolver 自定义服务解析器（可选），覆盖默认注册表，例如对接 Consul 健康查询。
// 接收 ctx 以便从请求链路提取 traceID，使解析失败日志能按 traceID 串联。
var serviceResolver func(ctx context.Context, name string) (*ServiceEntry, bool)

// RegisterService 注册逻辑服务名到 gRPC 目标地址，并声明其 proto 全限定服务名。
//   - alias：调用侧 api 使用的逻辑名，形如 "user.v1"，与 HTTP API 版本化风格保持一致；
//   - service：proto 全限定服务名（包名.服务名），如 "user.v1.UserService"，用于构造 gRPC 全方法名；
//   - target：gRPC 目标地址（host:port）。
//
// 注册后两种调用风格等价，均命中同一项：
//
//	grpcclient.SendRequest(ctx, user.UserService_IsInBlacklist_FullMethodName, req, resp) // 推荐
//	grpcclient.SendRequest(ctx, "user.v1.IsInBlacklist", req, resp)
func RegisterService(alias, service, target string) {
	entry := &ServiceEntry{Service: service, Target: target}
	serviceRegistry.Store(alias, entry)
	// 同时以 proto 全限定服务名为键注册，使调用侧可直接传入下游 pb 生成的
	// xxx_FullMethodName 常量（形如 "/user.v1.UserService/IsInBlacklist"），
	// 避免手写字符串拼接方法名。
	if service != "" && service != alias {
		serviceRegistry.Store(service, entry)
	}
}

// SetServiceResolver 设置自定义服务解析器，覆盖默认注册表（用于对接 Consul 等服务发现）。
// 解析器返回 (*ServiceEntry, true) 时优先于 RegisterService 注册表。
// 解析器接收 ctx 以便从请求链路提取 traceID，使解析失败日志能按 traceID 串联。
func SetServiceResolver(fn func(ctx context.Context, name string) (*ServiceEntry, bool)) {
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

// SendRequest 根据 api 解析服务与方法并发起 gRPC 一元调用。
//
// api 推荐直接使用下游服务 pb 生成的全方法名常量，由 proto 定义单一来源产出，
// 方法改名或包名变更时编译期即可暴露，无需手写字符串：
//
//	user.UserService_IsInBlacklist_FullMethodName // "/user.v1.UserService/IsInBlacklist"
//
// 同时兼容点号风格 "<alias>.<Method>"（如 "user.v1.IsInBlacklist"），适用于调用侧
// 不便引入下游 pb 包的场景。两种写法最终都映射为 gRPC 全方法名 "/<protoService>/<Method>"。
//
// 相比为每个下游生成强类型客户端，本函数统一了连接管理与鉴权透传：
//
//   - 地址解析：优先 serviceResolver，其次 RegisterService 注册的逻辑名；
//   - 连接复用：按目标地址缓存 *grpc.ClientConn（含重连与鉴权透传）；
//   - 鉴权透传：复用 Dial 内置的 AuthForwardInterceptor，下游写方法自动带身份；
//   - req/resp 需为 proto.Message（直接传入目标服务 pb 生成的请求/响应结构体即可）。
//
// 典型用法：
//
//	grpcclient.RegisterService(constants.UserServiceV1Alias, constants.UserServiceV1Service, cfg.UserService.Addr())
//	var resp user.IsBlacklistResponse
//	err := grpcclient.SendRequest(ctx, user.UserService_IsInBlacklist_FullMethodName,
//	    &user.IsBlacklistRequest{UserId: 1, TargetUserId: 2}, &resp)
//
// 注意：api 的 alias 在注册时须绑定到真实的 proto 全限定服务名（含包名），这样
// conn.Invoke 的全方法名 "/<protoService>/<Method>" 才能与服务端精确匹配。
func SendRequest(ctx context.Context, api string, req, resp proto.Message) error {
	return SendRequestWithFallback(ctx, api, req, resp, nil)
}

// SendRequestWithFallback 是 SendRequest 的增强版：在发起 gRPC 一元调用前，
// 依据 resilience.ForService(alias) 返回的 Policy 施加超时、限流与熔断控制；
// 当熔断打开或调用失败且提供了 fallback 时，调用 fallback 返回兜底响应，
// 避免下游故障向上游雪崩。fallback 可为 nil（不降级，错误直接上抛）。
//
// 降级函数应负责填充 resp（若调用方需要兜底内容），其返回值仅用于表达降级是否成功；
// 函数签名与 Policy.Execute 对齐，便于直接复用。
func SendRequestWithFallback(ctx context.Context, api string, req, resp proto.Message, fallback func(ctx context.Context) error) error {
	alias, method, err := parseAPI(api)
	if err != nil {
		return err
	}

	entry, ok := resolveTarget(ctx, alias)
	if !ok {
		return fmt.Errorf("grpcclient: 未注册服务 %q 的地址，请调用 RegisterService 或 SetServiceResolver", alias)
	}

	conn, err := getConn(entry.Target)
	if err != nil {
		return fmt.Errorf("grpcclient: 建立到 %s 的连接失败: %w", entry.Target, err)
	}

	fullMethod := "/" + entry.Service + "/" + method

	// 将 alias 作为 serviceKey 注入 context，使 resilience 按下游区分熔断/限流。
	policy := resilience.ForService(alias)
	ctx = resilience.WithServiceKey(ctx, alias)

	traceID := middleware.GetTraceIDFromContext(ctx)
	ctxKV := middleware.DumpContext(ctx)
	start := time.Now()

	if log.IsDebug() {
		if reqJSON, err := protojson.Marshal(req); err == nil {
			log.Debugf("[gRPC-Client] traceID=%v | ctxKV=%v | method=%s | target=%s | req=%s",
				traceID, ctxKV, fullMethod, entry.Target, string(reqJSON))
		}
	}

	invoke := func(c context.Context) error {
		return conn.Invoke(c, fullMethod, req, resp)
	}

	// 降级钩子：fallback 负责填充 resp，并对 ctx deadline 友好。
	var fb func(c context.Context) (interface{}, error)
	if fallback != nil {
		fb = func(c context.Context) (interface{}, error) {
			return nil, fallback(c)
		}
	}

	invokeErr := policy.Execute(ctx, invoke, fb)
	latency := time.Since(start)

	if invokeErr != nil {
		log.Errorf("[gRPC-Client] traceID=%v | ctxKV=%v | method=%s | target=%s | latency=%v | err=%v",
			traceID, ctxKV, fullMethod, entry.Target, latency, invokeErr)
		return fmt.Errorf("grpcclient: 调用 %s 失败: %w", fullMethod, invokeErr)
	}
	if log.IsDebug() {
		if respJSON, err := protojson.Marshal(resp); err == nil {
			log.Debugf("[gRPC-Client] traceID=%v | ctxKV=%v | method=%s | target=%s | latency=%v | resp=%s",
				traceID, ctxKV, fullMethod, entry.Target, latency, string(respJSON))
		}
	}
	log.Infof("[gRPC-Client] traceID=%v | method=%s | target=%s | latency=%v",
		traceID, fullMethod, entry.Target, latency)
	return nil
}

// parseAPI 将 api 拆分为逻辑服务名(alias)与方法名，兼容两种风格：
//   - 新点号风格 "alias.method"（如 "user.v1.IsInBlacklist"），按最后一个 '.' 切分；
//   - 旧斜杠风格 "Service/Method"（如 "user.UserService/IsInBlacklist"），按最后一个 '/' 切分。
//
// 调用侧统一使用点号风格，与服务端 proto 服务名解耦。
func parseAPI(api string) (alias, method string, err error) {
	s := strings.TrimPrefix(api, "/")
	if idx := strings.LastIndex(s, "/"); idx >= 0 {
		alias, method = s[:idx], s[idx+1:]
	} else if idx := strings.LastIndex(s, "."); idx >= 0 {
		alias, method = s[:idx], s[idx+1:]
	} else {
		return "", "", fmt.Errorf("invalid api %q: 期望格式 <alias>.<Method> 或 <Service>/<Method>", api)
	}
	if alias == "" || method == "" {
		return "", "", fmt.Errorf("invalid api %q: 服务名或方法名为空", api)
	}
	return alias, method, nil
}

// resolveTarget 解析逻辑服务名对应的注册项：优先自定义解析器，其次注册表。
// ctx 透传至解析器以便串联 traceID。
func resolveTarget(ctx context.Context, alias string) (*ServiceEntry, bool) {
	if serviceResolver != nil {
		if e, ok := serviceResolver(ctx, alias); ok {
			return e, true
		}
	}
	if e, ok := serviceRegistry.Load(alias); ok {
		return e.(*ServiceEntry), true
	}
	return nil, false
}
