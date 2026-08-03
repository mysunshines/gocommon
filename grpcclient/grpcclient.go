// Package grpcclient 提供微服务之间建立 gRPC 客户端连接的统一封装，
// 消解各服务手写 grpc.NewClient 的重复样板（insecure 凭证、round_robin 负载均衡、
// 健康检查、keepalive），并默认挂载 AuthForwardInterceptor 实现服务间调用时的
// 身份（Authorization）自动透传，使下游 GRPCAuthInterceptor 能继续校验调用方。
//
// 典型用法（参考 comment-service 调用 user-service）：
//
//	conn, err := grpcclient.Dial(cfg.UserService.Addr())
//	if err != nil { return err }
//	defer conn.Close()
//	userClient := user.NewUserServiceClient(conn)
//	resp, err := userClient.IsInBlacklist(ctx, &user.IsBlacklistRequest{...})
//
// 注意：Dial 默认使用 insecure 传输凭证，仅适用于内部可信网络（如 Docker bridge）。
// 跨不可信网络时请改用带 TLS 的自定义 DialOption 覆盖（见 WithTLSCredentials）。
package grpcclient

import (
	"context"
	"strings"
	"time"

	"github.com/mysunshines/gocommon/constants"
	"github.com/mysunshines/gocommon/middleware"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
)

// defaultServiceConfig 启用 round_robin 负载均衡与 gRPC 健康检查，
// 配合 grpc.WithDefaultServiceConfig 使用（空 serviceName 表示对健康检查整个连接）。
const defaultServiceConfig = `{
	"loadBalancingPolicy": "round_robin",
	"healthCheckConfig": { "serviceName": "" }
}`

// defaultKeepalive 客户端心跳：10s 探测一次，3s 超时，无活动流也发心跳（断线自动重连）。
var defaultKeepalive = keepalive.ClientParameters{
	Time:                10 * time.Second,
	Timeout:             3 * time.Second,
	PermitWithoutStream: true,
}

// Dial 创建到 target（host:port）的 *grpc.ClientConn，已内置：
//   - insecure 传输凭证（内部可信网络）；
//   - round_robin 负载均衡 + 健康检查；
//   - keepalive 心跳探测（断线自动重连）；
//   - AuthForwardInterceptor：自动把调用方 Authorization 透传到下游。
//
// opts 可追加额外的 DialOption（如自定义拦截器、TLS 凭证等），会与内置选项合并生效。
// 底层使用 grpc.NewClient（惰性连接：首个 RPC 才真正建连，连接失败不会在 Dial 阶段致命），
// 因此目标服务短暂不可用时调用方仍可启动。
func Dial(target string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	base := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(defaultServiceConfig),
		grpc.WithKeepaliveParams(defaultKeepalive),
		grpc.WithChainUnaryInterceptor(AuthForwardInterceptor()),
	}
	all := append(base, opts...)
	return grpc.NewClient(target, all...)
}

// AuthForwardInterceptor 返回一元客户端拦截器，用于在服务间调用时透传鉴权身份
// 和链路追踪 ID：
//  1. 优先取 GRPCAuthInterceptor 存入 ctx 的原始 token；同取存入 ctx 的 traceID；
//  2. 回退到入站 gRPC metadata 的 authorization / x-trace-id；
//  3. 取到则追加到出站 metadata，使下游能校验调用方并串联链路。
func AuthForwardInterceptor() grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req, reply interface{},
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		// 1) 优先使用 GRPCAuthInterceptor 存入 ctx 的 token
		if token, ok := middleware.GetGRPCToken(ctx); ok {
			ctx = metadata.AppendToOutgoingContext(ctx, "authorization", token)
			// token 可用时 traceID 一并从 context 透传
			if traceID := middleware.GetTraceIDFromContext(ctx); traceID != "" {
				ctx = metadata.AppendToOutgoingContext(ctx, strings.ToLower(constants.HeaderXTraceID), traceID)
			}
			return invoker(ctx, method, req, reply, cc, opts...)
		}
		// 2) 回退：从入站 metadata 透传 authorization 与 traceID
		if md, ok := metadata.FromIncomingContext(ctx); ok {
			if vals := md.Get("authorization"); len(vals) > 0 {
				ctx = metadata.AppendToOutgoingContext(ctx, "authorization", vals[0])
			}
			if vals := md.Get(strings.ToLower(constants.HeaderXTraceID)); len(vals) > 0 {
				ctx = metadata.AppendToOutgoingContext(ctx, strings.ToLower(constants.HeaderXTraceID), vals[0])
			}
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// ForwardAuth 便捷函数：在不使用 Dial 默认拦截器、需手动发起调用前，
// 将 ctx 中的 Authorization 与 traceID 透传到出站 ctx（逻辑同 AuthForwardInterceptor）。
func ForwardAuth(ctx context.Context) context.Context {
	if token, ok := middleware.GetGRPCToken(ctx); ok {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", token)
		if traceID := middleware.GetTraceIDFromContext(ctx); traceID != "" {
			ctx = metadata.AppendToOutgoingContext(ctx, strings.ToLower(constants.HeaderXTraceID), traceID)
		}
		return ctx
	}
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if vals := md.Get("authorization"); len(vals) > 0 {
			ctx = metadata.AppendToOutgoingContext(ctx, "authorization", vals[0])
		}
		if vals := md.Get(strings.ToLower(constants.HeaderXTraceID)); len(vals) > 0 {
			ctx = metadata.AppendToOutgoingContext(ctx, strings.ToLower(constants.HeaderXTraceID), vals[0])
		}
	}
	return ctx
}
