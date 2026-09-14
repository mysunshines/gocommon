package middleware

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// newGzipTestRouter 搭建一个挂了 GzipMiddleware 的测试路由
func newGzipTestRouter(payload string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(GzipMiddleware())

	// 大体积 JSON：应当被压缩
	r.GET("/data", func(c *gin.Context) {
		c.Data(http.StatusOK, "application/json; charset=utf-8", []byte(payload))
	})
	// 显式声明了 Content-Length 的极小响应：应当跳过压缩
	r.GET("/tiny", func(c *gin.Context) {
		c.Header("Content-Length", "2")
		c.String(http.StatusOK, "hi")
	})
	return r
}

func doGet(t *testing.T, r *gin.Engine, path, acceptEncoding string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if acceptEncoding != "" {
		req.Header.Set("Accept-Encoding", acceptEncoding)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestGzipMiddlewareCompressesJSON(t *testing.T) {
	payload := strings.Repeat(`{"word":"abandon","translation":"放弃"},`, 300)
	r := newGzipTestRouter(payload)

	w := doGet(t, r, "/data", "gzip")

	if got := w.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("期望 Content-Encoding: gzip, 实际 %q", got)
	}
	if w.Body.Len() >= len(payload) {
		t.Errorf("压缩后应小于原文: %d >= %d", w.Body.Len(), len(payload))
	}

	// 解压后必须与原文完全一致
	zr, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatalf("响应不是合法 gzip 数据: %v", err)
	}
	defer zr.Close()
	decoded, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("解压失败: %v", err)
	}
	if string(decoded) != payload {
		t.Error("解压后内容与原文不一致")
	}
}

func TestGzipMiddlewareSkipsWhenNotAccepted(t *testing.T) {
	payload := strings.Repeat(`{"a":1},`, 300)
	r := newGzipTestRouter(payload)

	w := doGet(t, r, "/data", "")

	if got := w.Header().Get("Content-Encoding"); got == "gzip" {
		t.Error("客户端未声明 gzip 时不应压缩")
	}
	if w.Body.String() != payload {
		t.Error("未压缩时响应体应原样返回")
	}
}

func TestGzipMiddlewareSkipsTinyResponse(t *testing.T) {
	r := newGzipTestRouter("")

	w := doGet(t, r, "/tiny", "gzip")

	if got := w.Header().Get("Content-Encoding"); got == "gzip" {
		t.Error("已知长度小于阈值的响应不应压缩")
	}
	if w.Body.String() != "hi" {
		t.Errorf("期望响应体 hi, 实际 %q", w.Body.String())
	}
}
