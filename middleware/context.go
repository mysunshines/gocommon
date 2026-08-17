package middleware

import (
	"context"
	"fmt"
	"reflect"
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

// DumpContext 遍历 context 链，返回其中携带的所有 key/value（均转成 readable 字符串）。
// 用于 debug 日志打印链路上下文（trace_id、user_id 等），避免逐一手动取字段漏打。
//
// 实现说明：标准库未提供遍历 context value 的公开 API，此处通过反射读取
// valueCtx.Key/Value 字段，并沿 Context 父链向上；当 ctx 为 *gin.Context 时，
// 额外读取其 Keys map（gin 的 Value 走方法而非字段，反射字段取不到）。
// 反射遍历设置了深度上限与已访问指针去重，避免异常 context 导致死循环。
func DumpContext(ctx context.Context) map[string]string {
	out := map[string]string{}
	if ctx == nil {
		return out
	}

	// *gin.Context：直接读其 Keys map。
	if gc, ok := ctx.(*gin.Context); ok {
		for k, v := range gc.Keys {
			out[k] = fmt.Sprintf("%v", v)
		}
	}

	v := reflect.ValueOf(ctx)
	visited := map[uintptr]bool{}
	for depth := 0; depth < 16 && v.IsValid() && !v.IsNil(); depth++ {
		if v.Kind() == reflect.Ptr && v.Elem().IsValid() {
			// valueCtx 携带 Key/Value 字段。
			if key := v.Elem().FieldByName("Key"); key.IsValid() && key.CanInterface() {
				if val := v.Elem().FieldByName("Value"); val.IsValid() && val.CanInterface() {
					out[fmt.Sprintf("%v", key.Interface())] = fmt.Sprintf("%v", val.Interface())
				}
			}
			// 沿 Context 父链向上。
			parent := v.Elem().FieldByName("Context")
			if !parent.IsValid() || !parent.CanInterface() {
				break
			}
			pv := reflect.ValueOf(parent.Interface())
			if pv.Kind() == reflect.Ptr && pv.Pointer() != 0 {
				if visited[pv.Pointer()] {
					break
				}
				visited[pv.Pointer()] = true
			}
			v = pv
			continue
		}
		break
	}
	return out
}
