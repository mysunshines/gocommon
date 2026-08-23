package log

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/mysunshines/gocommon/constants"
	"github.com/mysunshines/gocommon/config"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// Loki 集中日志（想法 3 · 方案 A）
//
// 通过 logrus Hook 把每条结构化日志异步批量推送到 Loki，并携带 trace_id / service
// 作为 label，使得 gateway 的 debug API 能用一个 trace_id 聚合「全链路」日志
// （含各服务的 request/response）。OTel 的 TraceID 即作为日志的 trace_id label，
// 与 Tempo/Jaeger（方案 B）天然共用同一 ID。
//
// 降级：未调用 EnableLoki（或 Loki 地址为空 / 推送失败）时，日志仍只走本地 stdout/文件，
// 完全不影响现有部署。推送失败仅打 warning，绝不阻塞请求路径。
// ============================================================================

// lokiConfig 保存 Loki 推送所需参数。
type lokiConfig struct {
	enabled    bool
	url        string // Loki HTTP push 地址，如 http://loki:3100/loki/api/v1/push
	service    string // 服务名（作为 label）
	tenantID   string // 多租户 ID（可选，空则不带 X-Scope-OrgID 头）
	batchSize  int    // 每批最大条数
	flushEvery time.Duration
}

var (
	lokiMu   sync.Mutex
	lokiHook *LokiHook
)

// LokiHook 实现 logrus.Hook，将日志异步批量推送到 Loki。
type LokiHook struct {
	cfg      lokiConfig
	ch       chan lokiEntry
	stopCh   chan struct{}
	stopOnce sync.Once
	httpCli  *http.Client
}

type lokiEntry struct {
	ts       time.Time
	level    string
	message  string
	fields   logrus.Fields
	service  string
	traceID  string
}

// EnableLoki 启用 Loki 集中日志推送。lokiURL 为空时直接跳过（降级）。
// 应在 log.Init 之后调用一次（通常在服务 main 启动早期）。
func EnableLoki(lokiURL, service, tenantID string) {
	lokiMu.Lock()
	defer lokiMu.Unlock()

	if lokiURL == "" {
		return
	}
	if lokiHook != nil {
		return // 已启用，避免重复
	}

	h := &LokiHook{
		cfg: lokiConfig{
			enabled:    true,
			url:        lokiURL,
			service:    service,
			tenantID:   tenantID,
			batchSize:  64,
			flushEvery: 2 * time.Second,
		},
		ch:      make(chan lokiEntry, constants.AsyncLogBufferSize),
		stopCh:  make(chan struct{}),
		httpCli: &http.Client{Timeout: 5 * time.Second},
	}
	lokiHook = h
	GetLogger().AddHook(h)
	go h.run()
}

// Levels 声明 Hook 捕获所有级别。
func (h *LokiHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

// Fire 把一条日志投递到异步 channel（非阻塞，满则丢弃，绝不阻塞请求 goroutine）。
func (h *LokiHook) Fire(e *logrus.Entry) error {
	ts := e.Time
	if ts.IsZero() {
		ts = time.Now()
	}
	// 从 fields 中优先取 trace_id（中间件已注入），否则从 msg 兜底。
	traceID, _ := e.Data[constants.LogFieldTraceID].(string)
	select {
	case h.ch <- lokiEntry{
		ts:      ts,
		level:   e.Level.String(),
		message: e.Message,
		fields:  e.Data,
		service: h.cfg.service,
		traceID: traceID,
	}:
	default:
		// 缓冲满，丢弃（运维可观察日志洪峰，不阻塞业务）。
	}
	return nil
}

// run 后台批量推送 goroutine。
func (h *LokiHook) run() {
	batch := make([]lokiEntry, 0, h.cfg.batchSize)
	ticker := time.NewTicker(h.cfg.flushEvery)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := h.push(batch); err != nil {
			// 仅 warning，不致命。
			GetLogger().WithField(constants.LogFieldService, h.cfg.service).
				Warnf("loki push failed (dropped %d logs): %v", len(batch), err)
		}
		batch = batch[:0]
	}

	for {
		select {
		case entry := <-h.ch:
			batch = append(batch, entry)
			if len(batch) >= h.cfg.batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-h.stopCh:
			// 兜底把残留全部读出再 flush
			for {
				select {
				case entry := <-h.ch:
					batch = append(batch, entry)
				default:
					flush()
					return
				}
			}
		}
	}
}

// DisableLoki 停止 Loki 推送（优雅关闭时调用，flush 残留日志）。
func DisableLoki() {
	lokiMu.Lock()
	defer lokiMu.Unlock()
	if lokiHook == nil {
		return
	}
	lokiHook.stopOnce.Do(func() {
		close(lokiHook.stopCh)
	})
	lokiHook = nil
}

// push 将一批日志按 Loki JSON push 协议发送。
// 同一 trace_id 的日志归并到同一个 stream（label 含 trace_id + service + level），
// 使查询 {app="blog", trace_id="xxx"} 能精确聚合全链路日志。
func (h *LokiHook) push(entries []lokiEntry) error {
	// 以 (service,level,trace_id) 组合为 key 聚合到不同 stream。
	streams := map[string]*lokiStream{}
	order := []string{}

	for _, e := range entries {
		key := fmt.Sprintf("%s|%s|%s",
			nonEmpty(e.service, "unknown"),
			nonEmpty(e.level, "info"),
			nonEmpty(e.traceID, "none"),
		)
		st, ok := streams[key]
		if !ok {
			st = &lokiStream{
				Stream: map[string]string{
					"app":      "blog",
					"service":  nonEmpty(e.service, "unknown"),
					"level":    nonEmpty(e.level, "info"),
					"trace_id": nonEmpty(e.traceID, "none"),
				},
				Values: [][2]string{},
			}
			streams[key] = st
			order = append(order, key)
		}
		// Loki 要求纳秒时间戳字符串
		ns := fmt.Sprintf("%d", e.ts.UnixNano())
		line, _ := json.Marshal(map[string]interface{}{
			"time":    e.ts.Format(constants.DateTimeFormat),
			"level":   e.level,
			"service": e.service,
			"traceID": e.traceID,
			"msg":     e.message,
			"fields":  e.fields,
		})
		_ = line
		st.Values = append(st.Values, [2]string{ns, string(line)})
	}

	payload := lokiPushRequest{Streams: make([]lokiStream, 0, len(order))}
	for _, lbl := range order {
		payload.Streams = append(payload.Streams, *streams[lbl])
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, h.cfg.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if h.cfg.tenantID != "" {
		req.Header.Set("X-Scope-OrgID", h.cfg.tenantID)
	}

	resp, err := h.httpCli.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("loki push http status %d", resp.StatusCode)
	}
	return nil
}

type lokiPushRequest struct {
	Streams []lokiStream `json:"streams"`
}

type lokiStream struct {
	Stream map[string]string `json:"stream"`
	Values [][2]string       `json:"values"`
}

// EnableLokiFromConfig 便捷封装：从 config.LokiConfig 启用 Loki 集中日志。
// enabled=false 或 url 为空时跳过（降级：仅本地日志）。
func EnableLokiFromConfig(cfg config.LokiConfig, service string) {
	if !cfg.Enabled || cfg.URL == "" {
		return
	}
	EnableLoki(cfg.URL, service, cfg.TenantID)
}

func nonEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
