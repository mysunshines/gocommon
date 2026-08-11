package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mysunshines/gocommon/constants"
	"github.com/mysunshines/gocommon/log"
	"github.com/mysunshines/gocommon/middleware"
	"github.com/mysunshines/gocommon/resilience"

	"github.com/sirupsen/logrus"
)

// Client HTTP 客户端
type Client struct {
	client         *http.Client      // 底层标准库 HTTP 客户端
	baseURL        string            // 基础 URL，拼接到各请求路径前
	timeout        time.Duration     // 请求超时
	headers        map[string]string // 默认请求头（单次请求可覆盖）
	middleware     []Middleware      // 请求发出前执行的中间件链
	resilienceKey  string            // resilience 按此 key 区分熔断/限流；为空时取 baseURL 的 host
	dumpBodies     bool              // 是否在 debug 日志中记录请求/响应体
}

// Middleware HTTP 中间件
type Middleware func(*http.Request) error

// Config 客户端配置
type Config struct {
	BaseURL   string        // 基础 URL
	Timeout   time.Duration // 超时时间
	Headers   map[string]string // 默认请求头
	MaxIdle   int           // 最大空闲连接数
	MaxConns  int           // 最大连接数
	KeepAlive time.Duration // 连接保活时间
}

// Option 配置选项
type Option func(*Client)

// New 创建 HTTP 客户端
func New(opts ...Option) *Client {
	c := &Client{
		client: &http.Client{
			Timeout: constants.DefaultReadTimeout * time.Second,
		},
		timeout: constants.DefaultReadTimeout * time.Second,
		headers: make(map[string]string),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// WithBaseURL 设置基础 URL
func WithBaseURL(baseURL string) Option {
	return func(c *Client) {
		c.baseURL = baseURL
	}
}

// WithTimeout 设置超时时间
func WithTimeout(timeout time.Duration) Option {
	return func(c *Client) {
		c.timeout = timeout
		c.client.Timeout = timeout
	}
}

// WithHeader 设置默认请求头
func WithHeader(key, value string) Option {
	return func(c *Client) {
		c.headers[key] = value
	}
}

// WithHeaders 设置默认请求头
func WithHeaders(headers map[string]string) Option {
	return func(c *Client) {
		for k, v := range headers {
			c.headers[k] = v
		}
	}
}

// WithMiddleware 添加中间件
func WithMiddleware(m Middleware) Option {
	return func(c *Client) {
		c.middleware = append(c.middleware, m)
	}
}

// WithResilienceKey 设置 resilience 的 serviceKey，用于按下游区分超时/熔断/限流。
// 不设置时默认取 baseURL 的 host 作为 key。与 resilience.SetPolicy(key, ...) 配套使用。
func WithResilienceKey(key string) Option {
	return func(c *Client) {
		c.resilienceKey = key
	}
}

// WithDumpBodies 开启后，debug 级别的请求/响应日志会附带 req_body 与 resp_body，
// 便于排查 Consul 等服务发现的请求与响应内容。默认关闭，避免大响应体刷日志。
func WithDumpBodies(enable bool) Option {
	return func(c *Client) {
		c.dumpBodies = enable
	}
}

// Response HTTP 响应
type Response struct {
	StatusCode int         // HTTP 状态码
	Headers    http.Header // 响应头
	Body       []byte      // 响应体原始字节
}

// buildURL 构建完整 URL
func (c *Client) buildURL(path string) string {
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

// applyHeaders 应用默认请求头
func (c *Client) applyHeaders(req *http.Request) {
	for k, v := range c.headers {
		if req.Header.Get(k) == "" {
			req.Header.Set(k, v)
		}
	}
}

// executeMiddleware 执行中间件
func (c *Client) executeMiddleware(req *http.Request) error {
	for _, m := range c.middleware {
		if err := m(req); err != nil {
			return err
		}
	}
	return nil
}

// Get GET 请求
func (c *Client) Get(ctx context.Context, path string, params map[string]string) (*Response, error) {
	_fullURL := c.buildURL(path)
	if len(params) > 0 {
		q := url.Values{}
		for k, v := range params {
			q.Set(k, v)
		}
		if strings.Contains(_fullURL, "?") {
			_fullURL += "&" + q.Encode()
		} else {
			_fullURL += "?" + q.Encode()
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, _fullURL, nil)
	if err != nil {
		return nil, err
	}

	return c.do(req)
}

// Post POST 请求
func (c *Client) Post(ctx context.Context, path string, body interface{}) (*Response, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.buildURL(path), reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set(constants.HeaderContentType, constants.ContentTypeJSON)

	return c.do(req)
}

// Put PUT 请求
func (c *Client) Put(ctx context.Context, path string, body interface{}) (*Response, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.buildURL(path), reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set(constants.HeaderContentType, constants.ContentTypeJSON)

	return c.do(req)
}

// Delete DELETE 请求
func (c *Client) Delete(ctx context.Context, path string) (*Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.buildURL(path), nil)
	if err != nil {
		return nil, err
	}

	return c.do(req)
}

// Patch PATCH 请求
func (c *Client) Patch(ctx context.Context, path string, body interface{}) (*Response, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, c.buildURL(path), reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set(constants.HeaderContentType, constants.ContentTypeJSON)

	return c.do(req)
}

// SendRequest 发送自定义请求
func (c *Client) SendRequest(ctx context.Context, method, path string, body io.Reader, headerFunc func(http.Header)) (*Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.buildURL(path), body)
	if err != nil {
		return nil, err
	}

	if headerFunc != nil {
		headerFunc(req.Header)
	}

	return c.do(req)
}

// do 执行请求
func (c *Client) do(req *http.Request) (*Response, error) {
	// 应用默认请求头
	c.applyHeaders(req)

	// 若开启 body 记录，先读取原始请求体（读完须重建 req.Body，因后续 Clone 仍需读取）。
	var reqBody []byte
	if c.dumpBodies && req.Body != nil {
		reqBody, _ = io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewReader(reqBody))
	}

	// 执行中间件
	if err := c.executeMiddleware(req); err != nil {
		return nil, err
	}

	// 选取 resilience serviceKey：显式 key 优先，否则取 baseURL 的 host。
	key := c.resilienceKey
	if key == "" && c.baseURL != "" {
		if u, err := url.Parse(c.baseURL); err == nil {
			key = u.Scheme + "://" + u.Host
		} else {
			key = c.baseURL
		}
	}
	policy := resilience.ForService(key)
	ctx := resilience.WithServiceKey(req.Context(), key)
	traceID := middleware.GetTraceIDFromContext(req.Context())

	// 用 resilience 包裹真正的网络请求：超时 + 限流 + 熔断 + 降级。
	var resp *http.Response
	start := time.Now()
	execErr := policy.Execute(ctx, func(cctx context.Context) error {
		// 将带超时的 context 注入请求，覆盖 client.Timeout（后者对读取也生效，二者取最短）。
		r := req.Clone(cctx)
		hr, err := c.client.Do(r)
		if err != nil {
			return err
		}
		resp = hr
		return nil
	}, nil)
	if execErr != nil {
		fields := logrus.Fields{
			constants.LogFieldTraceID: traceID,
			"method":                  req.Method,
			"url":                     req.URL.String(),
			"duration":                time.Since(start).String(),
			"err":                     execErr.Error(),
		}
		if c.dumpBodies {
			fields["req_body"] = string(reqBody)
		}
		log.WithFields(fields).Errorf("[httpclient] request failed")
		return nil, fmt.Errorf("request failed: %w", execErr)
	}
	defer resp.Body.Close()

	// 读取响应体
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fields := logrus.Fields{
			constants.LogFieldTraceID: traceID,
			"method":                  req.Method,
			"url":                     req.URL.String(),
			"status":                  resp.StatusCode,
			"duration":                time.Since(start).String(),
			"err":                     err.Error(),
		}
		if c.dumpBodies {
			fields["req_body"] = string(reqBody)
		}
		log.WithFields(fields).Errorf("[httpclient] read response failed")
		return nil, fmt.Errorf("read response failed: %w", err)
	}

	fields := logrus.Fields{
		constants.LogFieldTraceID: traceID,
		"method":                  req.Method,
		"url":                     req.URL.String(),
		"status":                  resp.StatusCode,
		"duration":                time.Since(start).String(),
	}
	if c.dumpBodies {
		fields["req_body"] = string(reqBody)
		fields["resp_body"] = string(body)
	}
	log.WithFields(fields).Debugf("[httpclient] request completed")
	return &Response{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header,
		Body:       body,
	}, nil
}

// Unmarshal 解析响应体为 JSON
func (r *Response) Unmarshal(v interface{}) error {
	return json.Unmarshal(r.Body, v)
}

// String 获取响应体字符串
func (r *Response) String() string {
	return string(r.Body)
}

// IsSuccess 检查是否成功响应
func (r *Response) IsSuccess() bool {
	return r.StatusCode >= http.StatusOK && r.StatusCode < http.StatusMultipleChoices
}

// IsClientError 检查是否是客户端错误
func (r *Response) IsClientError() bool {
	return r.StatusCode >= http.StatusBadRequest && r.StatusCode < http.StatusInternalServerError
}

// IsServerError 检查是否是服务端错误
func (r *Response) IsServerError() bool {
	return r.StatusCode >= http.StatusInternalServerError
}

// Close 关闭客户端（清理资源）
func (c *Client) Close() error {
	// HTTP 客户端不需要显式关闭
	return nil
}
