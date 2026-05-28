package middleware

import (
	"context"
	"net/http"
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
	limiters map[string]*rate.Limiter
	mu       sync.RWMutex
	rps      rate.Limit
	burst    int
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

		if raw != "" {
			path = path + "?" + raw
		}

		log.Infof("[%s] %s %s %d %v %s",
			method,
			path,
			clientIP,
			statusCode,
			latency,
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

func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Authorization, X-Requested-With, Accept, Cache-Control, Content-Length, Accept-Encoding, X-CSRF-Token")
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
		c.Header(constants.HeaderXTraceID, traceID)
		c.Next()
	}
}

func generateTraceID() string {
	return time.Now().Format(constants.DateTimeCompact) + "-" + util.GenerateRandomString(8)
}
