package middleware

import (
	"context"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/mysunshines/gocommon/config"
	"github.com/mysunshines/gocommon/constants"
	"github.com/mysunshines/gocommon/log"
	"github.com/mysunshines/gocommon/metrics"
	"github.com/mysunshines/gocommon/observability"
	"github.com/sirupsen/logrus"
	"github.com/sony/gobreaker"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// GRPCRecoveryInterceptor 返回 gRPC 一元服务端拦截器，等价于 HTTP 层的
// RecoveryMiddleware：捕获业务 handler 中的 panic，记录指标与日志后转为
// Internal 错误返回，防止 panic 击穿整个进程。
//
// 为什么必需：gRPC-go 默认不恢复 handler 的 panic——任一 handler panic 都会
// 导致整个服务进程崩溃（连带拖垮同进程的其它请求）。挂载本拦截器后，
// 单个请求的 panic 只影响该请求本身。
//
// 挂载位置：建议放在拦截器链首位，使其包裹（含其它拦截器在内的）整条链路；
// service 为服务名（如 constants.ServiceNameArticle），用于 panic_counter_total 打标。
//
// 行为：
//   - 指标：metrics.RecordPanic(service) → panic_counter_total{service} +1
//     （Grafana Infrastructure Overview 的 Panic 面板数据来源）；
//   - 日志：打印方法名、panic 值与完整调用栈；
//   - 响应：返回 codes.Internal，客户端看到 "internal server error"，
//     不泄露 panic 细节（防信息泄露）。
func GRPCRecoveryInterceptor(service string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler) (resp interface{}, err error) {
		// 命名返回值：defer 中需改写 resp/err，转换为 Internal 错误
		defer func() {
			if r := recover(); r != nil {
				metrics.RecordPanic(service)
				log.Errorf("[gRPC-Panic] service=%s method=%s panic=%v\n%s",
					service, info.FullMethod, r, debug.Stack())
				resp = nil
				err = status.Error(codes.Internal, "internal server error")
			}
		}()
		return handler(ctx, req)
	}
}

// GRPCTimeoutInterceptor gRPC 一元拦截器（超时）
// 利用 info.FullMethod 实现按方法差异化超时，轻量查询走默认超时。
func GRPCTimeoutInterceptor(service string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
		// 超时控制（按方法名差异化）
		ctx, cancel := context.WithTimeout(ctx, grpcMethodTimeout(info.FullMethod))
		defer cancel()
		return handler(ctx, req)
	}
}

// GRPCCircuitBreakerInterceptor gRPC 一元拦截器（熔断）
func GRPCCircuitBreakerInterceptor(cb *gobreaker.CircuitBreaker) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
		// 熔断器保护
		return cb.Execute(func() (interface{}, error) {
			return handler(ctx, req)
		})
	}
}

// GRPCMetricsInterceptor 等价于 HTTP 层的 MetricsMiddleware：
// 记录每个 RPC 的调用次数与耗时（Prometheus 指标 rpc_requests_total /
// rpc_request_duration_seconds），与 HTTP 指标共用同一套 metrics 包。
// service 为服务名（如 constants.ServiceNameArticle），与 HTTP 侧取自同一来源。
//
// 注意：它读取 handler 返回的 error，通过 status.Code(err) 得到最终 gRPC 状态码
// （如 OK / Unauthenticated / InvalidArgument），因此认证失败等也会被计入对应 status 标签。
func GRPCMetricsInterceptor(service string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		duration := time.Since(start)

		metrics.RecordRPCRequest(service, info.FullMethod, status.Code(err).String(), duration)
		return resp, err
	}
}

// GRPCLoggingInterceptor 等价于 HTTP 层的 LoggingMiddleware：
// 记录每个 RPC 的 traceID / method / client / userId / status / latency，错误时带 err，
// 字段与 HTTP 访问日志保持一致，便于统一审计。同时从 gRPC metadata 提取 Gateway 透传的
// traceID，注入 context 并打印，实现 Gateway → 下游 gRPC 全链路串联。
// 当日志级别为 debug 时，额外以 [gRPC-REQ] / [gRPC-RESP] 打印请求与响应体（JSON）。
func GRPCLoggingInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// 用 W3C 传播器从入站 metadata 提取 traceparent（otelgrpc 客户端会自动注入），
		// 与 HTTP 侧保持同一套传播协议，形成跨服务真实 span 树（B 方案）。
		if md, ok := metadata.FromIncomingContext(ctx); ok {
			carrier := propagation.HeaderCarrier{}
			for k, v := range md {
				carrier[k] = v
			}
			ctx = otel.GetTextMapPropagator().Extract(ctx, carrier)
		}
		// 为本次 RPC 开一个 span（OTel 未初始化时 no-op，不影响日志流程）。
		tracer := observability.Tracer()
		spanCtx, span := tracer.Start(ctx, info.FullMethod)
		defer span.End()
		ctx = spanCtx

		// 从 gRPC 入站 metadata 提取 traceID（由 Gateway forwardRequest 透传），
		// 作为日志的 trace_id label（A 方案 / Loki 查询用）。
		traceID := observability.TraceIDFromContext(spanCtx)
		if md, ok := metadata.FromIncomingContext(ctx); ok {
			if vals := md.Get(strings.ToLower(constants.HeaderXTraceID)); len(vals) > 0 {
				traceID = vals[0]
			}
		}
		if traceID != "" {
			// 注入 context，下游 MySQL/Redis 可通过 GetTraceIDFromContext 获取
			ctx = SetTraceIDToContext(ctx, traceID)
		}

		start := time.Now()
		resp, err := handler(ctx, req)
		duration := time.Since(start)

		clientAddr := "unknown"
		if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
			clientAddr = p.Addr.String()
		}
		userID := "" // 匿名请求为空字符串，与 HTTP 的 userId 行为一致
		if id, ok := GetGRPCUserID(ctx); ok {
			userID = strconv.FormatUint(uint64(id), 10)
		}
		statusCode := status.Code(err).String()

		if err != nil {
			log.Errorf("[gRPC] traceID=%v | method=%s | client=%s | userId=%s | status=%s | latency=%v | err=%v",
				traceID, info.FullMethod, clientAddr, userID, statusCode, duration, err)
		} else {
			log.Infof("[gRPC] traceID=%v | method=%s | client=%s | userId=%s | status=%s | latency=%v",
				traceID, info.FullMethod, clientAddr, userID, statusCode, duration)
		}

		// debug 级别下打印请求/响应体，便于线上排障（需 SetLevel("debug") 开启）。
		// 注意：仅当 handler 成功返回后才打印 resp，避免对错误路径误打半成品。
		if log.GetLogger().IsLevelEnabled(logrus.DebugLevel) {
			if pm, ok := req.(proto.Message); ok {
				if b, e := protojson.Marshal(pm); e == nil {
					log.Debugf("[gRPC-REQ] traceID=%v | method=%s | body=%s", traceID, info.FullMethod, string(b))
				}
			}
			if err == nil {
				if pm, ok := resp.(proto.Message); ok {
					if b, e := protojson.Marshal(pm); e == nil {
						log.Debugf("[gRPC-RESP] traceID=%v | method=%s | body=%s", traceID, info.FullMethod, string(b))
					}
				}
			}
		}

		return resp, err
	}
}

// grpcMethodTimeout 根据 gRPC 方法全名返回入站请求超时时间。
// 读取全局 config 的 Server.GRPC 段：列表/搜索/批量等慢方法（命中 SlowMethods 后缀）
// 给予 DefaultTimeoutSec * SlowMultiplier 的更长超时，避免被统一短超时误杀；
// 其余走 DefaultTimeoutSec。该配置由 HotConfig 热更写回，无需重启即可调整超时。
func grpcMethodTimeout(fullMethod string) time.Duration {
	g := config.Get().Server.GRPC
	base := time.Duration(g.DefaultTimeoutSec) * time.Second
	if base <= 0 {
		base = constants.DefaultGRPCUnaryTimeout * time.Second
	}
	multi := g.SlowMultiplier
	if multi <= 0 {
		multi = 2
	}
	for _, suffix := range g.SlowMethods {
		if strings.HasSuffix(fullMethod, suffix) {
			return time.Duration(float64(base) * multi)
		}
	}
	return base
}

type grpcCtxKey string

const (
	grpcUserIDKey   grpcCtxKey = "grpc_user_id"
	grpcRoleKey     grpcCtxKey = "grpc_role"
	grpcUsernameKey grpcCtxKey = "grpc_username"
	grpcTokenKey    grpcCtxKey = "grpc_token"
)

// GRPCAuthInterceptor 返回 gRPC 一元服务端拦截器，用于补齐 gRPC 层的鉴权：
//   - 从入站 metadata 的 authorization 读取 Bearer Token；
//   - 若请求携带 Token：校验签名，无效则直接返回 Unauthenticated（杜绝伪造/过期令牌）；
//   - 若校验通过：将 user_id/role/username 注入 context，供下游 handler 通过
//     GetGRPCUserID 等读取，避免 handler 再从请求体盲目取 user_id（防 IDOR）；
//   - 若请求未携带 Token：放行（匿名），由具体 handler 自行决定是否要求登录。
//
// 与 HTTP 层 AuthMiddleware / CSRFMiddleware 形成互补：前端主流流量经网关→gRPC，
// 过去 gRPC 层完全没有鉴权，导致后端 gin 的 JWT 中间件形同虚设；本拦截器让鉴权真正落地。
func GRPCAuthInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		md, _ := metadata.FromIncomingContext(ctx)
		authHeaders := md.Get(constants.AuthMetadataKey)
		authHeader := ""
		if len(authHeaders) > 0 {
			authHeader = authHeaders[0]
		}

		if authHeader == "" {
			// 匿名请求：是否允许由 handler 决定
			return handler(ctx, req)
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 {
			return nil, status.Error(codes.Unauthenticated, "invalid authorization format")
		}
		if !strings.EqualFold(parts[0], strings.TrimSpace(constants.JWTAuthScheme)) {
			return nil, status.Error(codes.Unauthenticated, "unsupported authorization scheme")
		}

		claims, err := parseJWTClaims(parts[1])
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "invalid or expired token")
		}

		// 注入身份到 context
		if uid, ok := claims[constants.JWTClaimUserID]; ok {
			switch v := uid.(type) {
			case float64:
				ctx = context.WithValue(ctx, grpcUserIDKey, uint(v))
			case uint:
				ctx = context.WithValue(ctx, grpcUserIDKey, v)
			case int64:
				ctx = context.WithValue(ctx, grpcUserIDKey, uint(v))
			}
		}
		if role, ok := claims[constants.JWTClaimRole]; ok {
			ctx = context.WithValue(ctx, grpcRoleKey, role)
		}
		if username, ok := claims[constants.JWTClaimUsername]; ok {
			if s, ok := username.(string); ok {
				ctx = context.WithValue(ctx, grpcUsernameKey, s)
			}
		}
		// 保存原始 token，供服务间调用时由 grpcclient.AuthForwardInterceptor 透传到下游，
		// 避免 token 随 metadata 链丢失（例如 handler 用 context.Background() 发起下游调用时）。
		ctx = context.WithValue(ctx, grpcTokenKey, parts[1])

		return handler(ctx, req)
	}
}

// GetGRPCUserID 从 gRPC context 读取已认证的用户 ID。
// 第二个返回值表示请求是否携带了有效令牌（即是否已登录）。
func GetGRPCUserID(ctx context.Context) (uint, bool) {
	v, ok := ctx.Value(grpcUserIDKey).(uint)
	return v, ok
}

// GetGRPCRole 从 gRPC context 读取角色。
func GetGRPCRole(ctx context.Context) (interface{}, bool) {
	v := ctx.Value(grpcRoleKey)
	return v, v != nil
}

// GetGRPCUsername 从 gRPC context 读取用户名。
func GetGRPCUsername(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(grpcUsernameKey).(string)
	return v, ok
}

// GetGRPCToken 从 gRPC context 读取已认证请求的原始 JWT（不含 "Bearer " 前缀）。
// 供 grpcclient.AuthForwardInterceptor 在服务间调用时透传到下游，
// 使下游 GRPCAuthInterceptor 能继续校验调用方身份（防越权）。
// 未携带令牌的请求返回 ("", false)。
func GetGRPCToken(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(grpcTokenKey).(string)
	return v, ok && v != ""
}

// RequireGRPCAuth 便捷方法：要求已登录，否则返回 Unauthenticated。
// 各写操作 handler 在开头调用，确保未认证请求被拒绝。
func RequireGRPCAuth(ctx context.Context) (uint, error) {
	uid, ok := GetGRPCUserID(ctx)
	if !ok || uid == 0 {
		return 0, status.Error(codes.Unauthenticated, "authentication required")
	}
	return uid, nil
}
