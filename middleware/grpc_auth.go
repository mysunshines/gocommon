package middleware

import (
	"context"
	"strings"

	"github.com/mysunshines/gocommon/constants"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

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
		authHeaders := md.Get("authorization")
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
		if uid, ok := claims["user_id"]; ok {
			switch v := uid.(type) {
			case float64:
				ctx = context.WithValue(ctx, grpcUserIDKey, uint(v))
			case uint:
				ctx = context.WithValue(ctx, grpcUserIDKey, v)
			case int64:
				ctx = context.WithValue(ctx, grpcUserIDKey, uint(v))
			}
		}
		if role, ok := claims["role"]; ok {
			ctx = context.WithValue(ctx, grpcRoleKey, role)
		}
		if username, ok := claims["username"]; ok {
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
