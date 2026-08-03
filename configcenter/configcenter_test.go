package configcenter

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mysunshines/gocommon/config"
)

// fakeConsulKV 启动一个最小可用的 Consul KV 模拟服务，支持：
//   - GET  /v1/kv/{key}          读取值（支持 ?index= / ?wait= blocking query）
//   - PUT  /v1/kv/{key}          写入值（任意 body）
//
// 写入后 ModifyIndex 自增，GET 带 index 且 index>=当前时阻塞直到下次变更或 wait 超时。
func fakeConsulKV(t *testing.T) (*httptest.Server, func()) {
	t.Helper()
	var mu sync.Mutex
	store := map[string]string{} // key -> base64 value
	var index uint64 = 1

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/kv/", func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/v1/kv/")
		mu.Lock()
		defer mu.Unlock()

		switch r.Method {
		case http.MethodPut:
			buf := make([]byte, r.ContentLength)
			r.Body.Read(buf)
			store[key] = base64.StdEncoding.EncodeToString(buf)
			index++
			w.Header().Set("X-Consul-Index", fmt.Sprintf("%d", index))
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("true"))
		case http.MethodGet:
			idx := parseConsulIndex(r.URL.Query().Get("index"))
			// 若客户端已知 index >= 当前，则阻塞直到变更或 200ms 超时（简化为不阻塞，直接按 index 差异返回）。
			if idx > 0 && idx >= index {
				// 模拟 blocking query：返回 304 风格的相同 index（无变更）。
				w.Header().Set("X-Consul-Index", fmt.Sprintf("%d", index))
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode([]struct {
					Value       string `json:"Value"`
					ModifyIndex uint64 `json:"ModifyIndex"`
				}{})
				return
			}
			v, ok := store[key]
			w.Header().Set("X-Consul-Index", fmt.Sprintf("%d", index))
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			json.NewEncoder(w).Encode([]struct {
				Value       string `json:"Value"`
				ModifyIndex uint64 `json:"ModifyIndex"`
			}{
				{Value: v, ModifyIndex: index},
			})
		}
	})

	srv := httptest.NewServer(mux)
	return srv, srv.Close
}

func extractHost(srv *httptest.Server) string {
	return strings.TrimPrefix(srv.URL, "http://")
}

func TestKey(t *testing.T) {
	got := Key("article-service", "production")
	want := "config/article-service/production"
	if got != want {
		t.Fatalf("Key() = %q, want %q", got, want)
	}
}

func TestUnmarshalAutoYAML(t *testing.T) {
	var hc HotConfig
	raw := []byte("log_level: debug\nrate_limit:\n  qps: 50\n  burst: 100\njwt_expire_time: 3600\n")
	if err := unmarshalAuto(raw, &hc); err != nil {
		t.Fatalf("unmarshalAuto yaml: %v", err)
	}
	if hc.LogLevel != "debug" || hc.RateLimit.QPS != 50 || hc.RateLimit.Burst != 100 || hc.JWTExpireTime != 3600 {
		t.Fatalf("unexpected unmarshalled: %+v", hc)
	}
}

func TestUnmarshalAutoJSON(t *testing.T) {
	var hc HotConfig
	raw := []byte(`{"log_level":"warn","rate_limit":{"qps":5,"burst":10},"jwt_expire_time":7200}`)
	if err := unmarshalAuto(raw, &hc); err != nil {
		t.Fatalf("unmarshalAuto json: %v", err)
	}
	if hc.LogLevel != "warn" || hc.JWTExpireTime != 7200 {
		t.Fatalf("unexpected unmarshalled: %+v", hc)
	}
}

func TestClientPutAndLoad(t *testing.T) {
	srv, closeFn := fakeConsulKV(t)
	defer closeFn()

	c := New(extractHost(srv))
	defer c.Stop()
	key := Key("svc-a", "test")
	payload := []byte("log_level: info\nrate_limit:\n  qps: 200\n  burst: 400\n")
	if err := c.Put(key, payload); err != nil {
		t.Fatalf("Put: %v", err)
	}

	var hc HotConfig
	if err := c.Load(key, &hc); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if hc.RateLimit.QPS != 200 || hc.RateLimit.Burst != 400 {
		t.Fatalf("Load mismatch: %+v", hc)
	}
}

func TestClientLoadNotFound(t *testing.T) {
	srv, closeFn := fakeConsulKV(t)
	defer closeFn()

	c := New(extractHost(srv))
	defer c.Stop()
	var hc HotConfig
	err := c.Load(Key("missing", "test"), &hc)
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestServiceConfigInitAndLoad(t *testing.T) {
	srv, closeFn := fakeConsulKV(t)
	defer closeFn()

	sc := Init(extractHost(srv), "svc-b", "test")
	// 初始快照应为默认值
	if sc.Get().LogLevel != "info" {
		t.Fatalf("default log level should be info, got %q", sc.Get().LogLevel)
	}

	// 写入后 Load 应更新
	_ = sc.KV().Put(Key("svc-b", "test"), []byte("log_level: debug\nrate_limit:\n  qps: 99\n  burst: 199\njwt_expire_time: 1234\n"))
	if err := sc.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if sc.Get().LogLevel != "debug" || sc.Get().RateLimit.QPS != 99 || sc.Get().JWTExpireTime != 1234 {
		t.Fatalf("after load mismatch: %+v", sc.Get())
	}
}

// TestServiceConfigApply 验证热更值能回写到全局 config.Config。
func TestServiceConfigApply(t *testing.T) {
	srv, closeFn := fakeConsulKV(t)
	defer closeFn()

	// 先初始化全局 config，便于断言热更值回写。
	tmp := writeTempYAML(t, "log_level: info\nrate_limit:\n  qps: 10\n  burst: 20\njwt:\n  expire_time: 100\n")
	if _, err := config.Load(tmp); err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	sc := Init(extractHost(srv), "svc-c", "test")
	hc := &HotConfig{LogLevel: "debug", RateLimit: config.RateLimitConfig{Enabled: true, QPS: 77, Burst: 88}, JWTExpireTime: 555}
	sc.apply(hc)

	if got := sc.Get().LogLevel; got != "debug" {
		t.Fatalf("snapshot not updated: %q", got)
	}
	g := config.Get()
	if g.RateLimit.QPS != 77 || g.JWT.ExpireTime != 555 {
		t.Fatalf("global config not applied: qps=%d jwt=%d", g.RateLimit.QPS, g.JWT.ExpireTime)
	}
}

// TestWatchDetectsChange 验证 Watch 能在 KV 变更后触发 onUpdate 并刷新快照。
func TestWatchDetectsChange(t *testing.T) {
	srv, closeFn := fakeConsulKV(t)
	defer closeFn()

	sc := Init(extractHost(srv), "svc-d", "test")
	defer sc.Stop()
	key := Key("svc-d", "test")
	// 先写入初始值（模拟后台已配置），再 Load 拿到它（使 currentIndex 非 0）。
	_ = sc.KV().Put(key, []byte("log_level: info\nrate_limit:\n  qps: 1\n  burst: 2\n"))
	if err := sc.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if sc.Get().RateLimit.QPS != 1 {
		t.Fatalf("initial load not applied, got qps=%d", sc.Get().RateLimit.QPS)
	}

	var hc HotConfig
	updated := make(chan struct{}, 1)
	go func() {
		_ = sc.client.Watch(key, &hc, func() {
			sc.current.Store(&hc)
			select {
			case updated <- struct{}{}:
			default:
			}
		})
	}()

	_ = sc.KV().Put(key, []byte("log_level: warn\nrate_limit:\n  qps: 333\n  burst: 444\n"))

	select {
	case <-updated:
		if sc.Get().RateLimit.QPS != 333 {
			t.Fatalf("watch did not apply new qps, got %d", sc.Get().RateLimit.QPS)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("watch did not detect change within timeout")
	}
}

// writeTempYAML 写一个临时 YAML 文件供 config.Load 初始化全局配置，测试结束后自动清理。
func writeTempYAML(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp yaml: %v", err)
	}
	return p
}
