package middleware

import (
	"context"
	"time"

	"github.com/mysunshines/gocommon/log"
	"github.com/mysunshines/gocommon/metrics"

	"google.golang.org/grpc"
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
func GRPCLoggingInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		duration := time.Since(start)

		clientAddr := "unknown"
		if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
			clientAddr = p.Addr.String()
		}

		if err != nil {
			log.Errorf("[gRPC] %s client=%s duration=%v err=%v", info.FullMethod, clientAddr, duration, err)
		} else {
			log.Infof("[gRPC] %s client=%s duration=%v", info.FullMethod, clientAddr, duration)
		}
		return resp, err
	}
}
