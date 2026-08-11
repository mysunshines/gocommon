package middleware

import (
	"context"
	"strings"
	"time"

	"github.com/mysunshines/gocommon/config"
	"github.com/mysunshines/gocommon/constants"
	"github.com/mysunshines/gocommon/log"
	"github.com/mysunshines/gocommon/metrics"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

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
// 记录每个 RPC 的方法、对端地址、耗时与错误（如有），便于问题排查与审计。
// 同时从 gRPC metadata 提取 Gateway 透传的 traceID，注入 context 并打印到日志，
// 实现 Gateway → 下游 gRPC 全链路串联。
func GRPCLoggingInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// 从 gRPC 入站 metadata 提取 traceID（由 Gateway forwardRequest 透传）
		traceID := ""
		if md, ok := metadata.FromIncomingContext(ctx); ok {
			if vals := md.Get(strings.ToLower(constants.HeaderXTraceID)); len(vals) > 0 {
				traceID = vals[0]
				// 注入 context，下游 MySQL/Redis 可通过 GetTraceIDFromContext 获取
				ctx = SetTraceIDToContext(ctx, traceID)
			}
		}

		start := time.Now()
		resp, err := handler(ctx, req)
		duration := time.Since(start)

		clientAddr := "unknown"
		if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
			clientAddr = p.Addr.String()
		}

		if err != nil {
			log.Errorf("[gRPC] traceID=%v | method=%s | client=%s | duration=%v | err=%v",
				traceID, info.FullMethod, clientAddr, duration, err)
		} else {
			log.Infof("[gRPC] traceID=%v | method=%s | client=%s | duration=%v",
				traceID, info.FullMethod, clientAddr, duration)
		}
		return resp, err
	}
}

// GRPCMethodTimeout 根据 gRPC 方法全名返回入站请求超时时间。
// 读取全局 config 的 Server.GRPC 段：列表/搜索/批量等慢方法（命中 SlowMethods 后缀）
// 给予 DefaultTimeoutSec * SlowMultiplier 的更长超时，避免被统一短超时误杀；
// 其余走 DefaultTimeoutSec。该配置由 HotConfig 热更写回，无需重启即可调整超时。
//
// 各服务的 grpcUnaryInterceptor 直接调用本函数替代原先硬编码的 grpcMethodTimeout。
func GRPCMethodTimeout(fullMethod string) time.Duration {
	g := config.Get().Server.GRPC
	base := time.Duration(g.DefaultTimeoutSec) * time.Second
	if base <= 0 {
		base = constants.DefaultGRPCUnaryTimeout * time.Second
	}
	mult := g.SlowMultiplier
	if mult <= 0 {
		mult = 2
	}
	for _, suffix := range g.SlowMethods {
		if strings.HasSuffix(fullMethod, suffix) {
			return time.Duration(float64(base) * mult)
		}
	}
	return base
}
