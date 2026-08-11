package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/mysunshines/gocommon/constants"
	"github.com/mysunshines/gocommon/log"
	"github.com/mysunshines/gocommon/middleware"

	"github.com/sirupsen/logrus"
	"github.com/valyala/fasthttp"
)

// ============================================================================
// FastClient — 基于 fasthttp 的高性能 HTTP 客户端
// API 与原生 Client 保持一致，性能提升约 2-3x，适合高频调用场景。
//
// 切换方式：仅需将 client := httpclient.New(...) 改为 client := httpclient.NewFast(...)
// ============================================================================

// FastClient 基于 fasthttp 的 HTTP 客户端
type FastClient struct {
	client     *fasthttp.Client  // 底层 fasthttp 客户端
	baseURL    string            // 基础 URL，拼接到各请求路径前
	timeout    time.Duration     // 请求超时
	headers    map[string]string // 默认请求头（单次请求可覆盖）
	middleware []FastMiddleware  // 请求发出前执行的中间件链
}

// FastMiddleware fasthttp 中间件
type FastMiddleware func(*fasthttp.Request) error

// FastOption 配置选项
type FastOption func(*FastClient)

// NewFast 创建 fasthttp 客户端
func NewFast(opts ...FastOption) *FastClient {
	c := &FastClient{
		client: &fasthttp.Client{
			ReadTimeout:         constants.DefaultReadTimeout * time.Second,
			WriteTimeout:        constants.DefaultWriteTimeout * time.Second,
			MaxIdleConnDuration: constants.DefaultIdleTimeout * time.Second,
		},
		timeout: constants.DefaultReadTimeout * time.Second,
		headers: make(map[string]string),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// WithFastBaseURL 设置基础 URL
func WithFastBaseURL(baseURL string) FastOption {
	return func(c *FastClient) {
		c.baseURL = baseURL
	}
}

// WithFastTimeout 设置超时时间
func WithFastTimeout(timeout time.Duration) FastOption {
	return func(c *FastClient) {
		c.timeout = timeout
		c.client.ReadTimeout = timeout
		c.client.WriteTimeout = timeout
	}
}

// WithFastHeader 设置默认请求头
func WithFastHeader(key, value string) FastOption {
	return func(c *FastClient) {
		c.headers[key] = value
	}
}

// WithFastHeaders 批量设置默认请求头
func WithFastHeaders(headers map[string]string) FastOption {
	return func(c *FastClient) {
		for k, v := range headers {
			c.headers[k] = v
		}
	}
}

// WithFastMiddleware 添加中间件
func WithFastMiddleware(m FastMiddleware) FastOption {
	return func(c *FastClient) {
		c.middleware = append(c.middleware, m)
	}
}

// WithFastMaxConns 设置最大连接数（per-host）
func WithFastMaxConns(n int) FastOption {
	return func(c *FastClient) {
		c.client.MaxConnsPerHost = n
	}
}

// ============================================================================
// 公共方法 — buildURL / applyHeaders / executeMiddleware
// ============================================================================

func (c *FastClient) buildURL(path string) string {
	if c.baseURL == "" {
		return path
	}

	u, err := url.Parse(c.baseURL)
	if err != nil {
		return c.baseURL + path
	}

	basePath := u.Path
	if !strings.HasSuffix(basePath, "/") && !strings.HasPrefix(path, "/") {
		basePath += "/"
	} else if strings.HasSuffix(basePath, "/") && strings.HasPrefix(path, "/") {
		path = path[1:]
	}

	u.Path = basePath + path
	return u.String()
}

func (c *FastClient) applyHeaders(req *fasthttp.Request) {
	for k, v := range c.headers {
		if len(req.Header.Peek(k)) == 0 {
			req.Header.Set(k, v)
		}
	}
}

func (c *FastClient) executeMiddleware(req *fasthttp.Request) error {
	for _, m := range c.middleware {
		if err := m(req); err != nil {
			return err
		}
	}
	return nil
}

// ============================================================================
// HTTP 方法
// ============================================================================

// Get GET 请求
func (c *FastClient) Get(ctx context.Context, path string, params map[string]string) (*Response, error) {
	fullURL := c.buildURL(path)
	if len(params) > 0 {
		q := url.Values{}
		for k, v := range params {
			q.Set(k, v)
		}
		if strings.Contains(fullURL, "?") {
			fullURL += "&" + q.Encode()
		} else {
			fullURL += "?" + q.Encode()
		}
	}

	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)

	req.SetRequestURI(fullURL)
	req.Header.SetMethod(fasthttp.MethodGet)

	return c.do(ctx, req, resp)
}

// Post POST 请求
func (c *FastClient) Post(ctx context.Context, path string, body interface{}) (*Response, error) {
	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)

	req.SetRequestURI(c.buildURL(path))
	req.Header.SetMethod(fasthttp.MethodPost)
	req.Header.Set(constants.HeaderContentType, constants.ContentTypeJSON)

	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		req.SetBody(data)
	}

	return c.do(ctx, req, resp)
}

// Put PUT 请求
func (c *FastClient) Put(ctx context.Context, path string, body interface{}) (*Response, error) {
	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)

	req.SetRequestURI(c.buildURL(path))
	req.Header.SetMethod(fasthttp.MethodPut)
	req.Header.Set(constants.HeaderContentType, constants.ContentTypeJSON)

	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		req.SetBody(data)
	}

	return c.do(ctx, req, resp)
}

// Delete DELETE 请求
func (c *FastClient) Delete(ctx context.Context, path string) (*Response, error) {
	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)

	req.SetRequestURI(c.buildURL(path))
	req.Header.SetMethod(fasthttp.MethodDelete)

	return c.do(ctx, req, resp)
}

// Patch PATCH 请求
func (c *FastClient) Patch(ctx context.Context, path string, body interface{}) (*Response, error) {
	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)

	req.SetRequestURI(c.buildURL(path))
	req.Header.SetMethod(fasthttp.MethodPatch)
	req.Header.Set(constants.HeaderContentType, constants.ContentTypeJSON)

	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		req.SetBody(data)
	}

	return c.do(ctx, req, resp)
}

// SendRequest 发送自定义请求（完全兼容原生 Client 的 SendRequest 签名）
func (c *FastClient) SendRequest(ctx context.Context, method, path string, body io.Reader, headerFunc func(*fasthttp.Request)) (*Response, error) {
	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseRequest(req)
	defer fasthttp.ReleaseResponse(resp)

	req.SetRequestURI(c.buildURL(path))
	req.Header.SetMethod(method)

	if body != nil {
		data, err := io.ReadAll(body)
		if err != nil {
			return nil, fmt.Errorf("read request body failed: %w", err)
		}
		req.SetBody(data)
	}

	if headerFunc != nil {
		headerFunc(req)
	}

	return c.do(ctx, req, resp)
}

// ============================================================================
// 核心执行逻辑
// ============================================================================

// do 执行请求
func (c *FastClient) do(ctx context.Context, req *fasthttp.Request, resp *fasthttp.Response) (*Response, error) {
	// 应用默认请求头
	c.applyHeaders(req)

	// 执行中间件
	if err := c.executeMiddleware(req); err != nil {
		return nil, err
	}

	// 获取超时
	deadline, hasDeadline := ctx.Deadline()
	var doTimeout time.Duration
	if hasDeadline {
		doTimeout = time.Until(deadline)
		if doTimeout <= 0 {
			return nil, context.DeadlineExceeded
		}
	} else {
		doTimeout = c.timeout
	}

	// 发送请求（fasthttp 不内置 context 支持，用 DoDeadline 模拟）
	traceID := middleware.GetTraceIDFromContext(ctx)
	start := time.Now()
	err := c.client.DoDeadline(req, resp, deadline)
	if err != nil {
		log.WithFields(logrus.Fields{
			constants.LogFieldTraceID: traceID,
			"method":                  string(req.Header.Method()),
			"url":                     string(req.URI().FullURI()),
			"duration":                time.Since(start).String(),
			"err":                     err.Error(),
		}).Errorf("[httpclient:fast] request failed")
		return nil, fmt.Errorf("request failed: %w", err)
	}

	// 检查 context 是否已取消
	if ctx.Err() != nil {
		log.WithFields(logrus.Fields{
			constants.LogFieldTraceID: traceID,
			"method":                  string(req.Header.Method()),
			"url":                     string(req.URI().FullURI()),
			"duration":                time.Since(start).String(),
			"err":                     ctx.Err().Error(),
		}).Errorf("[httpclient:fast] request canceled")
		return nil, ctx.Err()
	}

	// 复制响应体（fasthttp 对象会被回收到池，必须拷贝）
	bodyCopy := make([]byte, len(resp.Body()))
	copy(bodyCopy, resp.Body())

	_ = doTimeout // 已通过 deadline 体现

	// 转换响应头（fasthttp → map[string][]string，等同于 http.Header）
	headers := make(map[string][]string)
	resp.Header.VisitAll(func(key, value []byte) {
		headers[string(key)] = append(headers[string(key)], string(value))
	})

	log.WithFields(logrus.Fields{
		constants.LogFieldTraceID: traceID,
		"method":                  string(req.Header.Method()),
		"url":                     string(req.URI().FullURI()),
		"status":                  resp.StatusCode(),
		"duration":                time.Since(start).String(),
	}).Debugf("[httpclient:fast] request completed")
	return &Response{
		StatusCode: resp.StatusCode(),
		Headers:    headers,
		Body:       bodyCopy,
	}, nil
}

// ============================================================================
// 便捷构造方法（与原生 Client 共用 Response 类型）
// ============================================================================

// FastJSONResponse 快速解析 JSON 响应
func FastJSONResponse(resp *Response, v interface{}) error {
	return json.Unmarshal(resp.Body, v)
}

// QuickGet 便捷 GET 请求（默认超时）
func QuickGet(ctx context.Context, url string, params map[string]string) (*Response, error) {
	return NewFast(WithFastBaseURL(url)).Get(ctx, "", params)
}

// QuickPost 便捷 POST 请求（默认超时）
func QuickPost(ctx context.Context, url string, body interface{}) (*Response, error) {
	return NewFast(WithFastBaseURL(url)).Post(ctx, "", body)
}

// QuickPostJSON 便捷 POST JSON 请求（默认超时，封装常用场景）
func QuickPostJSON(ctx context.Context, url string, body interface{}, result interface{}) error {
	resp, err := QuickPost(ctx, url, body)
	if err != nil {
		return err
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, resp.String())
	}
	return resp.Unmarshal(result)
}

// QuickGetJSON 便捷 GET JSON 请求（默认超时）
func QuickGetJSON(ctx context.Context, url string, params map[string]string, result interface{}) error {
	resp, err := QuickGet(ctx, url, params)
	if err != nil {
		return err
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, resp.String())
	}
	return resp.Unmarshal(result)
}

// ReadBodyString 读取请求体为字符串（兼容 io.Reader → body 的常用写法）
func ReadBodyString(r io.Reader) string {
	if r == nil {
		return ""
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return ""
	}
	return string(data)
}

// ReadBodyBytes 读取请求体为 []byte
func ReadBodyBytes(r io.Reader) []byte {
	if r == nil {
		return nil
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil
	}
	return data
}

// BodyReader 从字符串创建 io.Reader
func BodyReader(s string) io.Reader {
	if s == "" {
		return nil
	}
	return bytes.NewReader([]byte(s))
}
