package middleware

import (
	"context"
	"strconv"
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
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	logrus "github.com/sirupsen/logrus"
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
// 记录每个 RPC 的 traceID / method / client / userId / status / latency，错误时带 err，
// 字段与 HTTP 访问日志保持一致，便于统一审计。同时从 gRPC metadata 提取 Gateway 透传的
// traceID，注入 context 并打印，实现 Gateway → 下游 gRPC 全链路串联。
// 当日志级别为 debug 时，额外以 [gRPC-REQ] / [gRPC-RESP] 打印请求与响应体（JSON）。
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
