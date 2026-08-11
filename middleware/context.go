package middleware

import (
	"context"
	"strconv"
	"time"

	"github.com/mysunshines/gocommon/constants"
	"github.com/mysunshines/gocommon/util"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// UserIDKey 用于标准 context.Context 存储 user_id（跨 goroutine / 下游组件传递）。
type contextKey string

const UserIDKey contextKey = "user_id"

// UserIDContextKey 用于 gin.Context.Set/Get 的 string 键
// 与 UserIDKey 值相同，但类型为 string，可直接用于 gin.Context
const UserIDContextKey = "user_id"

// UsernameContextKey 用于 gin.Context.Set/Get 的 string 键
const UsernameContextKey = "username"

// RoleContextKey 用于 gin.Context.Set/Get 的 string 键
const RoleContextKey = "role"

// traceIDKey 用于 context.Context 存储 traceID，仅在包内使用。
const traceIDKey contextKey = "trace_id"

// GetUserIDFromContext 从 gin.Context 提取已认证的用户 ID。
// 第二个返回值在底层依赖中保留错误语义，但当前实现中未设置时返回 (0, nil)，
// 调用方应按 "== 0 表示未登录" 判断。
func GetUserIDFromContext(c *gin.Context) (uint, error) {
	userID, exists := c.Get(UserIDContextKey)
	if !exists {
		return 0, nil
	}
	switch v := userID.(type) {
	case uint:
		return v, nil
	case float64:
		return uint(v), nil
	case string:
		id, _ := strconv.ParseUint(v, 10, 64)
		return uint(id), nil
	default:
		return 0, jwt.ErrTokenInvalidClaims
	}
}

// GetUsernameFromContext 从 gin.Context 提取已认证的用户名，未设置时返回空串。
func GetUsernameFromContext(c *gin.Context) string {
	username, exists := c.Get(UsernameContextKey)
	if !exists {
		return ""
	}
	if name, ok := username.(string); ok {
		return name
	}
	return ""
}

// GetTraceIDFromContext 从 context.Context 中提取链路追踪 traceID。
// MySQL/Redis/gRPC 拦截器等非 HTTP Handler 代码通过此函数获取 traceID 打印日志。
// 若未设置则返回空串，调用方自行降级处理。
func GetTraceIDFromContext(ctx context.Context) string {
	v, ok := ctx.Value(traceIDKey).(string)
	if !ok {
		return ""
	}
	return v
}

// SetTraceIDToContext 将 traceID 写入 context.Context，返回新的 context。
// 主要用于 gRPC 拦截器从 metadata 提取 traceID 后注入 context，后续链路可统一获取。
func SetTraceIDToContext(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceIDKey, traceID)
}

func generateTraceID() string {
	return time.Now().Format(constants.DateTimeCompact) + "-" + util.GenerateRandomString(8)
}
