package middleware

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mysunshines/gocommon/config"
	"github.com/mysunshines/gocommon/constants"
	"github.com/mysunshines/gocommon/log"
	"github.com/mysunshines/gocommon/metrics"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	logrus "github.com/sirupsen/logrus"
)

// bodyCaptureWriter 在 debug 级别下包装 gin.ResponseWriter，截获响应体以便记录日志。
// 仅在日志级别为 debug 时启用，非 debug 路径不包装，无性能损耗。
type bodyCaptureWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w *bodyCaptureWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func (w *bodyCaptureWriter) WriteString(s string) (int, error) {
	w.body.WriteString(s)
	return w.ResponseWriter.WriteString(s)
}

var (
	jwtSecret string
	jwtOnce   sync.Once
)

// InitJWT 初始化 JWT 签名密钥（幂等），供 AuthMiddleware / CSRFMiddleware 解析令牌使用。
func InitJWT(secret string) {
	jwtOnce.Do(func() {
		jwtSecret = secret
	})
}

func RateLimitMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if rateLimiter == nil {
			c.Next()
			return
		}

		// 限流 key：已登录用户用 userID（防账号级刷接口），未登录用客户端 IP（防单 IP 刷）。
		// 这样写操作（发布/评论/注册/登录）即便攻击者换 IP，也会受单账号限速约束。
		key := c.ClientIP()
		if uid, err := GetUserIDFromContext(c); err == nil && uid != 0 {
			key = "u:" + strconv.FormatUint(uint64(uid), 10)
		}

		if !rateLimiter.Allow(c.Request.URL.Path, key) {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"code":    429,
				"message": "Too Many Requests",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

func MetricsMiddleware(service string) gin.HandlerFunc {
	return func(c *gin.Context) {
		metrics.IncrementInFlight()
		start := time.Now()

		c.Next()

		duration := time.Since(start)
		status := c.Writer.Status()
		metrics.RecordRequest(service, c.Request.Method, c.FullPath(), status, duration)
	}
}

func LoggingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery

		// 仅在 debug 级别下记录请求/响应体：先克隆请求体（还原供下游 handler 读取），
		// 并包装 ResponseWriter 以截获响应体。非 debug 路径不触发，零开销。
		debugEnabled := log.GetLogger().IsLevelEnabled(logrus.DebugLevel)
		var reqBody []byte
		if debugEnabled {
			if c.Request.Body != nil {
				reqBody, _ = io.ReadAll(io.LimitReader(c.Request.Body, 4096))
				c.Request.Body = io.NopCloser(bytes.NewReader(reqBody)) // 还原，避免下游读不到 body
			}
			c.Writer = &bodyCaptureWriter{ResponseWriter: c.Writer, body: &bytes.Buffer{}}
		}

		c.Next()

		latency := time.Since(start)
		clientIP := c.ClientIP()
		method := c.Request.Method
		statusCode := c.Writer.Status()
		timestamp := time.Now().Format(constants.DateTimeFormat)

		if raw != "" {
			path = path + "?" + raw
		}

		// 从 gin.Context 取链路追踪 ID（由前置的 TraceMiddleware 注入），
		// 取不到时降级为空串，避免日志缺字段。traceID 用于跨服务串联同一请求，方便 debug。
		traceID, _ := c.Get(constants.HeaderXTraceID)

		// 从 gin.Context 取 userId（由前置的 AuthMiddleware 注入），
		// 未登录接口取不到时降级为 "-"，避免日志缺字段。
		userID, _ := GetUserIDFromContext(c)
		userIDStr := "-"
		if userID != 0 {
			userIDStr = strconv.FormatUint(uint64(userID), 10)
		}

		// 运维探测类请求（健康检查/指标/就绪/版本）频繁，使用 Debug 避免日志噪音
		logFunc := log.Infof
		if path == constants.HealthCheckPath || path == constants.MetricsPath || path == constants.ReadinessPath || path == constants.VersionPath {
			logFunc = log.Debugf
		}

		logFunc("[%s] traceID=%v | path=%s | clientIP=%s | userId=%s | status=%d | latency=%v | timestamp=%s | err=%s",
			method,
			traceID,
			path,
			clientIP,
			userIDStr,
			statusCode,
			latency,
			timestamp,
			c.Errors.ByType(gin.ErrorTypePrivate).String(),
		)

		// debug 级别下打印请求与响应明细（含 query + body，请求体已限制最大 4KB 防止日志膨胀）
		if debugEnabled {
			log.Debugf("[HTTP-REQ] traceID=%v | method=%s | path=%s | query=%s | body=%s",
				traceID, method, path, raw, string(reqBody))
			if bw, ok := c.Writer.(*bodyCaptureWriter); ok {
				log.Debugf("[HTTP-RESP] traceID=%v | method=%s | path=%s | status=%d | body=%s",
					traceID, method, path, statusCode, bw.body.String())
			}
		}
	}
}

func RecoveryMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				metrics.RecordPanic(config.Get().App.ServiceID)
				log.Errorf("Panic recovered: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{
					"code":    500,
					"message": "Internal Server Error",
				})
				c.Abort()
			}
		}()
		c.Next()
	}
}

// csrfHeader 是 CSRF 防护使用的请求头名称，前后端约定一致。
// 复用 constants.HeaderCSRFToken，避免头名拼写不一致。
const csrfHeader = constants.HeaderCSRFToken

// CORSMiddleware 跨域中间件。
// 允许的来源通过环境变量 CORS_ALLOW_ORIGINS 配置（逗号分隔，支持 "*" 通配）。
// 未配置时（默认）仅允许同源，不对外暴露跨域，避免 API 被任意站点调用。
// 重要：Bearer 鉴权不依赖 Cookie，因此不再设置 Access-Control-Allow-Credentials，
// 消除原先 "* + credentials" 这一违规且危险的配置。
func CORSMiddleware() gin.HandlerFunc {
	allowed := parseAllowedOrigins(os.Getenv(constants.EnvCORSAllowOrigins))
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" {
			if isOriginAllowed(origin, allowed) {
				c.Writer.Header().Set(constants.HeaderAccessControlAllowOrigin, origin)
				c.Writer.Header().Set("Vary", "Origin")
			}
			// 不在白名单：不设置 Allow-Origin，浏览器将拒绝跨域读取响应
		}

		c.Writer.Header().Set(constants.HeaderAccessControlAllowHeaders,
			"Origin, Content-Type, "+constants.HeaderAuthorization+", X-Requested-With, Accept, Cache-Control, Content-Length, Accept-Encoding, "+csrfHeader)
		c.Writer.Header().Set(constants.HeaderAccessControlAllowMethods, "GET, POST, PUT, DELETE, PATCH, OPTIONS")
		c.Writer.Header().Set(constants.HeaderAccessControlExposeHeaders,
			"Content-Length, "+constants.HeaderAccessControlAllowOrigin+", "+constants.HeaderAccessControlAllowHeaders)
		c.Writer.Header().Set(constants.HeaderAccessControlMaxAge, constants.CORSMaxAge)

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

// CSRFMiddleware 防止跨站请求伪造（CSRF）。
// 机制（双重提交变体，与 Bearer 鉴权正交）：
//   - 登录/注册时服务端在 JWT 中写入 csrf 声明，并在登录响应下发该 token（见 SuccessLogin）；
//   - 前端对“已登录用户的状态变更请求”（非安全方法且带 Authorization）通过 X-CSRF-Token 头回传；
//   - 本中间件自行解析 JWT 取出 csrf 声明，与请求头比对，不一致/缺失则拒绝（403）。
//
// 对未带 Authorization 的公开写接口（注册、发验证码等）不强制校验，
// 其防护由邮箱验证码、频率限制等承担。
// 本中间件会在 AuthMiddleware 之前执行并自行解析一次 JWT；解析失败仅放行，
// 交由后续 AuthMiddleware(requireAuth=true) 返回 401，避免重复拒绝。
func CSRFMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		method := c.Request.Method
		// 安全方法（只读）不受 CSRF 约束
		if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
			c.Next()
			return
		}

		authHeader := c.GetHeader(constants.HeaderAuthorization)
		if authHeader == "" {
			// 未登录：公开写接口，不强制 CSRF 校验
			c.Next()
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 {
			c.Next()
			return
		}

		claims, err := parseJWTClaims(parts[1])
		if err != nil {
			// JWT 无效：放行，交由 AuthMiddleware(requireAuth=true) 返回 401
			c.Next()
			return
		}

		expected, _ := claims[constants.JWTClaimCSRF].(string)
		actual := c.GetHeader(csrfHeader)
		if expected == "" || actual == "" || expected != actual {
			c.JSON(http.StatusForbidden, gin.H{
				"code":    constants.ErrCodeForbidden,
				"message": "CSRF token 校验失败",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

func parseAllowedOrigins(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func isOriginAllowed(origin string, allowed []string) bool {
	for _, a := range allowed {
		if a == "*" || a == origin {
			return true
		}
	}
	return false
}

// AuthMiddleware 统一鉴权与用户上下文注入中间件。
//
// requireAuth 控制拦截行为：
//   - requireAuth=true（鉴权模式）：缺失 Authorization、格式非法或签名/过期校验失败
//     均直接返回 401 并中断，校验成功后将 user_id/username/role 注入 gin.Context。
//   - requireAuth=false（非拦截模式）：仅当携带合法 JWT 时提取并注入用户信息，
//     缺失或校验失败时跳过提取、请求仍放行，用于网关访问日志写 userId 等场景。
//
// 两种模式共用同一套 JWT 解析逻辑，避免重复实现与语义分裂。
func AuthMiddleware(requireAuth bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader(constants.HeaderAuthorization)
		if authHeader == "" {
			if requireAuth {
				c.JSON(http.StatusUnauthorized, gin.H{
					"code":    401,
					"message": "Authorization header required",
				})
				c.Abort()
				return
			}
			c.Next()
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != strings.TrimSpace(constants.JWTAuthScheme) {
			if requireAuth {
				c.JSON(http.StatusUnauthorized, gin.H{
					"code":    401,
					"message": "Invalid authorization header format",
				})
				c.Abort()
				return
			}
			c.Next()
			return
		}

		claims, err := parseJWTClaims(parts[1])
		if err != nil {
			if requireAuth {
				c.JSON(http.StatusUnauthorized, gin.H{
					"code":    401,
					"message": "Invalid or expired token",
				})
				c.Abort()
				return
			}
			c.Next()
			return
		}

		if uid, ok := claims[constants.JWTClaimUserID]; ok {
			c.Set(UserIDContextKey, uid)
		}
		if uname, ok := claims[constants.JWTClaimUsername]; ok {
			c.Set(UsernameContextKey, uname)
		}
		if role, ok := claims[constants.JWTClaimRole]; ok {
			c.Set(RoleContextKey, role)
		}
		c.Next()
	}
}

func parseJWTClaims(tokenString string) (jwt.MapClaims, error) {
	token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return []byte(jwtSecret), nil
	})
	if err != nil || !token.Valid {
		return nil, err
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, jwt.ErrTokenInvalidClaims
	}
	return claims, nil
}

// AdminOnlyMiddleware 仅允许管理员访问。
// 依赖前置的 AuthMiddleware(requireAuth=true) 已将 role 注入 gin.Context（见 RoleContextKey）。
// JWT 中数字声明默认解析为 float64，这里做类型兼容。
func AdminOnlyMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, exists := c.Get(RoleContextKey)
		if !exists {
			c.JSON(http.StatusForbidden, gin.H{"code": constants.ErrCodeForbidden, "message": "需要管理员权限"})
			c.Abort()
			return
		}

		var role uint8
		switch v := raw.(type) {
		case float64:
			role = uint8(v)
		case uint8:
			role = v
		case int:
			role = uint8(v)
		case int64:
			role = uint8(v)
		default:
			c.JSON(http.StatusForbidden, gin.H{"code": constants.ErrCodeForbidden, "message": "无效的角色信息"})
			c.Abort()
			return
		}

		if role != constants.RoleAdmin {
			c.JSON(http.StatusForbidden, gin.H{"code": constants.ErrCodeForbidden, "message": "需要管理员权限"})
			c.Abort()
			return
		}
		c.Next()
	}
}

func ContextMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		userID, exists := c.Get(UserIDContextKey)
		if exists {
			switch v := userID.(type) {
			case uint:
				ctx = context.WithValue(ctx, UserIDKey, v)
			case float64:
				ctx = context.WithValue(ctx, UserIDKey, uint(v))
			}
		}
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

func TimeoutMiddleware(timeout time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// TimeoutMiddlewareDefault 与 TimeoutMiddleware 等价，但超时值取自
// goconfig.Get().Server.HTTP.DefaultTimeoutSec，每次请求读取，
// 因此支持通过 configcenter 热更而无需重启进程。
func TimeoutMiddlewareDefault() gin.HandlerFunc {
	return func(c *gin.Context) {
		timeout := time.Duration(config.Get().Server.HTTP.DefaultTimeoutSec) * time.Second
		ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

func ValidateRequestMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodPost || c.Request.Method == http.MethodPut || c.Request.Method == http.MethodPatch {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, constants.MaxRequestBody)
		}
		c.Next()
	}
}

func TraceMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		traceID := c.GetHeader(constants.HeaderXTraceID)
		if traceID == "" {
			traceID = generateTraceID()
		}
		c.Set(constants.HeaderXTraceID, traceID)
		// 在业务逻辑执行前写入响应头。
		// 注意：若下游反向代理响应中也有同名头，网管通过 ModifyResponse 剥离下游的避免重复。
		c.Header(constants.HeaderXTraceID, traceID)
		// 同时注入 context.Context，方便 MySQL/Redis/gRPC 下游组件通过 GetTraceIDFromContext 获取
		ctx := context.WithValue(c.Request.Context(), traceIDKey, traceID)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
