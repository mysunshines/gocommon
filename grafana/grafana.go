// Package grafana 提供 Grafana 面板图片渲染客户端（依赖 grafana-image-renderer 插件）。
// 通过 /render/d-solo 接口把单个面板渲染为 PNG，供 report-service 将真实 Grafana
// 图表直接内联到邮件报告中。仅依赖标准库。
package grafana

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mysunshines/gocommon/constants"
	"github.com/mysunshines/gocommon/log"
	"github.com/mysunshines/gocommon/middleware"

	"github.com/sirupsen/logrus"
)

// Client 是 Grafana 图片渲染客户端。
type Client struct {
	baseURL  string // 如 http://grafana:3000
	apiKey   string // 服务账号 Token（优先于 basic auth）
	user     string // basic auth 用户名（apiKey 为空时使用）
	password string // basic auth 密码
	orgID    int    // 组织 ID（默认 1）
	http     *http.Client // 底层 HTTP 客户端
}

// Options 客户端构造参数。
type Options struct {
	BaseURL  string        // 基础地址，如 http://grafana:3000
	APIKey   string        // 服务账号 Token（优先于 basic auth）
	User     string        // basic auth 用户名
	Password string        // basic auth 密码
	OrgID    int           // 组织 ID（默认 1）
	Timeout  time.Duration // 请求超时（默认 30s）
}

// New 创建 Grafana 渲染客户端。
func New(opt Options) *Client {
	if opt.Timeout <= 0 {
		opt.Timeout = 30 * time.Second
	}
	if opt.OrgID <= 0 {
		opt.OrgID = 1
	}
	return &Client{
		baseURL:  strings.TrimRight(opt.BaseURL, "/"),
		apiKey:   opt.APIKey,
		user:     opt.User,
		password: opt.Password,
		orgID:    opt.OrgID,
		http: &http.Client{
			Timeout: opt.Timeout,
			Transport: &http.Transport{
				MaxIdleConns:        50,
				MaxIdleConnsPerHost: 50,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

// PanelOptions 描述一次面板渲染请求。
type PanelOptions struct {
	DashboardUID  string            // 仪表盘 UID（必填）
	DashboardSlug string            // 仪表盘 slug（可选，仅用于 URL 可读性）
	PanelID       int               // 面板 ID（必填）
	From          time.Time         // 起始时间
	To            time.Time         // 结束时间
	Width         int               // 图片宽（默认 1000）
	Height        int               // 图片高（默认 500）
	Theme         string            // light | dark（默认 light）
	Timezone      string            // 如 Asia/Shanghai
	Vars          map[string]string // 模板变量（var-xxx=值）
}

// RenderPanel 渲染指定面板并返回 PNG 字节。
func (c *Client) RenderPanel(ctx context.Context, opt PanelOptions) ([]byte, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("grafana: base url is empty")
	}
	if opt.DashboardUID == "" || opt.PanelID <= 0 {
		return nil, fmt.Errorf("grafana: dashboard_uid and panel_id are required")
	}

	slug := opt.DashboardSlug
	if slug == "" {
		slug = "d"
	}
	if opt.Width <= 0 {
		opt.Width = 1000
	}
	if opt.Height <= 0 {
		opt.Height = 500
	}
	if opt.Theme == "" {
		opt.Theme = "light"
	}

	q := url.Values{}
	q.Set("orgId", strconv.Itoa(c.orgID))
	q.Set("panelId", strconv.Itoa(opt.PanelID))
	q.Set("width", strconv.Itoa(opt.Width))
	q.Set("height", strconv.Itoa(opt.Height))
	q.Set("theme", opt.Theme)
	if !opt.From.IsZero() {
		q.Set("from", strconv.FormatInt(opt.From.UnixMilli(), 10))
	}
	if !opt.To.IsZero() {
		q.Set("to", strconv.FormatInt(opt.To.UnixMilli(), 10))
	}
	if opt.Timezone != "" {
		q.Set("tz", opt.Timezone)
	}
	for k, v := range opt.Vars {
		q.Set("var-"+k, v)
	}

	u := fmt.Sprintf("%s/render/d-solo/%s/%s?%s", c.baseURL, url.PathEscape(opt.DashboardUID), url.PathEscape(slug), q.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("grafana: build request failed: %w", err)
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	} else if c.user != "" {
		req.SetBasicAuth(c.user, c.password)
	}

	traceID := middleware.GetTraceIDFromContext(ctx)
	start := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		log.WithFields(logrus.Fields{
			constants.LogFieldTraceID: traceID,
			"url":                     u,
			"dashboard":               opt.DashboardUID,
			"panel":                   opt.PanelID,
			"duration":                time.Since(start).String(),
			"err":                     err.Error(),
		}).Errorf("[grafana] render request failed")
		return nil, fmt.Errorf("grafana: render request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		log.WithFields(logrus.Fields{
			constants.LogFieldTraceID: traceID,
			"url":                     u,
			"status":                  resp.StatusCode,
			"duration":                time.Since(start).String(),
			"err":                     err.Error(),
		}).Errorf("[grafana] read response failed")
		return nil, fmt.Errorf("grafana: read response failed: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		log.WithFields(logrus.Fields{
			constants.LogFieldTraceID: traceID,
			"url":                     u,
			"status":                  resp.StatusCode,
			"duration":                time.Since(start).String(),
			"body":                    truncate(string(data), 256),
		}).Errorf("[grafana] unexpected status")
		return nil, fmt.Errorf("grafana: unexpected status %d: %s", resp.StatusCode, truncate(string(data), 256))
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "image/") {
		// 渲染插件缺失或鉴权失败时通常返回 HTML/JSON，此处直接判定为失败便于排查。
		log.WithFields(logrus.Fields{
			constants.LogFieldTraceID: traceID,
			"url":                     u,
			"status":                  resp.StatusCode,
			"content_type":            ct,
			"duration":                time.Since(start).String(),
			"body":                    truncate(string(data), 256),
		}).Errorf("[grafana] unexpected content-type (image-renderer plugin missing or auth failed)")
		return nil, fmt.Errorf("grafana: unexpected content-type %q (image-renderer plugin missing or auth failed): %s", ct, truncate(string(data), 256))
	}
	log.WithFields(logrus.Fields{
		constants.LogFieldTraceID: traceID,
		"url":                     u,
		"status":                  resp.StatusCode,
		"duration":                time.Since(start).String(),
	}).Debugf("[grafana] render completed")
	return data, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
