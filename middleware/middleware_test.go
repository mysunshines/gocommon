package middleware

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mysunshines/gocommon/config"
	"github.com/mysunshines/gocommon/constants"
	"github.com/mysunshines/gocommon/metrics"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/time/rate"
)

func TestNewRateLimiter(t *testing.T) {
	rl := NewRateLimiter(100, 200)
	if !rl.Allow("/api/v1/user/register", "k") {
		t.Fatal("first Allow should be true")
	}
	l1 := rl.GetLimiter("same")
	l2 := rl.GetLimiter("same")
	if l1 != l2 {
		t.Fatal("same key should return the same limiter")
	}
}

func TestUpdateRateLimiter(t *testing.T) {
	// 直接构造并赋值包级限流器（绕过 sync.Once，避免受其他测试的初始化顺序影响），
	// 验证动态更新阈值后已存在与新建限流器均反映新值。
	rateLimiter = NewRateLimiter(100, 200)
	// 先消费一个已存在 key 的 limiter
	_ = rateLimiter.Allow("/api/v1/user/login", "existing")

	UpdateRateLimiter(&config.RateLimitConfig{Enabled: true, QPS: 50, Burst: 80})
	if rateLimiter.rps != rate.Limit(50) || rateLimiter.burst != 80 {
		t.Fatalf("rps/burst not updated: rps=%v burst=%d", rateLimiter.rps, rateLimiter.burst)
	}
	// 已存在的 limiter 应被刷新
	if got := rateLimiter.limiters["existing"].Burst(); got != 80 {
		t.Fatalf("existing limiter burst not updated, got %d", got)
	}
	// 后续新建的 limiter 应使用新默认值
	newL := rateLimiter.GetLimiter("fresh")
	if newL.Limit() != rate.Limit(50) || newL.Burst() != 80 {
		t.Fatalf("fresh limiter not using new defaults: limit=%v burst=%d", newL.Limit(), newL.Burst())
	}

	// 传入 Enabled=false 的更新应被忽略，不降级关闭限流
	UpdateRateLimiter(&config.RateLimitConfig{Enabled: false, QPS: 1, Burst: 1})
	if rateLimiter.rps != rate.Limit(50) || rateLimiter.burst != 80 {
		t.Fatalf("disabled update should be ignored: rps=%v burst=%d", rateLimiter.rps, rateLimiter.burst)
	}
}

func TestRateLimitMiddlewareDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	InitRateLimiter(&config.RateLimitConfig{Enabled: false})

	r := gin.New()
	r.Use(RateLimitMiddleware())
	r.GET("/", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "1.2.3.4:1234"
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("disabled limiter should pass through, code=%d", w.Code)
	}
}

func TestContextHelpers(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set(UserIDContextKey, float64(42))
	id, err := GetUserIDFromContext(c)
	if err != nil || id != 42 {
		t.Fatalf("id=%d err=%v", id, err)
	}

	c.Set(UsernameContextKey, "alice")
	if GetUsernameFromContext(c) != "alice" {
		t.Fatal("username mismatch")
	}

	c2, _ := gin.CreateTestContext(httptest.NewRecorder())
	if _, err := GetUserIDFromContext(c2); err != nil {
		t.Fatalf("missing user should return nil error, got %v", err)
	}
}

func TestAuthMiddlewareRequireAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	secret := "test-secret"
	InitJWT(secret)

	r := gin.New()
	r.Use(AuthMiddleware(true))
	r.GET("/", func(c *gin.Context) {
		uid, _ := GetUserIDFromContext(c)
		c.String(http.StatusOK, "%d", uid)
	})

	claims := jwt.MapClaims{"user_id": float64(7), "username": "bob", "role": float64(1)}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+signed)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("valid token should pass, code=%d", w.Code)
	}
	if w.Body.String() != "7" {
		t.Fatalf("body=%s", w.Body.String())
	}
}

func TestAuthMiddlewareRequireAuthMissingHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	InitJWT("test-secret")

	r := gin.New()
	r.Use(AuthMiddleware(true))
	r.GET("/", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("missing header should be rejected, code=%d", w.Code)
	}
}

func TestAdminOnlyMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	InitJWT("test-secret")

	r := gin.New()
	r.Use(AuthMiddleware(true), AdminOnlyMiddleware())
	r.GET("/", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	makeTok := func(role float64) string {
		claims := jwt.MapClaims{"user_id": float64(1), "role": role}
		tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		s, _ := tok.SignedString([]byte("test-secret"))
		return s
	}

	// 管理员
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+makeTok(float64(constants.RoleAdmin)))
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("admin should pass, code=%d", w.Code)
	}

	// 非管理员（RoleNormal = 1）
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.Header.Set("Authorization", "Bearer "+makeTok(float64(constants.RoleNormal)))
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusForbidden {
		t.Fatalf("non-admin should be forbidden, code=%d", w2.Code)
	}
}

func TestCORSMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// 配置允许的来源为 *，模拟开放跨域场景
	t.Setenv("CORS_ALLOW_ORIGINS", "*")

	r := gin.New()
	r.Use(CORSMiddleware())
	r.GET("/", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	// 带 Origin 头的请求，CORS 应回显该 origin 并设置 Vary: Origin
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "http://example.com")
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://example.com" {
		t.Fatalf("CORS origin not echoed, got %q", got)
	}
	if w.Header().Get("Vary") != "Origin" {
		t.Fatal("CORS Vary header missing")
	}
	if w.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS should return 204, got %d", w.Code)
	}
}

func TestCORSMiddlewareNoOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// 无 Origin 头的同源请求不应设置 Allow-Origin
	r := gin.New()
	r.Use(CORSMiddleware())
	r.GET("/", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(w, req)

	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("no Origin should not set Allow-Origin, got %q", w.Header().Get("Access-Control-Allow-Origin"))
	}
	if w.Code != http.StatusOK {
		t.Fatalf("GET should pass through, got %d", w.Code)
	}
}

func TestCORSMiddlewareDisallowedOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("CORS_ALLOW_ORIGINS", "https://trusted.com")

	r := gin.New()
	r.Use(CORSMiddleware())
	r.GET("/", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://evil.com")
	r.ServeHTTP(w, req)

	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("disallowed origin should not set Allow-Origin, got %q", w.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestRecoveryMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// RecoveryMiddleware 内部会读取 config.Get().App.ServiceID，需先初始化全局配置
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("app:\n  name: test-svc\n  service_id: test-svc\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(cfgPath); err != nil {
		t.Fatal(err)
	}
	metrics.Init("test-service")

	r := gin.New()
	r.Use(RecoveryMiddleware())
	r.GET("/", func(c *gin.Context) { panic("boom") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("recovered panic should return 500, got %d", w.Code)
	}
}

func TestTimeoutMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(TimeoutMiddleware(50 * time.Millisecond))
	r.GET("/", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("timeout middleware should pass through, code=%d", w.Code)
	}
}

func ExampleNewRateLimiter() {
	rl := NewRateLimiter(10, 20)
	_ = rl.Allow("/api/v1/upload", "client-ip")
}

// ExampleUpdateRateLimiter 演示配置中心热更限流阈值时即时刷新限流器。
func ExampleUpdateRateLimiter() {
	InitRateLimiter(&config.RateLimitConfig{Enabled: true, QPS: 100, Burst: 200})
	// 线上通过配置后台把限流阈值下调，无需重启即时生效。
	UpdateRateLimiter(&config.RateLimitConfig{Enabled: true, QPS: 50, Burst: 80})
}
