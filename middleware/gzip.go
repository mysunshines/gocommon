package middleware

import (
	"compress/gzip"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// gzipMinSize 小于该体积的响应不压缩：压缩收益抵不上 CPU 与首字节延迟开销。
// 仅在响应已声明 Content-Length 时生效（分块响应的长度事先未知，仍会压缩）。
const gzipMinSize = 1024

// gzipCompressible 判断内容类型是否值得压缩。
// 图片、字体、音视频等本身已是压缩格式，再 gzip 只会白白消耗 CPU。
func gzipCompressible(contentType string) bool {
	ct := strings.ToLower(contentType)
	if ct == "" {
		return false
	}
	targets := []string{
		"text/",
		"application/json",
		"application/javascript",
		"application/x-javascript",
		"application/xml",
		"image/svg+xml",
	}
	for _, t := range targets {
		if strings.HasPrefix(ct, t) || strings.Contains(ct, t) {
			return true
		}
	}
	return false
}

// gzipWriter 在「首次写入」时才决定是否压缩 —— 因为此时业务代码已经写好
// Content-Type / Content-Length，才能据此判断要不要压缩；同时也保证压缩相关的
// 响应头在 net/http 隐式发送 WriteHeader 之前被设置。
//
// gin 的 c.JSON/c.Data 只会先设置状态码，真正的 WriteHeader 由底层 Write 触发，
// 因此这里改写响应头是安全的。
type gzipWriter struct {
	gin.ResponseWriter
	gz       *gzip.Writer
	decided  bool
	compress bool
}

func (g *gzipWriter) decide() {
	if g.decided {
		return
	}
	g.decided = true

	h := g.Header()
	// 上游已压缩过（如预 gzip 的静态资源）则不要再压一层
	if h.Get("Content-Encoding") != "" {
		return
	}
	if !gzipCompressible(h.Get("Content-Type")) {
		return
	}
	// 已知长度且过小时跳过（未知长度的分块响应仍压缩，多为动态 JSON）
	if cl := h.Get("Content-Length"); cl != "" {
		if n, err := strconv.Atoi(cl); err == nil && n < gzipMinSize {
			return
		}
	}

	// 先创建压缩器，成功后再改响应头；失败则降级为不压缩
	gz, err := gzip.NewWriterLevel(g.ResponseWriter, gzip.BestSpeed)
	if err != nil {
		return
	}

	h.Del("Content-Length") // 压缩后长度会变，交给底层分块/计算
	h.Set("Content-Encoding", "gzip")
	h.Add("Vary", "Accept-Encoding")
	g.compress = true
	g.gz = gz
}

func (g *gzipWriter) Write(b []byte) (int, error) {
	g.decide()
	if g.compress {
		return g.gz.Write(b)
	}
	return g.ResponseWriter.Write(b)
}

func (g *gzipWriter) WriteString(s string) (int, error) { return g.Write([]byte(s)) }

func (g *gzipWriter) Flush() {
	if g.compress && g.gz != nil {
		_ = g.gz.Flush()
	}
	g.ResponseWriter.Flush()
}

// finish 收尾，把 gzip 缓冲区刷到底层 writer。由中间件 defer 调用。
func (g *gzipWriter) finish() {
	if g.compress && g.gz != nil {
		_ = g.gz.Close()
		g.gz = nil
	}
}

// GzipMiddleware 对文本类响应做 gzip 压缩（JSON / HTML / CSS / JS / SVG）。
//
// 对大体积 JSON 收益尤其明显：实测数 MB 的词库 JSON 压缩后约为原来的 27%
// （5.3MB → 约 1.4MB）。以下情况会自动跳过压缩：客户端未声明 Accept-Encoding: gzip、
// WebSocket 升级请求、响应已被压缩过、内容类型不可压缩、以及已知长度小于 1KB 的响应。
//
// 用法（注册顺序建议在其他写入响应体的中间件之后、路由之前）：
//
//	r.Use(middleware.GzipMiddleware())
func GzipMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !strings.Contains(c.GetHeader("Accept-Encoding"), "gzip") || c.GetHeader("Upgrade") != "" {
			return
		}
		gw := &gzipWriter{ResponseWriter: c.Writer}
		c.Writer = gw
		defer gw.finish()
		c.Next()
	}
}
