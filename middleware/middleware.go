package middleware

import (
	"context"
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
	"github.com/mysunshines/gocommon/util"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/time/rate"
)

type RateLimiter struct {
	// limiters 按 key（通常为客户端 IP）缓存各自的令牌桶限流器。
	// 每个 key 独立限流，首次访问时惰性创建（见 GetLimiter）。
	limiters map[string]*rate.Limiter
	// mu 保护对 limiters map 的并发读写（新建/查询限流器时会加锁）。
	mu sync.RWMutex
	// rps 新建限流器时使用的速率（每秒允许的平均请求数，rate.Limit 即 float64）。
	rps rate.Limit
	// burst 新建限流器时允许的突发容量（令牌桶可积攒的最大令牌数，即瞬时最大放行请求数）。
	burst int
}

func NewRateLimiter(rps int, burst int) *RateLimiter {
	return &RateLimiter{
		limiters: make(map[string]*rate.Limiter),
		rps:      rate.Limit(rps),
		burst:    burst,
	}
}

func (rl *RateLimiter) GetLimiter(key string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if limiter, ok := rl.limiters[key]; ok {
		return limiter
	}
	limiter := rate.NewLimiter(rl.rps, rl.burst)
	rl.limiters[key] = limiter
	return limiter
}

func (rl *RateLimiter) Allow(key string) bool {
	limiter := rl.GetLimiter(key)
	return limiter.Allow()
}

// SetLimit 动态更新限流阈值：既更新后续新建限流器的默认值，也即时刷新
// 所有已存在的限流器（已缓存的令牌桶），实现阈值热更无需重启。
func (rl *RateLimiter) SetLimit(rps int, burst int) {
	rl.mu.Lock()
	rl.rps = rate.Limit(rps)
	rl.burst = burst
	for _, limiter := range rl.limiters {
		limiter.SetLimit(rl.rps)
		limiter.SetBurst(rl.burst)
	}
	rl.mu.Unlock()
}

var (
	rateLimiter *RateLimiter
	limiterOnce sync.Once

	jwtSecret string
	jwtOnce   sync.Once
)

func InitRateLimiter(cfg *config.RateLimitConfig) {
	limiterOnce.Do(func() {
		if cfg.Enabled {
			rateLimiter = NewRateLimiter(cfg.QPS, cfg.Burst)
		}
	})
}

// UpdateRateLimiter 动态调整已初始化限流器的阈值（供配置中心热更即时生效）。
// 仅在 InitRateLimiter 已初始化限流器后有效；若尚未初始化或配置关闭，则忽略。
func UpdateRateLimiter(cfg *config.RateLimitConfig) {
	if rateLimiter == nil {
		return
	}
	if !cfg.Enabled {
		return
	}
	rateLimiter.SetLimit(cfg.QPS, cfg.Burst)
}

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

		key := c.ClientIP()
		if !rateLimiter.Allow(key) {
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

		// 从 gin.Context 取 userId（由前置的 JWTValidMiddleware 注入），
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
const csrfHeader = "X-CSRF-Token"

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
				c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
				c.Writer.Header().Set("Vary", "Origin")
			}
			// 不在白名单：不设置 Allow-Origin，浏览器将拒绝跨域读取响应
		}

		c.Writer.Header().Set("Access-Control-Allow-Headers",
			"Origin, Content-Type, Authorization, X-Requested-With, Accept, Cache-Control, Content-Length, Accept-Encoding, "+csrfHeader)
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, PATCH, OPTIONS")
		c.Writer.Header().Set("Access-Control-Expose-Headers", "Content-Length, Access-Control-Allow-Origin, Access-Control-Allow-Headers")
		c.Writer.Header().Set("Access-Control-Max-Age", constants.CORSMaxAge)

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
// 本中间件会在 JWTValidMiddleware 之前执行并自行解析一次 JWT；解析失败仅放行，
// 交由后续 JWTValidMiddleware 返回 401，避免重复拒绝。
func CSRFMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		method := c.Request.Method
		// 安全方法（只读）不受 CSRF 约束
		if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
			c.Next()
			return
		}

		authHeader := c.GetHeader("Authorization")
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
			// JWT 无效：放行，交由 JWTValidMiddleware 返回 401
			c.Next()
			return
		}

		expected, _ := claims["csrf"].(string)
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

func JWTValidMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "Authorization header required",
			})
			c.Abort()
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != strings.TrimSpace(constants.JWTAuthScheme) {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "Invalid authorization header format",
			})
			c.Abort()
			return
		}

		tokenString := parts[1]

		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return []byte(jwtSecret), nil
		})

		if err != nil || !token.Valid {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "Invalid or expired token",
			})
			c.Abort()
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "Invalid token claims",
			})
			c.Abort()
			return
		}

		c.Set(UserIDContextKey, claims["user_id"])
		c.Set(UsernameContextKey, claims["username"])
		if role, ok := claims["role"]; ok {
			c.Set(RoleContextKey, role)
		}
		c.Next()
	}
}

// UserContextMiddleware 解析并提取 JWT 中的用户身份信息（user_id/username/role）
// 注入 gin.Context，供后续 LoggingMiddleware 等记录，但不拦截请求：token 缺失或
// 校验失败时仅跳过提取，请求仍放行。鉴权职责仍由下游服务 / 显式 JWTValidMiddleware 承担。
// 典型用途：在网关层统一把 userId 写入访问日志，而不强制所有路由鉴权。
func UserContextMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.Next()
			return
		}
		tokenStr, ok := strings.CutPrefix(authHeader, constants.JWTAuthScheme)
		if !ok {
			c.Next()
			return
		}
		claims, err := parseJWTClaims(tokenStr)
		if err != nil {
			c.Next()
			return
		}
		if uid, ok := claims["user_id"]; ok {
			c.Set(UserIDContextKey, uid)
		}
		if uname, ok := claims["username"]; ok {
			c.Set(UsernameContextKey, uname)
		}
		if role, ok := claims["role"]; ok {
			c.Set(RoleContextKey, role)
		}
		c.Next()
	}
}

// AdminOnlyMiddleware 仅允许管理员访问。
// 依赖前置的 JWTValidMiddleware 已将 role 注入 gin.Context（见 RoleContextKey）。
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
